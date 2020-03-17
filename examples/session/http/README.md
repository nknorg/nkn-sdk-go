Host the files in a directory at listener side and make it accessible on dialer
side.

Basic usage:

On listener side:

```shell
go run examples/http/main.go -l -n 4 -dir .
```

and you will see the listening address at console output:

```
Serving content at <addr>
```

Then on dialer side:

```shell
go run examples/http/main.go -d -a <addr> -http :8080
```

Now you can visit `http://127.0.0.1:8080` to see the content of serving
directory.

Use `--help` to see more options.
