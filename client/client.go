package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/coder/websocket"
)

// readLimit is the largest message the client accepts: 1 MiB, as the
// engine's.
const readLimit = 1 << 20

// eventBuffer is how many of the engine's messages wait for a reader.
const eventBuffer = 64

// Event is a message the engine sent of its own, not a reply.
type Event struct {
	Name string
	Data json.RawMessage
}

// Client is one connection to the engine. Its methods may be called
// concurrently.
type Client struct {
	ws      *websocket.Conn
	events  chan Event
	done    chan struct{}
	mu      sync.Mutex
	next    int64
	pending map[int64]chan reply
	err     error
}

type reply struct {
	Result json.RawMessage
	Error  *Error
}

// Dial connects to the engine listening on socketPath. ctx bounds the
// dial only, not the connection. An engine that does not answer under
// physa.v1 is closed and refused.
func Dial(ctx context.Context, socketPath string) (*Client, error) {
	hc := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
	}}
	ws, _, err := websocket.Dial(ctx, "ws://physa"+Path, &websocket.DialOptions{
		HTTPClient:   hc,
		Subprotocols: []string{Subprotocol},
	})
	if err != nil {
		return nil, err
	}
	if ws.Subprotocol() != Subprotocol {
		// A refusal: the error returned is that the engine does not
		// speak physa.v1, whether or not it answers the close.
		_ = ws.Close(websocket.StatusPolicyViolation, "the "+Subprotocol+" subprotocol is required")
		return nil, errors.New("engine does not speak " + Subprotocol)
	}
	ws.SetReadLimit(readLimit)
	c := &Client{
		ws:      ws,
		events:  make(chan Event, eventBuffer),
		done:    make(chan struct{}),
		pending: map[int64]chan reply{},
	}
	go c.read()
	return c, nil
}

// read is the client's one read loop: it routes replies to their calls by
// id and the engine's own messages to Events. When the connection ends it
// fails every pending call and closes Events.
func (c *Client) read() {
	var err error
	for {
		var data []byte
		_, data, err = c.ws.Read(context.Background())
		if err != nil {
			break
		}
		var m struct {
			ID     *int64          `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *Error          `json:"error"`
			Event  string          `json:"event"`
			Data   json.RawMessage `json:"data"`
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		if m.ID != nil {
			c.mu.Lock()
			ch := c.pending[*m.ID]
			delete(c.pending, *m.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- reply{Result: m.Result, Error: m.Error}
			}
			continue
		}
		if m.Event != "" {
			select {
			case c.events <- Event{Name: m.Event, Data: m.Data}:
			default:
			}
		}
	}
	c.mu.Lock()
	c.err = fmt.Errorf("the connection to the engine closed: %w", err)
	c.pending = map[int64]chan reply{}
	c.mu.Unlock()
	close(c.done)
	close(c.events)
}

// Call calls method with params (nil means {}) and decodes its result into
// result (nil discards it). An error reply returns *Error. A closed
// connection returns an error wrapping the WebSocket close, so
// websocket.CloseStatus tells why it closed.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	raw := json.RawMessage("{}")
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("%s params: %w", method, err)
		}
		raw = b
	}
	ch := make(chan reply, 1)
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return err
	}
	c.next++
	id := c.next
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()
	b, err := json.Marshal(struct {
		ID     int64           `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}{id, method, raw})
	if err != nil {
		return err
	}
	if err := c.ws.Write(ctx, websocket.MessageText, b); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	var r reply
	select {
	case r = <-ch:
	case <-c.done:
		select {
		case r = <-ch: // The reply came just before the connection closed.
		default:
			c.mu.Lock()
			defer c.mu.Unlock()
			return c.err
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	if r.Error != nil {
		return r.Error
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(r.Result, result); err != nil {
		return fmt.Errorf("%s result: %w", method, err)
	}
	return nil
}

// Events returns the messages the engine sends of its own, in order. The
// channel holds 64; when it is full because nothing reads it, a new
// message is dropped. It closes when the connection does.
func (c *Client) Events() <-chan Event { return c.events }

// Close closes the connection with a normal closure and waits for the
// read loop to end. Closing a closed client returns nil.
func (c *Client) Close() error {
	err := c.ws.Close(websocket.StatusNormalClosure, "")
	<-c.done
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
