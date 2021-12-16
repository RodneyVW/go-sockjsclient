package sockjsclient

import (
	"context"
	"errors"
	"math/rand"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/gorilla/websocket"
)

var errNoAddressProvided = errors.New("sockjsclient: no address provided")

// parseTransportAddr parses a valid transport address from given base address
// server ID and client session ID. ALL of these must be provided
func parseTransportAddr(addr, serverID, sessionID string) (string, error) {
	// Ensure valid addr
	url, err := url.Parse(addr)
	if err != nil {
		return "", err
	}

	// Ensure IDs are set
	if serverID == "" {
		return "", errors.New("sockjsclient: no server id provided")
	}
	if sessionID == "" {
		return "", errors.New("sockjsclient: no session id provided")
	}

	// Prepare transport address
	taddr := path.Join(url.Host, url.Path, serverID, sessionID)
	if url.Scheme != "" {
		taddr = url.Scheme + "://" + taddr
	}

	return taddr, nil
}

// maskCtxCancelled replaces any context cancelled/timeout errors with ErrClosedConnection
func maskCtxCancelled(ctx context.Context, err error) error {
	if errors.Is(err, ctx.Err()) {
		return ErrClosedConnection
	}
	return err
}

// isWebsocketClosed will check if this received websocket error indicates a closed connection
func isWebsocketClosed(err error) bool {
	_, ok := err.(*websocket.CloseError)
	if ok {
		return true
	} else if err == websocket.ErrCloseSent {
		return true
	} else if strings.Contains(err.Error(), "use of closed network connection") /* net.ErrClosed isn't available in all Go versions, but it points to poll.ErrNetClosing */ {
		return true
	}
	return false
}

// paddedRandomIntn returns a string representation of a padded random int up-to max
func paddedRandomIntn(max int) string {
	ml := len(strconv.Itoa(max))
	ri := rand.Intn(max)
	is := strconv.Itoa(ri)
	if len(is) < ml {
		is = strings.Repeat("0", ml-len(is)) + is
	}
	return is
}
