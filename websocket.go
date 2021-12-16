package sockjsclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

type WSDialer struct {
	// Dialer is the underlying websocket dialer used
	// by the produced websocket conn
	Dialer *websocket.Dialer
}

func (d *WSDialer) Dial(addr, serverID, sessionID string, hdrs http.Header) (Conn, *http.Response, error) {
	return d.DialContext(context.Background(), addr, serverID, sessionID, hdrs)
}

func (d *WSDialer) DialContext(ctx context.Context, addr, serverID, sessionID string, hdrs http.Header) (Conn, *http.Response, error) {
	// Parse a valid transport address
	taddr, err := parseTransportAddr(addr, serverID, sessionID)
	if err != nil {
		return nil, nil, err
	}
	taddr += "/websocket" // sockjs websocket endpoint

	// Ensure a dialer is set
	if d.Dialer == nil {
		d.Dialer = websocket.DefaultDialer
	}

	// Attempt to dial websocket endpoint
	ws, rsp, err := d.Dialer.DialContext(ctx, taddr, hdrs)
	if err != nil {
		return nil, rsp, err
	}

	// Read first message from websocket
	_, b, err := ws.ReadMessage()
	if err != nil {
		return nil, rsp, err
	} else if mt, _, err := parseMessage(b); err != nil || mt != MessageTypeOpen {
		return nil, rsp, fmt.Errorf("%w: opening sockjs session", ErrInvalidResponse)
	}

	// Create new connection with cancel context
	ctx, cncl := context.WithCancel(context.Background())
	conn := &wsConn{
		conn: ws,
		in:   make(chan interface{}, 10),
		cncl: cncl,
		ctx:  ctx,
	}
	go conn.run()

	return conn, rsp, nil
}

// wsConn wraps a websocket.Conn to add our own connection
// tracking, error handling and context usage
type wsConn struct {
	conn *websocket.Conn  // underlying ws conn
	in   chan interface{} // inbound data/error channel
	cncl func()           // context cancel
	ctx  context.Context  // conn context
}

// run starts the read loop and handles final error propagation
func (conn *wsConn) run() {
	// Start the read loop
	err := conn.readLoop()
	if err == nil {
		panic("closed read loop with nil error")
	}

	// Propagate err
	conn.in <- maskCtxCancelled(conn.ctx, err)
}

// readLoop is the main ws read routine, handling passing
// of inbound messages ready to be received, and heartbeat checks
func (conn *wsConn) readLoop() error {
	const timeout = time.Second * 30

	heartbeat := make(chan struct{})
	timer := time.NewTimer(timeout)
	defer func() {
		// ensure closed
		conn.Close()

		// drain timer
		if !timer.Stop() {
			<-timer.C
		}

		// stop heartbeat
		close(heartbeat)
	}()

	go func() {
		for {
			select {
			// Connected closed
			case <-conn.ctx.Done():
				return

			// Timed out :(
			case <-timer.C:
				conn.Close() // kill conn
				conn.in <- ErrNoHeartbeat
				return

			// Heartbeat received
			case <-heartbeat:
				timer.Reset(timeout)
			}
		}
	}()

	for {
		// Read next websocket message
		_, b, err := conn.conn.ReadMessage()
		if err != nil {
			// Check for unexpected close
			if isWebsocketClosed(err) {
				return fmt.Errorf("%w (no close frame received): %v", ErrClosedConnection, err)
			}

			return err
		}

		// Parse the received message
		mt, b, err := parseMessage(b)
		if err != nil {
			return err
		}

		switch mt {
		// Update heartbeat chan
		case MessageTypeHeartbeat:
			heartbeat <- struct{}{}

		// Parse message block, pass along
		case MessageTypeData:
			msgs := []string{}
			if err := json.Unmarshal(b, &msgs); err != nil {
				return err
			}
			for _, msg := range msgs {
				conn.in <- []byte(msg)
			}
		}
	}
}

// ReadMsg implements Conn.ReadMsg()
func (conn *wsConn) ReadMsg() ([]byte, error) {
	select {
	// Next message received
	case v := <-conn.in:
		switch v := v.(type) {
		case error:
			return nil, v
		case []byte:
			return v, nil
		default:
			panic("unexpected type down inbound channel")
		}

	// Check if already closed
	case <-conn.ctx.Done():
		return nil, ErrClosedConnection
	}
}

// WriteMsg implements Conn.WriteMsg()
func (conn *wsConn) WriteMsg(data ...[]byte) error {
	// Check if already closed
	if conn.ctx.Err() != nil {
		return ErrClosedConnection
	}

	// Convert to message block
	msgs := make([]string, 0, len(data))
	for _, b := range data {
		msgs = append(msgs, string(b))
	}

	// Marshal message block
	b, err := json.Marshal(msgs)
	if err != nil {
		return err
	}

	if err := conn.conn.WriteMessage(websocket.TextMessage, b); err != nil {
		// Check for expected close
		if conn.ctx.Err() != nil {
			return ErrClosedConnection
		}
		conn.cncl() // ensure closed

		// Wrap close errors with our own
		if isWebsocketClosed(err) {
			return fmt.Errorf("%w: %v", ErrClosedConnection, err)
		}

		return err
	}

	return nil
}

// Close implements Conn.Close()
func (conn *wsConn) Close() error {
	// Check if already closed
	if conn.ctx.Err() != nil {
		return nil
	}

	// Ensure canclled
	defer conn.cncl()

	// Attempt to send final close message
	if err := conn.conn.WriteMessage(websocket.CloseMessage, []byte{}); err != nil {
		if isWebsocketClosed(err) {
			return nil // already closed
		}

		// ignore others, still close
	}

	// Attempt to close the connection
	if err := conn.conn.Close(); err != nil {
		return fmt.Errorf("%w: %v", ErrClosingConnection, err)
	}

	return nil
}
