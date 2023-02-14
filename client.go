package sockjsclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gofrs/uuid"
)

// Sockjs client error messages
var (
	ErrClientNotConnected  = errors.New("sockjsclient: client not connected")
	ErrClientCannotConnect = errors.New("sockjsclient: client cannot connect")
	ErrClientCannotDecode  = errors.New("sockjsclient: client cannot decode")
)

type Client struct {
	// Address is the base server address to connection
	Address string

	// Query parameters will be added after /websocket part of socksjs connect uri
	Query map[string]string

	// ServerID is the server ID string to be used in generation of the transport address
	ServerID string

	// SessionID is the session ID string to be used in generation of the transport address
	SessionID string

	// Header defines any HTTP headers to provide on first connect
	Header http.Header

	// WSDialer provides configuration for dialing WS connections
	WSDialer *WSDialer

	// XHRDialer provides configuration for dialing XHR connections
	XHRDialer *XHRDialer

	// NoWebsocket indicates whether to prefer XHR connection over WS
	NoWebsocket bool

	conn Conn        // underlying client connection
	info *ServerInfo // currently connected server info
	mu   sync.Mutex  // protects conn
}

func (c *Client) Connect() error {
	return c.ConnectContext(context.Background())
}

func (c *Client) ConnectContext(ctx context.Context) error {
	// First check we can connect to info endpoint
	info, url, err := GetServerInfo(c.Address)
	if err != nil {
		if c.Address == "" {
			return errNoAddressProvided
		}
		return fmt.Errorf("%w: connecting to info endpoint: %v", ErrClientCannotConnect, err)
	}

	// Check if server + session ID need generating
	if c.ServerID == "" {
		c.ServerID = paddedRandomIntn(999)
	}
	if c.SessionID == "" {
		c.SessionID = uuid.Must(uuid.NewV4()).String()
	}

	// Websocket preferred (and available!)
	var wsErr error
	if !c.NoWebsocket && info.WebSocket {
		// Take copy of URL
		url := *url

		// Set appropriate scheme
		switch url.Scheme {
		case "http":
			url.Scheme = "ws"
		case "https":
			url.Scheme = "wss"
		}

		// Prepare WS dialer
		dialer := c.WSDialer
		if dialer == nil {
			dialer = &WSDialer{}
		}

		// Attempt to dial websocket conn
		wsConn, _, err := dialer.DialContext(
			ctx,
			url.String(),
			c.ServerID,
			c.SessionID,
			c.Header,
			c.Query,
		)

		// On success, set and return
		if err == nil {
			c.mu.Lock()
			c.conn = wsConn
			c.info = info
			c.mu.Unlock()
			return nil
		}

		// Set ws error for below
		wsErr = err
		log.Printf("websocket failed, using fallback: %v\n", err)
	}

	// Prepare XHR dialer
	dialer := c.XHRDialer
	if dialer == nil {
		dialer = &XHRDialer{}
	}

	// Attempt to dial XHR conn
	xhrConn, _, xhrErr := dialer.DialContext(
		ctx,
		url.String(),
		c.ServerID,
		c.SessionID,
		c.Header,
	)

	// On success, set and return
	if xhrErr == nil {
		c.mu.Lock()
		c.conn = xhrConn
		c.info = info
		c.mu.Unlock()
		return nil
	}

	if wsErr != nil {
		// Both websocket AND xhr connections failed
		return fmt.Errorf("%w: connecting to ws, xhr endpoints: %v, %v", ErrClientCannotConnect, wsErr, xhrErr)
	} else {
		// Only xhr connection failed (was only one attempted)
		return fmt.Errorf("%w: connecting to xhr endpoint: %v", ErrClientCannotConnect, xhrErr)
	}
}

// Conn returns the underlying conn (nil if not connected)
func (c *Client) Conn() Conn {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	return conn
}

// IsWebsocket returns whether current connection is via websocket
func (c *Client) IsWebsocket() bool {
	_, ok := c.Conn().(*wsConn)
	return ok
}

// ServerInfo returns ServerInfo related to current conn (empty if not connected)
func (c *Client) ServerInfo() ServerInfo {
	c.mu.Lock()
	info := ServerInfo{}
	if c.info != nil {
		info.CookieNeeded = c.info.CookieNeeded
		info.Entropy = c.info.Entropy
		info.WebSocket = c.info.WebSocket
		info.Origins = c.info.Origins
	}
	c.mu.Unlock()
	return info
}

// ReadMsg will read the next message from the sockjs connection
func (c *Client) ReadMsg() ([]byte, error) {
	conn := c.Conn()
	if conn == nil {
		return nil, ErrClientNotConnected
	}
	return conn.ReadMsg()
}

// WriteMsg will write a message to the sockjs connection
func (c *Client) WriteMsg(msg []byte) error {
	conn := c.Conn()
	if conn == nil {
		return ErrClientNotConnected
	}
	return conn.WriteMsg(msg)
}

// ReadJSON will read next message from the sockjs connection and attempt JSON decode into "v"
func (c *Client) ReadJSON(v interface{}) error {
	b, err := c.ReadMsg()
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// WriteJSON will JSON encode "v" and send to the sockjs connection
func (c *Client) WriteJSON(v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.WriteMsg(b)
}

// Close will close an open sockjs connection
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		c.info = nil
		return err
	}
	return nil
}
