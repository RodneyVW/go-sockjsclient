package sockjsclient_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/igm/sockjs-go/v3/sockjs"
	"github.com/third-light/go-sockjsclient"
)

func TestClientWebsocketSimple(t *testing.T) {
	testClientSimple(t, true)
}

func TestClientXHRSimple(t *testing.T) {
	testClientSimple(t, false)
}

func testClientSimple(t *testing.T, useWebsocket bool) {
	const addr = "127.0.0.1:8008"

	msgCh := make(chan []byte)
	rspCh := make(chan []byte)

	// Start the sockjs test server
	closeFunc := setupTestServer(t, addr, func(t *testing.T, session sockjs.Session) {
		// Indicate a message being sent
		msg := "hello world!"
		msgCh <- []byte(msg)

		// Send message to client
		if err := session.Send(msg); err != nil {
			t.Fatalf("error sending message to client: %v", err)
		}

		// Now wait on expected response
		expRsp := <-rspCh

		// Attempt to receive rsp from client
		rsp, err := session.Recv()
		if err != nil {
			t.Fatalf("error receiving message from client: %v", err)
		}

		// Verify response is expected
		if strings.TrimSpace(rsp) != string(expRsp) {
			t.Fatalf("response from client was not as expected: {Expect=%q Response=%q}", string(expRsp), rsp)
		}

		// close both chans
		close(rspCh)
		close(msgCh)
	}, useWebsocket)
	defer closeFunc()

	// Create new client for srv addr
	client := sockjsclient.Client{
		Address: "http://" + addr + "/sockjs",
	}

	// Attempt to connect
	if err := client.Connect(); err != nil {
		t.Fatalf("error connecting to sockjs test server: %v", err)
	}

	// Wait on message being sent
	expMsg := <-msgCh

	// Receive this message
	msg, err := client.ReadMsg()
	if err != nil {
		t.Fatalf("error receiving message from server: %v", err)
	}

	// Check message is expected
	if !bytes.Equal(expMsg, bytes.TrimSpace(msg)) {
		t.Fatalf("message from server was not as expected: {Expect=%q Message=%q}", string(expMsg), string(msg))
	}

	// Indicate a response being sent
	rsp := []byte("is \"ack\" what i'm meant to say?")
	rspCh <- rsp

	// Send this to server
	if err := client.WriteMsg(rsp); err != nil {
		t.Fatalf("error sending message to server: %v", err)
	}

	// Wait for channel close
	<-rspCh
	<-msgCh

	// close connection
	if client.Close() != nil {
		t.Fatal("failed closing connection")
	}
}

// setupTestServer will attempt to setup a new sockjs server on addr with handler func
func setupTestServer(t *testing.T, addr string, h func(*testing.T, sockjs.Session), useWebsocket bool) (closeFunc func()) {
	srv := http.Server{Addr: addr}
	closeFunc = func() { srv.Close() }

	// Set the sockjs server handler
	opts := sockjs.DefaultOptions
	opts.Websocket = useWebsocket
	srv.Handler = sockjs.NewHandler("/sockjs", opts, func(s sockjs.Session) {
		h(t, s)
	})

	// Start HTTP server in goroutine and
	// catch any early (launch-related) errors
	errs := make(chan error)
	go func() {
		err := srv.ListenAndServe()
		select {
		case errs <- err:
			// sent
		default:
			// already returned
		}
		close(errs)
	}()

	select {
	// Errored out
	case err := <-errs:
		if err != nil {
			t.Fatalf("error starting HTTP server: %v", err)
		}

	// Handed off
	case <-time.After(time.Second * 5):
	}

	return
}
