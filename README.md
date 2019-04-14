# webdu

[![](https://images.microbadger.com/badges/version/lutzky/webdu.svg)](https://microbadger.com/images/lutzky/webdu "Get your own version badge on microbadger.com")
[![](https://images.microbadger.com/badges/image/lutzky/webdu.svg)](https://microbadger.com/images/lutzky/webdu "Get your own image badge on microbadger.com")

WebDU is a simple web interface for disk usage.

## Usage

Some variables to make the example simpler:

```shell
PORT=8099 # The default port
BASEPATH=/path/to/relevant/files # Whatever path you want
```

Then, either use directly:

```
go build webdu
./webdu --base_path $BASEPATH --port $PORT
```

...or with docker:

```shell
docker run -p $PORT:8099 -v $BASEPATH:/data:ro \
  lutzky/webdu --base_path=/data
```

Then point your browser at http://localhost:8099/.

Note that http://localhost:8099/any/path/at/all will work as well, which is useful for putting webdu behind a reverse proxy. See http://github.com/lutzky/wamc for an example.
