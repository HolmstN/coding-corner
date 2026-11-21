# TCP Gateway Challenge

Build a TCP gateway. A client connects to it and sends a request, the gateway
gets that request to the right backend, and the response comes back to the
client. That's the MVP.

Get the MVP working first, then grab some bonus work if you're having fun with it.
A tested MVP is a complete submission; bonuses aren't required.

## What you get

Write your gateway in whatever language you want. The supplied tools use Go
1.26 or later and only the standard library:

- [`client`](cmd/client/main.go): connects, sends requests, and prints responses.
- [`backend`](cmd/backend/main.go): replies with its name and what you sent it.
- [`routes.json`](routes.json): maps target names to backend addresses.
- [Acceptance tests](acceptance/gateway_test.go): exercise your gateway as a
  separate process over real TCP sockets.

From the repository root, run these in separate terminals:

```sh
go run ./challenges/tcp-gateway/cmd/backend --name users --port 9001
go run ./challenges/tcp-gateway/cmd/backend --name orders --port 9002
```

Start your gateway on port 7000 with the supplied routes, then:

```sh
go run ./challenges/tcp-gateway/cmd/client --target users --body hello
go run ./challenges/tcp-gateway/cmd/client --target orders --body goodbye --count 3
```

`--count` sends requests sequentially over the same connection.

## Protocol

Every message is a four-byte unsigned big-endian length followed by that many
bytes of UTF-8 JSON. The length counts payload bytes, not the header. The same
format is used on both sides of the gateway. Payloads must be between 1 byte and
1 MiB (1,048,576 bytes).

Request:

```json
{"id": "abc123", "target": "users", "body": "hello"}
```

All three fields are required strings. `id` and `target` must be nonempty.
`body` may be empty. Additional fields needn't be interpreted, but must be
preserved when forwarding.

Response:

```json
{"id": "abc123", "status": "ok", "body": "users got: hello"}
```

An error response has the original request ID, `status: "error"`, and a nonempty
string `body` explaining what happened. Exact error wording isn't tested.

A client may keep its connection open for multiple requests. Requests may also
arrive together in the byte stream: processing them sequentially and replying
in request order is sufficient for the MVP. Parallel processing and out-of-order
responses are optional.

IDs are scoped to a client connection, not globally unique. Different clients
can use the same ID.

## MVP

1. Listen on port 7000 by default and accept TCP connections.
2. Read framed requests, including partial headers, partial payloads, and
   multiple frames buffered together. TCP is a byte stream, not a message queue.
3. Read routes on startup, look up `target`, and forward the original JSON payload
   bytes unchanged to its backend. Opening a new backend connection per request is fine.
4. Return the backend's framed response to the right client with the same ID.
5. Let different clients make progress concurrently.
6. Return an error for an unknown target, a refused backend connection, a backend
   disconnecting without a complete response, or an expired backend deadline.
   A failed request must not prevent later requests on that client connection.
7. Apply a finite deadline covering backend connection, request write, and
   response read. The default is 2,000 ms.
8. Close the client connection for malformed JSON, invalid request fields,
   invalid UTF-8, or an invalid frame length. Log the reason; an error response
   isn't required for invalid requests.
9. Discard incomplete frames when a client disconnects and release its resources.
   Invalid route configuration must fail startup with a useful error.

Routes are a JSON object mapping nonempty target names to `host:port` strings:

```json
{
  "users": "127.0.0.1:9001",
  "orders": "127.0.0.1:9002"
}
```

## Submission and launch contract

Put your solution in `submissions/tcp-gateway/<name>/` (letters, digits, hyphens,
and underscores; start with a letter or digit), including your README and
a `run.sh`. Tests invoke `bash run.sh` from your submission directory. The script
must build if needed and start the gateway in the foreground; use `exec` for the
final launch so signals reach it. Commit source and dependency lockfiles, not
build outputs.

Your launcher must honor:

| Environment variable | Default | Meaning |
| --- | --- | --- |
| `GATEWAY_PORT` | `7000` | TCP listening port |
| `GATEWAY_ROUTES` | none | Path to the routes JSON file; required |
| `GATEWAY_BACKEND_TIMEOUT_MS` | `2000` | Total backend request deadline |

Listen on localhost or all interfaces. CI supplies a temporary routes file,
chooses an available port, and sets the backend deadline to 500 ms. Your gateway
must be ready within 90 seconds, including its build.

Locally, for example:

```sh
GATEWAY_ROUTES="$PWD/challenges/tcp-gateway/routes.json" \
  bash submissions/tcp-gateway/your-name/run.sh
```

The launcher runs from your submission directory in tests, so keep dependency
installation and path handling self-contained. GitHub-hosted Ubuntu provides
common toolchains, and CI explicitly installs Go. If you need additional tools,
install them without elevated privileges in your launcher or propose a shared
workflow change. Never include credentials.

Run the required tests from the repository root:

```sh
GATEWAY_SUBMISSION="$PWD/submissions/tcp-gateway/your-name" \
  go test -count=1 -timeout=5m -v ./challenges/tcp-gateway/acceptance
```

The suite checks routing, Unicode and empty bodies, fragmented frames, batched
frames, concurrent clients with colliding IDs, backend failures and deadlines,
invalid input, and mid-frame disconnects. It also checks startup rejection of
invalid configuration. Passing the suite is required, but isn't a substitute for
your own tests or the code walkthrough.

After the initial build, invalid configuration must be rejected within 10 seconds.
The process-based acceptance launcher requires Linux or macOS.

Without `GATEWAY_SUBMISSION`, the gateway tests are explicitly skipped. Shared
tool tests can be run with `go test ./...`.

## Bonus

Pick whatever looks interesting:

- Parallel requests on one connection, matched up by `id`.
- Load balancing across copies of a backend. Round robin is fine.
- Health checks so you stop sending traffic to a dead backend.
- Reuse backend connections instead of opening one per request.
- Application heartbeats or idle timeouts; explain how these differ from TCP keepalive.
- Rate limiting per client.
- Graceful shutdown: finish in-flight requests before exiting.
- Load testing: report workload, hardware, error rate, connection count, and p99 latency.

## Wrapping up

Submit your PR, then we'll walk through each other's code. Expect questions like:

- What happens if a backend hangs forever?
- What happens if a client sends half a message and disconnects?
- What happens if a client stops reading responses?
- Which tests demonstrate that you handle TCP as a byte stream?
- How would you run two gateways behind a load balancer?

Practice explaining your choices out loud.
