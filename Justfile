build:
    #!/usr/bin/env bash
    xcaddy build  \
        --with github.com/dylibso/stackism/src/web=./src/web \
        --with github.com/dylibso/stackism/src/ftp=./src/ftp

deps: 
    go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest

start-example: deps build
    #!/usr/bin/env bash
    mv caddy ./example
    cd example && pwd && ./caddy run

stop-example: 
    #!/usr/bin/env bash
    kill -9 $(pgrep caddy)

build-kvplugin:
    cd src/kvplugin && cargo build --target wasm32-unknown-unknown --release
    
build-content:
    cd src/content && cargo build --target wasm32-unknown-unknown --release

build-reverse:
    cd src/reverse && tinygo build -o reverse.wasm -target wasi main.go

build-plugins: build-reverse build-kvplugin build-content
