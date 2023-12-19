package extismserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	caddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.etcd.io/bbolt"
	"go.uber.org/zap"

	extism "github.com/extism/go-sdk"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

var _ caddyhttp.MiddlewareHandler = (*ExtismServer)(nil)

func init() {
	caddy.RegisterModule(ExtismServer{})
	httpcaddyfile.RegisterHandlerDirective("extism", parseCaddyfileHandler)
}

func parseCaddyfileHandler(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var host ExtismServer
	err := host.UnmarshalCaddyfile(h.Dispenser)
	return host, err
}

type GlobalHostInfo ExtismServer

var hostWasi bool
var hostConfig map[string]string
var hostModulePath string

type Store interface {
	Get(key string) []byte
	Set(key string, value []byte)
}

type kvStore struct {
	db bbolt.DB
}

type ExtismServer struct {
	logger *zap.Logger
	store  Store
}

type MemKVStore struct {
	mu   *sync.Mutex
	data map[string][]byte
}

func (s MemKVStore) Get(key string) []byte {
	fmt.Println("GET: ", s.data)
	return s.data[key]
}

func (s MemKVStore) Set(key string, value []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = value
	fmt.Println("SET: ", s.data)
}

type DiskKVStore struct {
	db bbolt.DB
}

func (ExtismServer) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.extism",
		New: func() caddy.Module { return new(ExtismServer) },
	}
}

func (s *ExtismServer) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	println("unmarshall caddyfile")
	hostConfig = make(map[string]string)
	for d.Next() {
		for nesting := d.Nesting(); d.NextBlock(nesting); {
			switch d.Val() {
			case "wasi":
				if d.NextArg() {
					if d.Val() == "true" {
						hostWasi = true
					}
				}

			case "config":
				// any entry in `config` here can be overwritten by the request query params
				// which are later collected and put into the same `config` map on reach request
				for d.NextArg() {
					item := d.Val()
					if strings.Contains(item, "=") {
						kv := strings.Split(item, "=")
						if len(kv) < 2 {
							return errors.New(fmt.Sprintf("invalid extism config: %s", item))
						}
						hostConfig[kv[0]] = strings.Join(kv[1:], "=")
					}
				}

			case "path":
				if d.NextArg() {
					hostModulePath = d.Val()
					println("SET THE hostModulePath global!!!")
				}
			}
		}
	}

	return nil
}

func (s *ExtismServer) Provision(ctx caddy.Context) error {
	s.store = MemKVStore{data: make(map[string][]byte), mu: &sync.Mutex{}}
	s.logger = ctx.Logger()
	return nil
}

func (s ExtismServer) ServeHTTP(res http.ResponseWriter, req *http.Request, h caddyhttp.Handler) error {
	// TODO: handle the nested path scenario.
	// currently, only something like `/fn/some-module/{some-fn-name}` works with this code
	// but that expects all modules to be in the same top-level directory.
	// we want this to be possible too: `fn/some-dir/some-module/{some-fn-name}`
	parts := strings.Split(strings.TrimPrefix(req.URL.Path, "/"), "/")
	if len(parts) < 1 {
		res.WriteHeader(http.StatusBadRequest)
		s.logger.Info("need at least the wasm file prefix")
		return errors.New("Not enough information in URL path to call Extism plugin")
	}

	var fnName *string
	if len(parts) > 1 {
		fnName = &parts[1]
	}

	if len(req.URL.Query()) > 0 {
		s.logger.Debug(req.URL.Query().Encode())
	}

	// copy the static hostConfig into a request-specific one
	config := make(map[string]string)
	for k, v := range hostConfig {
		config[k] = v
	}
	// add the request query string items into the config
	for k, v := range req.URL.Query() {
		config[k] = v[0]
	}

	wasm := filepath.Join(hostModulePath, fmt.Sprintf("%s.wasm", parts[0]))
	fmt.Println("looking for wasm at:", wasm)
	// check that the wasm file is present
	if _, err := os.Stat(wasm); err == os.ErrNotExist {
		s.logger.Warn(fmt.Sprintf("unable to locate wasm: %s, %v", wasm, err))
		return h.ServeHTTP(res, req)
	}
	manifest := extism.Manifest{
		Wasm:   []extism.Wasm{extism.WasmFile{Path: wasm}},
		Config: config,
	}

	kvRead := extism.NewHostFunctionWithStack(
		"kv_read",
		func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
			if s.store == nil {
				fmt.Println("NO KVStore Initialized on provisioned struct")
				return
			}
			key, err := p.ReadString(stack[1])
			if err != nil {
				fmt.Println("kv_read: failed to read kv store key from current plugin:", p, err)
				return
			}
			fmt.Println("kv_read: key = ", key)

			value := s.store.Get(key)
			fmt.Println("kv_read: value = ", string(value))
			stack[0], err = p.WriteBytes(value)
			if err != nil {
				fmt.Println("kv_read: failed to write kv store value to current plugin:", p, err)
				return
			}
		},
		[]api.ValueType{extism.PTR, extism.PTR},
		[]api.ValueType{extism.PTR},
	)
	kvRead.SetNamespace("extism:host/user")

	kvWrite := extism.NewHostFunctionWithStack(
		"kv_write",
		func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
			if s.store == nil {
				fmt.Println("NO KVStore Initialized on provisioned struct")
				return
			}
			key, err := p.ReadString(stack[1])
			if err != nil {
				fmt.Println("kv_write: failed to read kv store key from current plugin:", p, err)
				return

			}

			fmt.Println("kv_write: key = ", key)

			value, err := p.ReadBytes(stack[2])
			if err != nil {
				fmt.Println("kv_write: failed to read kv store value from current plugin:", p, err)
				return
			}

			fmt.Println("kv_write: value = ", string(value))
			s.store.Set(key, value)
		},
		[]api.ValueType{extism.PTR, extism.PTR, extism.PTR},
		[]api.ValueType{},
	)
	kvWrite.SetNamespace("extism:host/user")

	moduleConfig := wazero.NewModuleConfig().WithStartFunctions()
	plugin, err := extism.NewPlugin(req.Context(), manifest, extism.PluginConfig{
		ModuleConfig: moduleConfig,
		EnableWasi:   hostWasi,
		LogLevel:     extism.LogLevelInfo,
	}, []extism.HostFunction{kvRead, kvWrite})
	if err != nil {
		s.logger.Info(fmt.Sprintf("failed to create plugin: %v", err))
		return h.ServeHTTP(res, req) // TODO: does this pass-thru to another block/handler?
	}

	plugin.SetLogger(func(level extism.LogLevel, log string) {
		fmt.Println(">>>>", level, log)
	})

	var bodyBuf bytes.Buffer
	_, err = io.Copy(&bodyBuf, req.Body)
	defer req.Body.Close()
	if err != nil {
		s.logger.Info(fmt.Sprintf("failed to copy body for input: %v", err))
		return errors.New(fmt.Sprintf("%s: %s", "copy body to buffer", err))
	}

	var statusCode uint32
	var responseBody []byte
	if fnName != nil {
		s.logger.Debug(fmt.Sprintf("%s.%s input size: %d", wasm, *fnName, bodyBuf.Len()))
		if strings.HasSuffix(*fnName, "json") {
			res.Header().Set(http.CanonicalHeaderKey("Content-Type"), "application/json")
		}
		statusCode, responseBody, err = plugin.Call(*fnName, bodyBuf.Bytes())
	} else {
		s.logger.Debug(fmt.Sprintf("%s.<<detect>> input size: %d", wasm, bodyBuf.Len()))
		statusCode, responseBody, err = s.detectCall(res, req, h, plugin, bodyBuf.Bytes())
	}
	if err != nil {
		return errors.New(fmt.Sprintf("%s: %s", "call plugin", err))
	}

	// convert extism return code to applicable HTTP status code
	if statusCode == 0 {
		statusCode = 200
	}
	if statusCode < 0 {
		s.logger.Error(fmt.Sprintf("error code: %d", statusCode))
		statusCode = 500
	}

	res.WriteHeader(int(statusCode))
	_, err = res.Write(responseBody)
	if err != nil {
		s.logger.Info(fmt.Sprintf("failed to write response: %v", err))
		return errors.New("failed to write response: " + err.Error())
	}

	return nil
}

func (s ExtismServer) detectCall(res http.ResponseWriter, req *http.Request, h caddyhttp.Handler, plugin *extism.Plugin, input []byte) (uint32, []byte, error) {
	var fnName string
	var accept string

	acceptHeader := req.Header.Get(http.CanonicalHeaderKey("Accept"))
	if strings.Contains(acceptHeader, "text/html") {
		accept = "html"
		res.Header().Set(http.CanonicalHeaderKey("Content-Type"), "text/html")
	} else if strings.Contains(acceptHeader, "application/json") {
		accept = "json"
		res.Header().Set(http.CanonicalHeaderKey("Content-Type"), "application/json")
	} else {
		accept = "text"
		res.Header().Set(http.CanonicalHeaderKey("Content-Type"), "text/plain")
	}

	fnName = fmt.Sprintf("%s_%s", strings.ToLower(req.Method), accept)

	if fnName != "" && plugin.FunctionExists(fnName) {
		return plugin.Call(fnName, input)
	} else if plugin.FunctionExists("respond") {
		return plugin.Call("respond", input)
	}

	return 404, nil, errors.New("Not Found")
}
