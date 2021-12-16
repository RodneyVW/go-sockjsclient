package main

import (
	"flag"
	"fmt"

	"github.com/third-light/go-sockjsclient"
)

func main() {
	addr := flag.String("addr", "", "Sockjs server address")
	ws := flag.Bool("ws", false, "Prefer websocket connection")
	flag.Parse()

	client := sockjsclient.Client{
		Address:     *addr,
		NoWebsocket: !*ws,
	}

	err := client.Connect()
	if err != nil {
		panic(err)
	}

	for {
		b, err := client.ReadMsg()
		if err != nil {
			panic(err)
		}
		fmt.Printf("%s\n", b)
	}
}
