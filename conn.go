package sockjsclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Sockjs client connection error messages
var (
	ErrClosedConnection   = errors.New("sockjsclient: use of a closed connection")
	ErrClosedByRemote     = errors.New("sockjsclient: connection closed by remote")
	ErrClosingConnection  = errors.New("sockjsclient: error closing connection")
	ErrInvalidResponse    = errors.New("sockjsclient: invalid server response")
	ErrUnexpectedResponse = errors.New("sockjsclient: unexpected server response")
	ErrNoHeartbeat        = errors.New("sockjsclient: no heartbeat")
)

// MessageType represents a sockjs message type
type MessageType uint8

// Sockjs message types (0th represents any unhandled, treated as error)
const (
	MessageTypeUnhandled = MessageType(iota)
	MessageTypeHeartbeat = MessageType(iota)
	MessageTypeData      = MessageType(iota)
	MessageTypeOpen      = MessageType(iota)
	MessageTypeClose     = MessageType(iota)
)

// Conn represents a sockjs client connection
type Conn interface {
	// ReadMsg reads the next single data message from the sockjs connection
	ReadMsg() ([]byte, error)

	// WriteMsg writes a block of data messages to the sockjs connection
	WriteMsg(...[]byte) error

	// Close will close the sockjs connection
	Close() error
}

// parseMessage attempts to parse a valid sockjs message from given data
func parseMessage(data []byte) (MessageType, []byte, error) {
	switch data[0] {
	// Heartbeat
	case 'h':
		// check this is valid heartbeat
		if len(bytes.TrimSpace(data[1:])) != 0 {
			return MessageTypeHeartbeat, nil, ErrInvalidResponse
		}
		return MessageTypeHeartbeat, nil, nil

	// Normal message
	case 'a':
		return MessageTypeData, data[1:], nil

	// Session open
	case 'o':
		if len(bytes.TrimSpace(data[1:])) != 0 {
			return MessageTypeOpen, nil, ErrInvalidResponse
		}
		return MessageTypeOpen, nil, nil

	// Session closed
	case 'c':
		var v []interface{}
		if err := json.Unmarshal(data[1:], &v); err == nil && len(v) == 2 {
			return MessageTypeClose, nil, fmt.Errorf("%w (%v, %v)", ErrClosedByRemote, v[0], v[1])
		}
		return MessageTypeClose, nil, fmt.Errorf("%w (extra close data was missing/invalid)", ErrClosedByRemote)

	// Unhandled type
	default:
		return MessageTypeUnhandled, nil, fmt.Errorf("%w: unknown message type '%c'", ErrInvalidResponse, data[0])
	}
}
