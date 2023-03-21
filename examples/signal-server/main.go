package main

import (
	"fmt"
	"os"

	"gitlab.mty.wang/kepler/rtclib/signal"
)

func main() {
	var addr string
	if len(os.Args) < 2 {
		addr = "localhost:8001"
	} else {
		addr = os.Args[1]
	}
	server := signal.NewSignalServer(addr)

	err := server.Start()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer server.Close()

	select {}
}
