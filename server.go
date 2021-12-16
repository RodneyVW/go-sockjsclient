package sockjsclient

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path"
)

// GetServerInfo attempts to fetch sockjs ServerInfo for given address and parse server addr
func GetServerInfo(addr string) (*ServerInfo, *url.URL, error) {
	// Ensure valid provided addr
	url, err := url.Parse(addr)
	if err != nil {
		return nil, nil, err
	}

	// Replace any websocket schemes with https for /info endpoint
	if url.Scheme == "ws" {
		url.Scheme = "http"
	} else if url.Scheme == "wss" || url.Scheme == "" {
		url.Scheme = "https"
	}

	// Take copy of url for /info
	u := *url
	u.Path = path.Join(url.Path, "/info")

	// Perform request to endpoint
	rsp, err := http.Get(u.String())
	if err != nil {
		return nil, nil, err
	}
	defer rsp.Body.Close()

	// Decoded received response
	info := ServerInfo{}
	err = json.NewDecoder(rsp.Body).Decode(&info)
	if err != nil {
		return nil, nil, err
	}

	return &info, url, nil
}

// ServerInfo representations an "/info" response from a sockjs server
type ServerInfo struct {
	WebSocket    bool     `json:"websocket"`
	CookieNeeded bool     `json:"cookie_needed"`
	Origins      []string `json:"origins"`
	Entropy      int      `json:"entropy"`
}
