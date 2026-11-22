package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/dphaener/physa/internal/api"
	"github.com/dphaener/physa/internal/apiclient"
)

type echoIn struct {
	Name  string `json:"name" desc:"Who to greet."`
	Times int    `json:"times,omitempty"`
}

type echoOut struct {
	Greeting string `json:"greeting"`
}

func echo() api.Method {
	return api.Define("test.echo", "Greets by name.", nil, `{"name":"Ada"}`, `{"greeting":"hello Ada"}`,
		func(_ context.Context, _ *api.Conn, in echoIn) (echoOut, error) {
			return echoOut{Greeting: "hello " + in.Name}, nil
		})
}

// serve runs a server for t's methods on a real Unix socket in a short
// temporary directory, closing everything when the test ends.
func serve(t *testing.T, h api.Hooks, methods ...api.Method) (socket string, srv *api.Server) {
	t.Helper()
	dir, err := os.MkdirTemp("", "px")
	if err != nil {
		t.Fatal(err)
	}
	socket = filepath.Join(dir, "engine.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatal(err)
	}
	srv = api.NewServer(api.NewTable(methods...), h)
	hs := &http.Server{Handler: srv}
	served := make(chan struct{})
	go func() {
		defer close(served)
		hs.Serve(ln)
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		hs.Shutdown(ctx)
		srv.Close(ctx)
		<-served
		os.RemoveAll(dir)
	})
	return socket, srv
}

func dial(t *testing.T, socket string) *apiclient.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := apiclient.Dial(ctx, socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// rawDial opens a WebSocket on the socket without the API client.
func rawDial(t *testing.T, socket string, subprotocols ...string) *websocket.Conn {
	t.Helper()
	hc := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws://physa"+api.Path, &websocket.DialOptions{HTTPClient: hc, Subprotocols: subprotocols})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.CloseNow() })
	return ws
}

func timeout(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func wantAPIError(t *testing.T, err error, code, message string) {
	t.Helper()
	var e *api.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want *api.Error", err)
	}
	if e.Code != code || e.Message != message {
		t.Fatalf("error = %s: %s, want %s: %s", e.Code, e.Message, code, message)
	}
}

func TestCall(t *testing.T) {
	socket, _ := serve(t, api.Hooks{}, echo())
	c := dial(t, socket)
	var out echoOut
	if err := c.Call(timeout(t), "test.echo", map[string]any{"name": "Ada", "times": 2}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Greeting != "hello Ada" {
		t.Fatalf("greeting = %q", out.Greeting)
	}
}

func TestBadParams(t *testing.T) {
	socket, _ := serve(t, api.Hooks{}, echo())
	c := dial(t, socket)
	cases := []struct {
		params any
		want   string
	}{
		{map[string]any{"name": 5}, "params.name: must be a string"},
		{nil, `params: missing required "name"`},
		{map[string]any{"name": "Ada", "x": 1}, `params: unknown property "x"`},
		{map[string]any{"name": "Ada", "times": 1.5}, "params.times: must be an integer"},
		{[]int{1}, "params: must be an object"},
	}
	for _, tc := range cases {
		err := c.Call(timeout(t), "test.echo", tc.params, nil)
		wantAPIError(t, err, api.CodeBadRequest, tc.want)
	}
}

func TestUnknownMethod(t *testing.T) {
	socket, _ := serve(t, api.Hooks{})
	c := dial(t, socket)
	err := c.Call(timeout(t), "test.nope", nil, nil)
	wantAPIError(t, err, api.CodeUnknownMethod, `no method "test.nope"`)
}

func TestList(t *testing.T) {
	socket, _ := serve(t, api.Hooks{}, echo())
	c := dial(t, socket)
	var out []api.MethodSummary
	if err := c.Call(timeout(t), "api.list", nil, &out); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range out {
		names = append(names, m.Name)
	}
	if got := strings.Join(names, ","); got != "api.describe,api.list,test.echo" {
		t.Fatalf("methods = %s", got)
	}
	if out[2].Summary != "Greets by name." {
		t.Fatalf("summary = %q", out[2].Summary)
	}
}

func TestDescribe(t *testing.T) {
	socket, _ := serve(t, api.Hooks{}, echo())
	c := dial(t, socket)
	var raw json.RawMessage
	if err := c.Call(timeout(t), "api.describe", map[string]string{"name": "test.echo"}, &raw); err != nil {
		t.Fatal(err)
	}
	want := `{"name":"test.echo","summary":"Greets by name.","notes":[],` +
		`"input":{"type":"object","properties":{"name":{"type":"string","description":"Who to greet."},"times":{"type":"integer"}},"required":["name"],"additionalProperties":false},` +
		`"output":{"type":"object","properties":{"greeting":{"type":"string"}},"required":["greeting"],"additionalProperties":false},` +
		`"request_example":"{\"name\":\"Ada\"}","response_example":"{\"greeting\":\"hello Ada\"}"}`
	if string(raw) != want {
		t.Fatalf("describe =\n%s\nwant\n%s", raw, want)
	}
	var d api.Description
	if err := c.Call(timeout(t), "api.describe", map[string]string{"name": "api.describe"}, &d); err != nil {
		t.Fatal(err)
	}
	if d.Input.Properties["name"].Type != "string" || d.Output.Properties["input"].Type != "object" {
		t.Fatalf("api.describe's own schemas = %+v, %+v", d.Input, d.Output)
	}
	err := c.Call(timeout(t), "api.describe", map[string]string{"name": "test.nope"}, nil)
	wantAPIError(t, err, api.CodeBadRequest, `no method "test.nope"`)
}

func TestSubprotocolRequired(t *testing.T) {
	socket, _ := serve(t, api.Hooks{})
	ws := rawDial(t, socket)
	_, _, err := ws.Read(timeout(t))
	if got := websocket.CloseStatus(err); got != websocket.StatusPolicyViolation {
		t.Fatalf("close status = %v (%v), want policy violation", got, err)
	}
	var ce websocket.CloseError
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, api.Subprotocol) {
		t.Fatalf("close reason = %q, want it to name %s", ce.Reason, api.Subprotocol)
	}
}

func TestMalformed(t *testing.T) {
	socket, _ := serve(t, api.Hooks{}, echo())
	t.Run("id without method", func(t *testing.T) {
		ws := rawDial(t, socket, api.Subprotocol)
		if err := ws.Write(timeout(t), websocket.MessageText, []byte(`{"id":7,"params":{}}`)); err != nil {
			t.Fatal(err)
		}
		_, data, err := ws.Read(timeout(t))
		if err != nil {
			t.Fatal(err)
		}
		want := `{"id":7,"error":{"code":"bad_request","message":"\"method\" must be a non-empty string"}}`
		if string(data) != want {
			t.Fatalf("reply = %s, want %s", data, want)
		}
	})
	for name, msg := range map[string]string{
		"not json":    `hello`,
		"not object":  `[1,2]`,
		"no id":       `{"method":"test.echo"}`,
		"string id":   `{"id":"7","method":"test.echo"}`,
		"binary data": `{"id":1,"method":"test.echo"}`,
	} {
		t.Run(name, func(t *testing.T) {
			ws := rawDial(t, socket, api.Subprotocol)
			typ := websocket.MessageText
			if name == "binary data" {
				typ = websocket.MessageBinary
			}
			if err := ws.Write(timeout(t), typ, []byte(msg)); err != nil {
				t.Fatal(err)
			}
			_, _, err := ws.Read(timeout(t))
			if got := websocket.CloseStatus(err); got != websocket.StatusUnsupportedData {
				t.Fatalf("close status = %v (%v), want unsupported data", got, err)
			}
		})
	}
}

func TestAfterReply(t *testing.T) {
	gotReply := make(chan struct{})
	ran := make(chan struct{})
	m := api.Define("test.after", "Runs something after its reply.", nil, `{}`, `{}`,
		func(_ context.Context, c *api.Conn, _ struct{}) (struct{}, error) {
			c.AfterReply(func() {
				// Had fn run before the reply was written, the call below
				// could never return and the test would time out.
				<-gotReply
				close(ran)
			})
			return struct{}{}, nil
		})
	socket, _ := serve(t, api.Hooks{}, m)
	c := dial(t, socket)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Call(ctx, "test.after", nil, nil); err != nil {
		t.Fatal(err)
	}
	close(gotReply)
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("AfterReply's fn never ran")
	}
}

func TestServerPush(t *testing.T) {
	kept := make(chan *api.Conn, 1)
	m := api.Define("test.push", "Sends messages of its own.", nil, `{}`, `{}`,
		func(ctx context.Context, c *api.Conn, _ struct{}) (struct{}, error) {
			if err := c.Send(ctx, "test.ping", map[string]int{"n": 1}); err != nil {
				return struct{}{}, err
			}
			c.AfterReply(func() {
				go func() {
					c.Send(context.Background(), "test.later", map[string]int{"n": 2})
					kept <- c
				}()
			})
			return struct{}{}, nil
		})
	disconnected := make(chan struct{}, 1)
	socket, _ := serve(t, api.Hooks{Disconnected: func() { disconnected <- struct{}{} }}, m)
	c := dial(t, socket)
	if err := c.Call(timeout(t), "test.push", nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`test.ping {"n":1}`, `test.later {"n":2}`} {
		select {
		case ev := <-c.Events():
			if got := ev.Name + " " + string(ev.Data); got != want {
				t.Fatalf("event = %s, want %s", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no %s event", want)
		}
	}
	conn := <-kept
	c.Close()
	<-disconnected
	if err := conn.Send(context.Background(), "test.gone", nil); err == nil {
		t.Fatal("Send on a closed Conn returned nil")
	}
}

func TestServerClose(t *testing.T) {
	called := make(chan struct{})
	returned := make(chan struct{})
	m := api.Define("test.block", "Waits until its connection ends.", nil, `{}`, `{}`,
		func(ctx context.Context, _ *api.Conn, _ struct{}) (struct{}, error) {
			defer close(returned)
			close(called)
			<-ctx.Done()
			time.Sleep(50 * time.Millisecond) // Close must wait for this too.
			return struct{}{}, ctx.Err()
		})
	disconnected := make(chan struct{})
	socket, srv := serve(t, api.Hooks{Disconnected: func() { close(disconnected) }}, m)
	c := dial(t, socket)
	callErr := make(chan error, 1)
	go func() { callErr <- c.Call(context.Background(), "test.block", nil, nil) }()
	<-called
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Close(ctx); err != nil {
		t.Fatalf("Close = %v", err)
	}
	for name, ch := range map[string]chan struct{}{"handler": returned, "Disconnected hook": disconnected} {
		select {
		case <-ch:
		default:
			t.Fatalf("Close returned before the %s did", name)
		}
	}
	err := <-callErr
	if got := websocket.CloseStatus(err); got != websocket.StatusGoingAway {
		t.Fatalf("pending call = %v, want a going-away close", err)
	}
	if _, err := apiclient.Dial(timeout(t), socket); err == nil {
		t.Fatal("a closed server accepted a connection")
	}
}

func TestCloseHungClient(t *testing.T) {
	socket, srv := serve(t, api.Hooks{})
	rawDial(t, socket, api.Subprotocol) // Never read: it cannot answer the close handshake.
	time.Sleep(50 * time.Millisecond)   // Let the server register the connection.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := srv.Close(ctx)
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("Close took %s with a hung client", took)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close = %v, want the deadline it was cut short by", err)
	}
}

func TestHandlerPanic(t *testing.T) {
	m := api.Define("test.panic", "Panics.", nil, `{}`, `{}`,
		func(context.Context, *api.Conn, struct{}) (struct{}, error) { panic("boom") })
	socket, _ := serve(t, api.Hooks{}, m, echo())
	c := dial(t, socket)
	wantAPIError(t, c.Call(timeout(t), "test.panic", nil, nil), api.CodeInternal, "boom")
	if err := c.Call(timeout(t), "test.echo", map[string]string{"name": "Ada"}, nil); err != nil {
		t.Fatalf("call after a panic = %v", err)
	}
}

func TestHooks(t *testing.T) {
	connected := make(chan struct{}, 1)
	calledWith := make(chan string, 1)
	socket, _ := serve(t, api.Hooks{
		Connected: func() { connected <- struct{}{} },
		Called:    func(m string) { calledWith <- m },
	}, echo())
	c := dial(t, socket)
	<-connected
	c.Call(timeout(t), "test.echo", map[string]string{"name": "Ada"}, nil)
	if got := <-calledWith; got != "test.echo" {
		t.Fatalf("Called with %q", got)
	}
}

func TestDefineTwice(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewTable accepted a method defined twice")
		}
	}()
	api.NewTable(echo(), echo())
}
