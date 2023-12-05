# Stackism

Running Stackism gives operators a platform to execute Extism plugins when
specified events are triggered.

## Event Types

### HTTPS

```
# global options
{
	http_port 8001
	order extism last
}

# site
localhost:8000 {
  file_server www
  
	handle_path /fn/* {
		extism +wasi thing=testing
    # this runs a module stored at {*}.wasm (/fn/ prefix is stripped),
    # providing a wasi host (+wasi) to run explicitly granted resources,
    #   NOTE: currently the extism module must be a WASI command
    # and setting initial configuration key-value with the `K=V` pairs in the directive
	}
}
```

### FTP

Run an FTP server as a Caddy application, expecting the Caddy server to `Start`
and `Stop` it.

```
localhost:8000 {
  file_server www/
  # exposes a file system over HTTP loading the file within the path provided

  # handle_path ... 
}

localhost:2121 {
  ftp admin admin
}
```
