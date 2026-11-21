package acceptance

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/HolmstN/coding-corner/challenges/tcp-gateway/protocol"
)

const backendTimeout = 500 * time.Millisecond

type gatewayProcess struct {
	cmd     *exec.Cmd
	done    chan struct{}
	waitErr error
	logPath string
}

func startProcess(t *testing.T, submission, routes, port string) *gatewayProcess {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "gateway.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "run.sh")
	cmd.Dir = submission
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GATEWAY_PORT=") &&
			!strings.HasPrefix(entry, "GATEWAY_ROUTES=") &&
			!strings.HasPrefix(entry, "GATEWAY_BACKEND_TIMEOUT_MS=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env,
		"GATEWAY_PORT="+port,
		"GATEWAY_ROUTES="+routes,
		"GATEWAY_BACKEND_TIMEOUT_MS=500",
	)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		t.Fatal(err)
	}
	process := &gatewayProcess{cmd: cmd, done: make(chan struct{}), logPath: logPath}
	go func() {
		process.waitErr = cmd.Wait()
		close(process.done)
	}()
	t.Cleanup(func() {
		// The launcher may leave compiler or gateway children in its process group.
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			t.Errorf("stop gateway process group: %v", err)
		}
		<-process.done
		if err := logFile.Close(); err != nil {
			t.Errorf("close gateway log: %v", err)
		}
		if t.Failed() {
			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Errorf("read gateway log: %v", err)
			} else {
				if len(data) > 8192 {
					data = data[len(data)-8192:]
				}
				t.Logf("gateway log (last 8 KiB):\n%s", data)
			}
		}
	})
	return process
}

func availablePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func writeRoutes(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "routes.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func startGateway(t *testing.T, submission string, routes map[string]string) string {
	t.Helper()
	data, err := json.Marshal(routes)
	if err != nil {
		t.Fatal(err)
	}
	port := availablePort(t)
	process := startProcess(t, submission, writeRoutes(t, data), port)
	address := net.JoinHostPort("127.0.0.1", port)
	deadline := time.NewTimer(90 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-process.done:
			t.Fatalf("gateway exited before readiness: %v", process.waitErr)
		case <-deadline.C:
			t.Fatal("gateway did not listen within 90 seconds")
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
			if err == nil {
				conn.Close()
				return address
			}
		}
	}
}

func startBackend(t *testing.T, handler func(net.Conn)) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	connections := make(map[net.Conn]bool)
	var workers sync.WaitGroup
	acceptDone := make(chan struct{})
	workers.Add(1)
	go func() {
		defer workers.Done()
		defer close(acceptDone)
		for {
			conn, err := listener.Accept()
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					t.Errorf("backend accept: %v", err)
				}
				return
			}
			mu.Lock()
			connections[conn] = true
			mu.Unlock()
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer func() {
					conn.Close()
					mu.Lock()
					delete(connections, conn)
					mu.Unlock()
				}()
				handler(conn)
			}()
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		<-acceptDone
		mu.Lock()
		for conn := range connections {
			conn.Close()
		}
		mu.Unlock()
		workers.Wait()
	})
	return listener.Addr().String()
}

func echoBackend(t *testing.T, name string) string {
	t.Helper()
	return startBackend(t, func(conn net.Conn) {
		for {
			request, err := protocol.ReadRequest(conn)
			if err != nil {
				if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
					t.Errorf("backend received invalid request: %v", err)
				}
				return
			}
			var response bytes.Buffer
			if err := protocol.WriteJSON(&response, protocol.Response{
				ID: request.ID, Status: "ok", Body: name + " got: " + request.Body,
			}); err != nil {
				t.Error(err)
				return
			}
			// Split the backend response too: framing is required on both TCP legs.
			frame := response.Bytes()
			for _, part := range [][]byte{frame[:2], frame[2:5], frame[5:]} {
				if _, err := io.Copy(conn, bytes.NewReader(part)); err != nil {
					if !errors.Is(err, net.ErrClosed) {
						t.Errorf("backend response write: %v", err)
					}
					return
				}
				time.Sleep(time.Millisecond)
			}
		}
	})
}

func connect(t *testing.T, address string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return conn
}

func exchange(conn net.Conn, request protocol.Request) (protocol.Response, error) {
	if err := protocol.WriteJSON(conn, request); err != nil {
		return protocol.Response{}, err
	}
	return protocol.ReadResponse(conn)
}

func expectResponse(t *testing.T, got protocol.Response, err error, want protocol.Response) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v; want %+v", got, want)
	}
}

func expectError(t *testing.T, got protocol.Response, err error, id string) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != id || got.Status != "error" || got.Body == "" {
		t.Fatalf("expected descriptive error with ID %q, got %+v", id, got)
	}
}

func requestFrame(t *testing.T, request protocol.Request) []byte {
	t.Helper()
	var frame bytes.Buffer
	if err := protocol.WriteJSON(&frame, request); err != nil {
		t.Fatal(err)
	}
	return frame.Bytes()
}

func TestGatewayAcceptance(t *testing.T) {
	submission := os.Getenv("GATEWAY_SUBMISSION")
	if submission == "" {
		t.Skip("set GATEWAY_SUBMISSION to run the required gateway acceptance tests")
	}
	submission, err := filepath.Abs(submission)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(submission, "run.sh")); err != nil {
		t.Fatalf("submission must include run.sh: %v", err)
	}
	slowReceived := make(chan struct{}, 16)
	routes := map[string]string{
		"users":  echoBackend(t, "users"),
		"orders": echoBackend(t, "orders"),
		"down":   net.JoinHostPort("127.0.0.1", availablePort(t)),
		"hang": startBackend(t, func(conn net.Conn) {
			if _, err := protocol.ReadRequest(conn); err != nil {
				t.Errorf("hanging backend request: %v", err)
				return
			}
			slowReceived <- struct{}{}
			if _, err := io.Copy(io.Discard, conn); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, syscall.ECONNRESET) {
				t.Errorf("hanging backend connection: %v", err)
			}
		}),
		"truncated": startBackend(t, func(conn net.Conn) {
			if _, err := protocol.ReadRequest(conn); err != nil {
				t.Errorf("truncated backend request: %v", err)
				return
			}
			if _, err := conn.Write([]byte{0, 0, 0, 10, '{'}); err != nil {
				t.Errorf("truncated backend write: %v", err)
			}
		}),
		"boundary": startBackend(t, func(conn net.Conn) {
			request, err := protocol.ReadRequest(conn)
			if err != nil {
				t.Errorf("boundary backend request: %v", err)
				return
			}
			if err := protocol.WriteJSON(conn, protocol.Response{ID: request.ID, Status: "ok", Body: "accepted"}); err != nil {
				t.Errorf("boundary backend response: %v", err)
			}
		}),
	}
	address := startGateway(t, submission, routes)

	t.Run("routing_and_sequential_requests", func(t *testing.T) {
		conn := connect(t, address)
		for i, body := range []string{"hello", "", "\u96ea \u2603 \U0001f680", "last"} {
			target := []string{"users", "orders"}[i%2]
			request := protocol.Request{ID: fmt.Sprintf("route-%d", i), Target: target, Body: body}
			got, err := exchange(conn, request)
			expectResponse(t, got, err, protocol.Response{ID: request.ID, Status: "ok", Body: target + " got: " + body})
		}
	})

	t.Run("fragmented_header_and_payload", func(t *testing.T) {
		conn := connect(t, address)
		request := protocol.Request{ID: "fragment", Target: "users", Body: "split across writes"}
		for _, b := range requestFrame(t, request) {
			if _, err := conn.Write([]byte{b}); err != nil {
				t.Fatal(err)
			}
			time.Sleep(time.Millisecond)
		}
		got, err := protocol.ReadResponse(conn)
		expectResponse(t, got, err, protocol.Response{ID: request.ID, Status: "ok", Body: "users got: " + request.Body})
	})

	t.Run("batched_frames", func(t *testing.T) {
		conn := connect(t, address)
		var batch bytes.Buffer
		requests := []protocol.Request{
			{ID: "batch-1", Target: "users", Body: "one"},
			{ID: "batch-2", Target: "orders", Body: "two"},
			{ID: "batch-3", Target: "users", Body: "three"},
		}
		for _, request := range requests {
			batch.Write(requestFrame(t, request))
		}
		if _, err := io.Copy(conn, &batch); err != nil {
			t.Fatal(err)
		}
		remaining := make(map[string]protocol.Request)
		for _, request := range requests {
			remaining[request.ID] = request
		}
		for range requests {
			got, err := protocol.ReadResponse(conn)
			if err != nil {
				t.Fatal(err)
			}
			request, ok := remaining[got.ID]
			if !ok {
				t.Fatalf("unknown or duplicate response ID: %q", got.ID)
			}
			expectResponse(t, got, nil, protocol.Response{ID: request.ID, Status: "ok", Body: request.Target + " got: " + request.Body})
			delete(remaining, got.ID)
		}
	})

	t.Run("concurrent_clients_with_same_ids", func(t *testing.T) {
		var workers sync.WaitGroup
		errors := make(chan error, 12)
		start := make(chan struct{})
		for client := range 12 {
			workers.Add(1)
			go func() {
				defer workers.Done()
				<-start
				conn, err := net.DialTimeout("tcp", address, 2*time.Second)
				if err != nil {
					errors <- err
					return
				}
				defer conn.Close()
				if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
					errors <- err
					return
				}
				for i := range 3 {
					target := []string{"users", "orders"}[(client+i)%2]
					request := protocol.Request{ID: fmt.Sprintf("shared-%d", i), Target: target, Body: fmt.Sprintf("client-%d-%d", client, i)}
					got, err := exchange(conn, request)
					want := protocol.Response{ID: request.ID, Status: "ok", Body: target + " got: " + request.Body}
					if err != nil || got != want {
						errors <- fmt.Errorf("client %d: got %+v, %v; want %+v", client, got, err, want)
						return
					}
				}
			}()
		}
		close(start)
		workers.Wait()
		close(errors)
		for err := range errors {
			t.Error(err)
		}
	})

	t.Run("backend_errors_and_connection_recovery", func(t *testing.T) {
		conn := connect(t, address)
		for _, target := range []string{"unknown", "down", "hang", "truncated"} {
			start := time.Now()
			got, err := exchange(conn, protocol.Request{ID: target, Target: target, Body: "hello"})
			expectError(t, got, err, target)
			if elapsed := time.Since(start); elapsed > backendTimeout+500*time.Millisecond {
				t.Fatalf("%s response took %s; must respect the 500ms deadline with 500ms scheduling allowance", target, elapsed)
			}
			if target == "hang" {
				select {
				case <-slowReceived:
				default:
					t.Fatal("gateway did not forward the request to the hanging backend")
				}
			}
			got, err = exchange(conn, protocol.Request{ID: "recovery", Target: "users", Body: "still connected"})
			expectResponse(t, got, err, protocol.Response{ID: "recovery", Status: "ok", Body: "users got: still connected"})
		}
	})

	t.Run("hanging_backend_does_not_block_other_clients", func(t *testing.T) {
		slow := connect(t, address)
		if err := protocol.WriteJSON(slow, protocol.Request{ID: "slow", Target: "hang", Body: "wait"}); err != nil {
			t.Fatal(err)
		}
		select {
		case <-slowReceived:
		case <-time.After(time.Second):
			t.Fatal("hanging backend did not receive the request")
		}
		fast := connect(t, address)
		start := time.Now()
		got, err := exchange(fast, protocol.Request{ID: "fast", Target: "users", Body: "independent"})
		expectResponse(t, got, err, protocol.Response{ID: "fast", Status: "ok", Body: "users got: independent"})
		if elapsed := time.Since(start); elapsed >= 400*time.Millisecond {
			t.Fatalf("healthy client took %s while another backend hung; expected less than 400ms", elapsed)
		}
		got, err = protocol.ReadResponse(slow)
		expectError(t, got, err, "slow")
	})

	t.Run("maximum_payload_is_accepted", func(t *testing.T) {
		request := protocol.Request{ID: "limit", Target: "boundary", Body: ""}
		payload, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		request.Body = strings.Repeat("x", protocol.MaxPayload-len(payload))
		frame := requestFrame(t, request)
		if len(frame) != protocol.MaxPayload+4 {
			t.Fatalf("test frame has wrong size: %d", len(frame))
		}
		conn := connect(t, address)
		if _, err := io.Copy(conn, bytes.NewReader(frame)); err != nil {
			t.Fatal(err)
		}
		got, err := protocol.ReadResponse(conn)
		expectResponse(t, got, err, protocol.Response{ID: "limit", Status: "ok", Body: "accepted"})
	})

	t.Run("invalid_requests_close_connection", func(t *testing.T) {
		var tooLarge [4]byte
		binary.BigEndian.PutUint32(tooLarge[:], protocol.MaxPayload+1)
		cases := map[string][]byte{
			"zero_length": {0, 0, 0, 0},
			"oversized":   tooLarge[:],
			"bad_utf8":    {0, 0, 0, 1, 0xff},
		}
		for name, payload := range map[string]string{
			"malformed_json": `{`,
			"missing_id":     `{"target":"users","body":""}`,
			"missing_target": `{"id":"a","body":""}`,
			"missing_body":   `{"id":"a","target":"users"}`,
			"numeric_id":     `{"id":7,"target":"users","body":""}`,
			"numeric_target": `{"id":"a","target":7,"body":""}`,
			"wrong_type":     `{"id":"a","target":"users","body":7}`,
			"null_body":      `{"id":"a","target":"users","body":null}`,
			"empty_id":       `{"id":"","target":"users","body":""}`,
			"empty_target":   `{"id":"a","target":"","body":""}`,
			"null_request":   `null`,
			"array_request":  `[]`,
			"trailing_json":  `{"id":"a","target":"users","body":""} {}`,
		} {
			var frame bytes.Buffer
			if err := protocol.WriteFrame(&frame, []byte(payload)); err != nil {
				t.Fatal(err)
			}
			cases[name] = frame.Bytes()
		}
		for name, frame := range cases {
			t.Run(name, func(t *testing.T) {
				conn := connect(t, address)
				if _, err := io.Copy(conn, bytes.NewReader(frame)); err != nil {
					t.Fatal(err)
				}
				// A gateway may emit an error first, but it must close rather than stall.
				if _, err := io.Copy(io.Discard, conn); err != nil {
					var netErr net.Error
					if errors.As(err, &netErr) && netErr.Timeout() {
						t.Fatal("invalid request connection was not closed within 3 seconds")
					}
					if !errors.Is(err, syscall.ECONNRESET) {
						t.Fatalf("unexpected close error: %v", err)
					}
				}
			})
		}
	})

	t.Run("disconnect_mid_frame_does_not_block_new_clients", func(t *testing.T) {
		for _, partial := range [][]byte{{0, 0}, {0, 0, 0, 100, '{', '"'}} {
			conn := connect(t, address)
			if _, err := conn.Write(partial); err != nil {
				t.Fatal(err)
			}
			if err := conn.Close(); err != nil {
				t.Fatal(err)
			}
		}
		got, err := exchange(connect(t, address), protocol.Request{ID: "after-disconnect", Target: "orders", Body: "alive"})
		expectResponse(t, got, err, protocol.Response{ID: "after-disconnect", Status: "ok", Body: "orders got: alive"})
	})

	t.Run("forward_complete_request_unchanged", func(t *testing.T) {
		payload := []byte(`{"id":"raw","target":"raw","body":"hello","extra":{"keep":true}}`)
		received := make(chan []byte, 1)
		rawRoutes := map[string]string{"raw": startBackend(t, func(conn net.Conn) {
			data, err := protocol.ReadFrame(conn)
			if err != nil {
				t.Errorf("raw backend read: %v", err)
				return
			}
			received <- data
			if err := protocol.WriteJSON(conn, protocol.Response{ID: "raw", Status: "ok", Body: "received"}); err != nil {
				t.Errorf("raw backend response: %v", err)
			}
		})}
		rawAddress := startGateway(t, submission, rawRoutes)
		conn := connect(t, rawAddress)
		if err := protocol.WriteFrame(conn, payload); err != nil {
			t.Fatal(err)
		}
		got, err := protocol.ReadResponse(conn)
		expectResponse(t, got, err, protocol.Response{ID: "raw", Status: "ok", Body: "received"})
		select {
		case data := <-received:
			if !bytes.Equal(data, payload) {
				t.Fatalf("forwarded payload changed: got %s; want %s", data, payload)
			}
		case <-time.After(time.Second):
			t.Fatal("backend did not receive the request")
		}
	})

	t.Run("invalid_routes_fail_startup", func(t *testing.T) {
		for name, config := range map[string]string{
			"malformed": `{`,
			"not_map":   `["127.0.0.1:9001"]`,
			"address":   `{"users":"not-an-address"}`,
		} {
			t.Run(name, func(t *testing.T) {
				process := startProcess(t, submission, writeRoutes(t, []byte(config)), availablePort(t))
				select {
				case <-process.done:
					if process.waitErr == nil {
						t.Fatal("invalid configuration must exit nonzero")
					}
					data, err := os.ReadFile(process.logPath)
					if err != nil {
						t.Fatal(err)
					}
					if len(bytes.TrimSpace(data)) == 0 {
						t.Fatal("invalid configuration must report a useful startup error")
					}
				case <-time.After(10 * time.Second):
					t.Fatal("invalid configuration did not fail startup within 10 seconds after the initial build")
				}
			})
		}
	})
}
