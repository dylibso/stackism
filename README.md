# Stackism

`stackism` is a walk down memory lane infused with cutting edge WebAssembly extensibility!

If you recall the days of uploading content via FTP, editing scripts and saving with immediately
published results, you'll feel right at home. 

The catch is, that `stackism` is scriptable at many levels! The embedded FTP server provides a hook
so that on any file change, you can run a plug-in and edit that file. The web-server can locate and
execute Wasm modules (currently focused on [Extism](https:/github.com/extism/extism)), enabling you
to build and host applications/functions written in [many different languages](https://extism.org/docs/concepts/pdk).

## Demo

(coming soon)

## Usage

Releases will be made available soon with pre-built binaries. The binary includes an FTP server and
the web server (based on [Caddy](https://github.com/caddyserver/caddy)). 

To build and run the project yourself, follow these steps: 

```sh
just build
cp caddy example
cd example
./caddy run
```

Check out the `Caddyfile` in the `example` directory for a quick peek at how Extism is configured.

## Todo

See `todo.txt` for the known missing features, bug fixes, etc. This is a work in progress!