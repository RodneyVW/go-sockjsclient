package sockjsclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"
)

type XHRDialer struct {
	// HTTPClient is the underlying http.Client used by
	// the produced XHR conn
	HTTPClient *http.Client
}

func (d *XHRDialer) Dial(addr, serverID, sessionID string, hdrs http.Header) (Conn, *http.Response, error) {
	return d.DialContext(context.Background(), addr, serverID, sessionID, hdrs)
}

func (d *XHRDialer) DialContext(ctx context.Context, addr, serverID, sessionID string, hdrs http.Header) (Conn, *http.Response, error) {
	// Parse a valid transport address
	taddr, err := parseTransportAddr(addr, serverID, sessionID)
	if err != nil {
		return nil, nil, err
	}

	// Ensure an HTTP client is set
	if d.HTTPClient == nil {
		d.HTTPClient = http.DefaultClient
	}

	// Prepare connection endpoints
	readAddr := taddr + "/xhr"
	writeAddr := taddr + "/xhr_send"

	// Attempt opening connection
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		readAddr,
		nil,
	)
	if err != nil {
		return nil, nil, err
	}

	// Send initial request
	rsp, err := d.HTTPClient.Do(req)
	if rsp != nil {
		defer rsp.Body.Close()
	}
	if err != nil {
		return nil, rsp, err
	}

	// Read and validate initial message
	b, err := ioutil.ReadAll(rsp.Body)
	if err != nil {
		return nil, rsp, err
	} else if mt, _, err := parseMessage(b); err != nil || mt != MessageTypeOpen {
		return nil, rsp, fmt.Errorf("%w: opening sockjs session", ErrInvalidResponse)
	}

	// Create new connection with cancel context
	ctx, cncl := context.WithCancel(context.Background())
	conn := &xhrConn{
		client: *d.HTTPClient,
		raddr:  readAddr,
		waddr:  writeAddr,
		cncl:   cncl,
		in:     make(chan interface{}, 10),
		ctx:    ctx,
	}
	go conn.run()

	return conn, rsp, nil
}

// xhrConn represents a sockjs XHR client connection,
// handling data passing, heartbeat and error tracking
type xhrConn struct {
	client http.Client      // our provided HTTP client
	raddr  string           // prepared XHR read endpoint addr
	waddr  string           // prepared XHR write endpoint addr
	cncl   func()           // context cancel
	in     chan interface{} // inbound data/error channel
	ctx    context.Context  // conn context
}

// run starts the read loop and handles final error propagation
func (conn *xhrConn) run() {
	// Start the read loop
	err := conn.readLoop()
	if err == nil {
		panic("closed read loop with nil error")
	}

	// Propagate error
	conn.in <- maskCtxCancelled(conn.ctx, err)
}

// readLoop is the main xhr read routine, handling passing
// of inbound messages ready to be received, and heartbeat checks
func (conn *xhrConn) readLoop() error {
	const defaultTimeout = time.Second * 30

	// Get our own copy with biggest set timeout
	client := conn.client
	if client.Timeout < defaultTimeout {
		client.Timeout = defaultTimeout
	}

	// ensure closed
	defer conn.Close()

loop:
	for {
		// Prepare read request (addr is constant, but checks ctx status)
		req, err := http.NewRequestWithContext(conn.ctx, "POST", conn.raddr, http.NoBody)
		if err != nil {
			return err
		}

		// Perform next read request
		rsp, err := client.Do(req)
		if err != nil {
			return err
		}

		switch rsp.StatusCode {
		// Success!
		case 200:

		// i.e. session not found --> closed
		case 404:
			return fmt.Errorf("%w (no close frame received)", ErrClosedConnection)

		// Unexpected status code
		default:
			return fmt.Errorf("%w (HTTP %d)", ErrUnexpectedResponse, rsp.StatusCode)
		}

		// Read response body and close
		b, err := ioutil.ReadAll(rsp.Body)
		rsp.Body.Close()
		if err != nil {
			return err
		}

		// Parse message type
		mt, b, err := parseMessage(b)
		if err != nil {
			return err
		}

		switch mt {
		// Heartbeat received, continue looping
		case MessageTypeHeartbeat:
			continue loop

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
func (conn *xhrConn) ReadMsg() ([]byte, error) {
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
func (conn *xhrConn) WriteMsg(data ...[]byte) error {
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

	// Prepare new write request (addr is constant, but checks ctx status)
	req, err := http.NewRequestWithContext(conn.ctx, "POST", conn.waddr, bytes.NewReader(b))
	if err != nil {
		conn.cncl() // ensure closed
		return maskCtxCancelled(conn.ctx, err)
	}

	// Prepare and perform the write request
	rsp, err := conn.client.Do(req)
	if err != nil {
		conn.cncl() // ensure closed
		return maskCtxCancelled(conn.ctx, err)
	}
	defer rsp.Body.Close()

	switch rsp.StatusCode {
	// Success!
	case 204:
		return nil

	// i.e. session not found --> closed
	case 404:
		conn.cncl() // ensure closed
		return ErrClosedConnection

	// Unexpected status code
	default:
		conn.cncl() // ensure closed
		return fmt.Errorf("%w (HTTP %d)", ErrUnexpectedResponse, rsp.StatusCode)
	}
}

// Close implements Conn.Close()
func (conn *xhrConn) Close() error {
	conn.cncl()
	return nil
}
