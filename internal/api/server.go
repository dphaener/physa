package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/coder/websocket"
)

// readLimit is the largest message either side accepts: 1 MiB.
const readLimit = 1 << 20

// Hooks tell the engine what happens on its connections; any may be nil.
// Disconnected runs once a connection's calls have returned too; Called
// runs before each request, with the method it names.
type Hooks struct {
	Connected, Disconnected func()
	Called                  func(method string)
}

// Server serves a table on Path. It tracks every open connection and its
// request goroutines, because http.Server.Shutdown neither closes nor
// waits for a connection upgraded to a WebSocket: the engine calls Close.
type Server struct {
	table    *Table
	hooks    Hooks
	mu       sync.Mutex
	closed   bool
	sessions map[*session]struct{}
	wg       sync.WaitGroup
}

// NewServer serves t, telling h what happens.
func NewServer(t *Table, h Hooks) *Server {
	return &Server{table: t, hooks: h, sessions: map[*session]struct{}{}}
}

// session is one open connection; raw is the connection Accept hijacked.
type session struct {
	ws     *websocket.Conn
	raw    net.Conn
	ctx    context.Context
	cancel context.CancelFunc
	calls  sync.WaitGroup
}

// Conn is the connection a handler's request came on. It stays valid
// after the handler returns, until the connection closes.
type Conn struct {
	s       *session
	mu      sync.Mutex
	replied bool
	after   []func()
}

// Send sends a message of the engine's own, {"event":name,"data":data}.
// It returns an error once the connection has closed.
func (c *Conn) Send(ctx context.Context, event string, data any) error {
	if c.s.ctx.Err() != nil {
		return errors.New("api: the connection has closed")
	}
	b, err := json.Marshal(struct {
		Event string `json:"event"`
		Data  any    `json:"data"`
	}{event, data})
	if err != nil {
		return fmt.Errorf("api: event %s: %w", event, err)
	}
	return c.s.ws.Write(ctx, websocket.MessageText, b)
}

// AfterReply runs fn once the handler's reply is written (or failed to be,
// the connection having closed); called later, it runs fn at once. So
// engine.stop replies before shutdown. fn must only signal: never call
// Server.Close from it, which waits for the goroutine running fn.
func (c *Conn) AfterReply(fn func()) {
	c.mu.Lock()
	if !c.replied {
		c.after = append(c.after, fn)
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	fn()
}

func (c *Conn) replyWritten() {
	c.mu.Lock()
	c.replied = true
	fns := c.after
	c.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// hijackRecorder keeps the connection Accept hijacks, so Close can end it
// even while a close handshake waits on a peer that never reads.
type hijackRecorder struct {
	http.ResponseWriter
	conn net.Conn
}

func (h *hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, rw, err := http.NewResponseController(h.ResponseWriter).Hijack()
	h.conn = c
	return c, rw, err
}

func run(f func()) {
	if f != nil {
		f()
	}
}

// ServeHTTP upgrades GET Path to a WebSocket under physa.v1 and serves it.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != Path || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		http.Error(w, "the engine is stopping", http.StatusServiceUnavailable)
		return
	}
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()

	rec := &hijackRecorder{ResponseWriter: w}
	ws, err := websocket.Accept(rec, r, &websocket.AcceptOptions{Subprotocols: []string{Subprotocol}})
	if err != nil {
		return // Accept has written the HTTP error.
	}
	if ws.Subprotocol() != Subprotocol {
		ws.Close(websocket.StatusPolicyViolation, "the "+Subprotocol+" subprotocol is required")
		return
	}
	ws.SetReadLimit(readLimit)
	ctx, cancel := context.WithCancel(context.Background())
	ss := &session{ws: ws, raw: rec.conn, ctx: ctx, cancel: cancel}
	s.mu.Lock()
	s.sessions[ss] = struct{}{}
	closed := s.closed
	s.mu.Unlock()
	run(s.hooks.Connected)
	if closed {
		ws.Close(websocket.StatusGoingAway, "the engine is stopping")
	} else {
		s.serve(ss)
	}
	cancel()
	ss.calls.Wait()
	ws.CloseNow()
	s.mu.Lock()
	delete(s.sessions, ss)
	s.mu.Unlock()
	run(s.hooks.Disconnected)
}

// serve is the read loop: a request per goroutine; a non-request closes.
func (s *Server) serve(ss *session) {
	for {
		typ, data, err := ss.ws.Read(ss.ctx)
		if err != nil {
			return
		}
		id, method, params, perr := parse(data)
		if typ != websocket.MessageText || id == nil {
			ss.ws.Close(websocket.StatusUnsupportedData, "a request is a JSON text object with a numeric id")
			return
		}
		if s.hooks.Called != nil {
			s.hooks.Called(method)
		}
		ss.calls.Add(1)
		s.wg.Add(1)
		go s.handle(ss, id, method, params, perr)
	}
}

// parse reads a request. A nil id means it is not one; an error means it
// is one to reply bad_request to.
func parse(data []byte) (id json.RawMessage, method string, params json.RawMessage, err error) {
	var m map[string]json.RawMessage
	if json.Unmarshal(data, &m) != nil {
		return nil, "", nil, nil
	}
	id = bytes.TrimSpace(m["id"])
	var n float64
	if len(id) == 0 || (id[0] != '-' && (id[0] < '0' || id[0] > '9')) || json.Unmarshal(id, &n) != nil {
		return nil, "", nil, nil
	}
	if json.Unmarshal(m["method"], &method) != nil || method == "" {
		return id, "", nil, &Error{Code: CodeBadRequest, Message: `"method" must be a non-empty string`}
	}
	return id, method, m["params"], nil
}

// handle answers one request, then runs what its handler asked to after.
func (s *Server) handle(ss *session, id json.RawMessage, method string, params json.RawMessage, perr error) {
	defer s.wg.Done()
	defer ss.calls.Done()
	c := &Conn{s: ss}
	defer c.replyWritten()
	result, err := s.call(ss.ctx, c, method, params, perr)
	var res json.RawMessage
	if err == nil {
		res, err = json.Marshal(result)
	}
	var e *Error
	if err != nil && !errors.As(err, &e) {
		e = &Error{Code: CodeInternal, Message: err.Error()}
	}
	if e != nil {
		res = nil
	}
	b, _ := json.Marshal(struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result,omitempty"`
		Error  *Error          `json:"error,omitempty"`
	}{id, res, e})
	ss.ws.Write(ss.ctx, websocket.MessageText, b)
}

// call runs the method under a recover: a panic replies internal.
func (s *Server) call(ctx context.Context, c *Conn, method string, params json.RawMessage, perr error) (result any, err error) {
	defer func() {
		if p := recover(); p != nil {
			result, err = nil, &Error{Code: CodeInternal, Message: fmt.Sprint(p)}
		}
	}()
	if perr != nil {
		return nil, perr
	}
	m, ok := s.table.byName[method]
	if !ok {
		return nil, &Error{Code: CodeUnknownMethod, Message: fmt.Sprintf("no method %q", method)}
	}
	return m.call(ctx, c, params)
}

func (s *Server) open() []*session {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	list := make([]*session, 0, len(s.sessions))
	for ss := range s.sessions {
		list = append(list, ss)
	}
	return list
}

// Close refuses new connections, closes every open one with going-away in
// parallel, and returns nil once their goroutines have returned. When ctx
// ends first, it closes the rest without a handshake and cancels their
// calls' contexts, so it is bounded even with a client that never reads,
// and returns ctx's error once the goroutines are done.
func (s *Server) Close(ctx context.Context) error {
	var closing sync.WaitGroup
	for _, ss := range s.open() {
		closing.Add(1)
		go func() {
			defer closing.Done()
			ss.ws.Close(websocket.StatusGoingAway, "the engine is stopping")
		}()
	}
	done := make(chan struct{})
	go func() {
		closing.Wait()
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
	}
	for _, ss := range s.open() {
		// CloseNow does nothing while a close handshake is under way, so
		// the hijacked connection is closed first.
		ss.cancel()
		if ss.raw != nil {
			ss.raw.Close()
		}
		ss.ws.CloseNow()
	}
	<-done
	return fmt.Errorf("api: connections closed without a handshake: %w", ctx.Err())
}
