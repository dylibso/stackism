package ftpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"goftp.io/server/v2"
	"goftp.io/server/v2/driver/file"

	extism "github.com/extism/go-sdk"

	caddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
)

var _ caddy.App = (*FTPApp)(nil)

func init() {
	caddy.RegisterModule(FTPApp{})
	httpcaddyfile.RegisterGlobalOption("ftpserver", parseGlobalFTPOption)
}

type FTPApp struct {
	server *server.Server
	logger *zap.Logger
}

func parseGlobalFTPOption(d *caddyfile.Dispenser, existingVal interface{}) (interface{}, error) {
	return httpcaddyfile.App{
		Name:  "ftpserver",
		Value: caddyconfig.JSON(FTPApp{}, nil),
	}, nil
}

func (a FTPApp) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID: "ftpserver",
		New: func() caddy.Module {
			return new(FTPApp)
		},
	}
}

func (a *FTPApp) Provision(ctx *caddy.Context) error {
	a.logger = ctx.Logger()
	return nil
}

func (a *FTPApp) startServer() error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}

	root = filepath.Join(root)
	driver, err := file.NewDriver(root)
	if err != nil {
		return err
	}

	s, err := server.NewServer(&server.Options{
		Driver: driver,
		Auth: &server.SimpleAuth{
			Name:     "admin",
			Password: "admin",
		},
		Perm:      server.NewSimplePerm("root", "root"), // TODO: make this configurable
		RateLimit: 1000000,                              // 1MB/s limit
		Port:      2121,
	})
	if err != nil {
		return err
	}
	if s == nil {
		return errors.New("no FTP server started")
	}

	s.RegisterNotifer(&pluginNotifier{driver: driver})
	a.server = s
	return nil
}

func (a FTPApp) Start() error {
	if err := a.startServer(); err != nil {
		return err
	}
	if err := a.server.ListenAndServe(); err != nil {
		return err
	}

	return nil
}

func (a FTPApp) Stop() error {
	return a.server.Shutdown()
}

type Action struct {
	Plugin       string            `json:"plugin"`
	AllowedHosts []string          `json:"allowed_hosts"`
	AllowedPaths map[string]string `json:"allowed_paths"`
}

type FtpxConfig struct {
	AfterFilePut        []Action `json:"after_file_put"`
	AfterCurDirChanged  []Action `json:"after_cur_dir_changed"`
	AfterDirCreated     []Action `json:"after_dir_created"`
	AfterDirDeleted     []Action `json:"after_dir_deleted"`
	AfterFileDeleted    []Action `json:"after_file_deleted"`
	AfterFileDownloaded []Action `json:"after_file_downloaded"`
	AfterUserLogin      []Action `json:"after_user_login"`
	BeforeChangeCurDir  []Action `json:"before_change_cur_dir"`
	BeforeCreateDir     []Action `json:"before_create_dir"`
	BeforeDeleteDir     []Action `json:"before_delete_dir"`
	BeforeDeleteFile    []Action `json:"before_delete_file"`
	BeforeDownloadFile  []Action `json:"before_download_file"`
	BeforeLoginUser     []Action `json:"before_login_user"`
	BeforePutFile       []Action `json:"before_put_file"`
}

var (
	_ server.Notifier = &pluginNotifier{}
)

type pluginNotifier struct {
	driver server.Driver
}

func (p *pluginNotifier) AfterFilePut(ctx *server.Context, dstPath string, size int64, err error) {
	println("AfterFilePut - called")
	if strings.HasSuffix(dstPath, ".wasm") || strings.HasSuffix(dstPath, ".ftpx.json") {
		println("ignore .wasm & .ftpx.json")
		return
	}

	pm, err := p.readFtpxConfig(ctx, dstPath)
	if err != nil {
		if strings.Contains(err.Error(), "no such file or directory") {
			return
		}
		println("failed to read manifest", err.Error())
		return
	}

	for _, action := range pm.AfterFilePut {
		_, fileData, err := p.driver.GetFile(ctx, dstPath, 0)
		if err != nil {
			println("failed to read file:", dstPath, err)
			return
		}

		var fileBuf bytes.Buffer
		_, err = io.Copy(&fileBuf, fileData)
		if err != nil {
			println("failed to copy file data:", dstPath, err.Error())
			return
		}

		dir := filepath.Dir(dstPath)
		// wasmLocation := filepath.Join("/.plugins", dir, action.Plugin)
		wasmLocation := filepath.Join(dir, action.Plugin)

		_, fileData, err = p.driver.GetFile(ctx, wasmLocation, 0)
		if err != nil {
			println("failed to read wasm file:", wasmLocation, err)
			return
		}
		var wasmBuf bytes.Buffer
		_, err = io.Copy(&wasmBuf, fileData)
		if err != nil {
			println("failed to copy wasm data:", wasmLocation, err.Error())
			return
		}

		manifest := extism.Manifest{
			Wasm: []extism.Wasm{extism.WasmData{
				Data: wasmBuf.Bytes(),
				Name: action.Plugin,
			}},
			AllowedHosts: action.AllowedHosts,
			AllowedPaths: action.AllowedPaths,
		}

		plugin, err := extism.NewPlugin(context.Background(), manifest, extism.PluginConfig{EnableWasi: true}, nil)
		if err != nil {
			println("AfterFilePut - failed to create plugin:", err.Error())
			return
		}
		println("loaded plugin", action.Plugin)
		defer plugin.Close()

		if !plugin.FunctionExists("after_file_put") {
			println("no function exists: 'after_file_put'")
			return
		}

		println("calling plugin with input =", fileBuf.Len())

		code, output, err := plugin.Call("after_file_put", fileBuf.Bytes())
		if err != nil {
			println("plugin call failed:", err.Error(), "error code:", code)
			return
		}

		outputBuf := &bytes.Buffer{}
		_, err = outputBuf.Write(output)
		if err != nil {
			println("failed to read output into buffer", err.Error())
			return
		}

		println("output size", outputBuf.Len())
		println("output", outputBuf.String())

		err = p.driver.DeleteFile(ctx, dstPath)
		if err != nil {
			println("failed to delete file", err.Error())
			return
		}
		_, err = p.driver.PutFile(ctx, dstPath, outputBuf, 0)
		if err != nil {
			println("failed to put file", dstPath)
			return
		}
	}
}

func (p *pluginNotifier) readFtpxConfig(ctx *server.Context, changePath string) (*FtpxConfig, error) {
	dir := filepath.Dir(changePath)
	_, file, err := p.driver.GetFile(ctx, filepath.Join(dir, ".ftpx.json"), 0)
	// _, file, err := p.driver.GetFile(ctx, filepath.Join("/.plugins", dir, ".ftpx.json"), 0)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	_, err = io.Copy(&buf, file)
	if err != nil {
		return nil, err
	}

	var pm FtpxConfig
	err = json.Unmarshal(buf.Bytes(), &pm)
	if err != nil {
		return nil, err
	}

	return &pm, nil
}

// AfterCurDirChanged implements server.Notifier.
func (*pluginNotifier) AfterCurDirChanged(ctx *server.Context, oldCurDir string, newCurDir string, err error) {
}

// AfterDirCreated implements server.Notifier.
func (*pluginNotifier) AfterDirCreated(ctx *server.Context, dstPath string, err error) {
}

// AfterDirDeleted implements server.Notifier.
func (*pluginNotifier) AfterDirDeleted(ctx *server.Context, dstPath string, err error) {
}

// AfterFileDeleted implements server.Notifier.
func (*pluginNotifier) AfterFileDeleted(ctx *server.Context, dstPath string, err error) {
}

// AfterFileDownloaded implements server.Notifier.
func (*pluginNotifier) AfterFileDownloaded(ctx *server.Context, dstPath string, size int64, err error) {
}

// AfterUserLogin implements server.Notifier.
func (*pluginNotifier) AfterUserLogin(ctx *server.Context, userName string, password string, passMatched bool, err error) {
}

// BeforeChangeCurDir implements server.Notifier.
func (*pluginNotifier) BeforeChangeCurDir(ctx *server.Context, oldCurDir string, newCurDir string) {
}

// BeforeCreateDir implements server.Notifier.
func (*pluginNotifier) BeforeCreateDir(ctx *server.Context, dstPath string) {
}

// BeforeDeleteDir implements server.Notifier.
func (*pluginNotifier) BeforeDeleteDir(ctx *server.Context, dstPath string) {
}

// BeforeDeleteFile implements server.Notifier.
func (*pluginNotifier) BeforeDeleteFile(ctx *server.Context, dstPath string) {
}

// BeforeDownloadFile implements server.Notifier.
func (*pluginNotifier) BeforeDownloadFile(ctx *server.Context, dstPath string) {
}

// BeforeLoginUser implements server.Notifier.
func (*pluginNotifier) BeforeLoginUser(ctx *server.Context, userName string) {
}

// BeforePutFile implements server.Notifier.
func (*pluginNotifier) BeforePutFile(ctx *server.Context, dstPath string) {
}
