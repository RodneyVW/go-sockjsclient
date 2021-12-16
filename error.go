package sockjsclient

import "errors"

// IsNotConnected will return if this is a client / conn not connected error
func IsNotConnected(err error) bool {
	return errors.Is(err, ErrClosedConnection) || errors.Is(err, ErrClientNotConnected)
}
