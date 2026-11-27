package client_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/dphaener/physa/client"
	"github.com/dphaener/physa/internal/api"
)

type numIn struct {
	N int `json:"n"`
}

type numOut struct {
	Double int `json:"double"`
}

func double() api.Method {
	return api.Define("test.double", "Doubles a number.", nil, `{"n":2}`, `{"double":4}`,
		func(_ context.Context, _ *api.Conn, in numIn) (numOut, error) { return numOut{in.N * 2}, nil })
}

// listen serves h on a Unix socket in a short temporary directory.
func listen(t *testing.T, h http.Handler, closeFirst func(context.Context)) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "px")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "engine.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatal(err)
	}
	hs := &http.Server{Handler: h}
	served := make(chan struct{})
	go func() {
		defer close(served)
		hs.Serve(ln)
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		hs.Shutdown(ctx)
		if closeFirst != nil {
			closeFirst(ctx)
		}
		<-served
		os.RemoveAll(dir)
	})
	return socket
}

func serve(t *testing.T, methods ...api.Method) (string, *api.Server) {
	srv := api.NewServer(api.NewTable(methods...), api.Hooks{})
	return listen(t, srv, func(ctx context.Context) { srv.Close(ctx) }), srv
}

func dial(t *testing.T, ctx context.Context, socket string) *client.Client {
	t.Helper()
	c, err := client.Dial(ctx, socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func timeout(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestDialContextEnds(t *testing.T) {
	socket, _ := serve(t, double())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	c := dial(t, ctx, socket)
	cancel()
	var out numOut
	if err := c.Call(timeout(t), "test.double", numIn{N: 21}, &out); err != nil {
		t.Fatalf("call after the dial's context ended = %v", err)
	}
	if out.Double != 42 {
		t.Fatalf("double = %d", out.Double)
	}
}

func TestConcurrentCalls(t *testing.T) {
	socket, _ := serve(t, double())
	c := dial(t, timeout(t), socket)
	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out numOut
			if err := c.Call(timeout(t), "test.double", numIn{N: i}, &out); err != nil {
				errs <- err
			} else if out.Double != 2*i {
				errs <- fmt.Errorf("double(%d) = %d", i, out.Double)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestErrorReply(t *testing.T) {
	socket, _ := serve(t, double())
	c := dial(t, timeout(t), socket)
	err := c.Call(timeout(t), "test.double", map[string]string{"n": "two"}, nil)
	var e *api.Error
	ok := errors.As(err, &e)
	if !ok || e.Code != api.CodeBadRequest || e.Message != "params.n: must be an integer" {
		t.Fatalf("err = %#v", err)
	}
}

func TestRefusesNoSubprotocol(t *testing.T) {
	socket := listen(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		ws.Read(r.Context())
	}), nil)
	_, err := client.Dial(timeout(t), socket)
	if err == nil || err.Error() != "engine does not speak physa.v1" {
		t.Fatalf("Dial = %v", err)
	}
}

func TestNoEngine(t *testing.T) {
	dir, err := os.MkdirTemp("", "px")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if _, err := client.Dial(timeout(t), filepath.Join(dir, "engine.sock")); err == nil {
		t.Fatal("Dial found an engine on a missing socket")
	}
}

func TestClosedConnection(t *testing.T) {
	called := make(chan struct{})
	block := api.Define("test.block", "Waits until its connection ends.", nil, `{}`, `{}`,
		func(ctx context.Context, _ *api.Conn, _ struct{}) (struct{}, error) {
			close(called)
			<-ctx.Done()
			return struct{}{}, ctx.Err()
		})
	socket, srv := serve(t, block)
	c := dial(t, timeout(t), socket)
	callErr := make(chan error, 1)
	go func() { callErr <- c.Call(context.Background(), "test.block", nil, nil) }()
	<-called
	if err := srv.Close(timeout(t)); err != nil {
		t.Fatal(err)
	}
	if err := <-callErr; websocket.CloseStatus(err) != websocket.StatusGoingAway {
		t.Fatalf("pending call = %v, want going away", err)
	}
	if _, open := <-c.Events(); open {
		t.Fatal("Events still open after the connection closed")
	}
	if err := c.Call(timeout(t), "api.list", nil, nil); websocket.CloseStatus(err) != websocket.StatusGoingAway {
		t.Fatalf("call on a closed client = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close after the engine closed = %v", err)
	}
}

func TestCloseTwice(t *testing.T) {
	socket, _ := serve(t)
	c, err := client.Dial(timeout(t), socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
}
