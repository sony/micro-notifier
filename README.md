# MicroNotifier - Simplified Pusher Clone

## Overview

MicroNotifier is a simple event multicaster:
You can push events to multiple Web browsers via WebSocket.
The protocol is a subset of Pusher, so you can use Pusher's client libraries.

MicroNotifier can run either *standalone* mode or *distributed* mode.
In the standalone mode, you just run a single notifier process
and it handles everything.

If a single process is not enough to deal with the amount of communication,
you can scale it up by running multiple notifier processes in the distributed mode.
With this mode, notifiers communicate each other via Redis server.
You have to run Redis server and a load balancer on your own.

The mode can be specified by the configuration file, which is explained below.


## Configuration

MicroNotifier's configuration file is in JSON, with the following fields recognized.
Sample configuration files can be found under [`config`](config) subdirectory.

- `host`: The server's host name [default: `localhost`]
- `port`: The server's port number [default: 8111]
- `certificate`: If you want to use secure connection, specify the path to the server certificate [default: ""]
- `private-key`: If you want to use secure connection, specify the path to the server private key [default: ""]
- `applications`: An array of application definitions. Each application must be the following map:
  - `name`: Name of the application.
  - `key`: Application key. A string consists of alphanumeric characters.
  - `secret`: Application secret. Used to sign subscription requests. A string consists of alphanumeric characters.
- `redis`: An object that gives the information of Redis server to use in distributed mode.
  If this option isn't specified, the notifier runs in standalone mode.
  - `address`: Redis server's hostname; you can also add port number after a colon. Example: `localhost:9375`.
  - `database`: (Optional) Nonnegative integer to specify the database number to use. [default: 0]
    > **Note**: Running the unit test with `-tags=redis_test` or `-tags=redis_sentinel_test` erases the database specified by this option.
    The test script uses database #1 for testing purpose by default.
    Change `config/sample-redis-test.json` or `config/sample-redis-sentinel-test.json` if you need to use different database.
  - `password`: (Optional) Redis password, if any.  [default: none]
  - `sentinel`: (Optional) Use redis server's with [Redis Sentinel](https://redis.io/topics/sentinel) mode. [default: false]

If `certificate` and `private-key` are given, the server serves with TLS.
Otherwise, the server serves plain HTTP.

If `redis` entry is specified, the server runs in distributed mode.
Otherwise, it runs in standalone mode.


## Running

Run binary, passing the path of the configuration file with `-c` option.


## Using from Pusher client libraries

You can find examples under [`samples`](samples) subdirectory.

### From pusher-http-go

When initializing `pusher.Client`, pass the application name as `AppID` and the key as `Key`.
You should specify the host name and the port number by `Host` (otherwise it'll connect to the official pusher server).

Example:

```go
	client := pusher.Client{
		AppID:   "testapp",
		Key:     "1234567890",
		Secret:  "donttellanybody",
		Host:    "localhost:8150",
	}
```

### From pusher.js

When initializing `Pusher`, pass the host name and the port number as `wsHost` and `wsPort` respectively.

Example:

```js
    var pusher = new Pusher('1234567890', {
      wsHost: 'localhost',
      wsPort: 8150,
    });
```


## Testing

To run the unit test in full, you need Chrome and Redis server installed on the test machine.
By default, `go test` just runs the tests that don't require Chrome nor Redis, but they are very limited.
You can give the following build tags to cover the rest of the tests:

- `chrome_test` includes tests using `Chrome` subprocess via `chromedp`.
- `redis_test` includes tests using Redis server. You have to tweak `config/sample-redis-test.json`
   to match your Redis server configuration.
- `redis_sentinel_test` includes tests using Redis Sentinel cluster. You have to tweak `config/sample-redis-sentinel-test.json`
   to match your Redis Sentinel cluster configuration.

The following command runs the full test:

```text
go test -tags="chrome_test redis_test" ./... ; go test -tags=redis_sentinel_test ./...
```


## License

The MIT License (MIT)

See [LICENSE](LICENSE) for details.
