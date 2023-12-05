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