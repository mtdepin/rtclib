package main

import (
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	ossginal "os/signal"

	"maitian.com/kepler/rtclib/logger"
	"maitian.com/kepler/rtclib/session"
)

const sendFilePath = "send.txt"
const recvFilePath = "recv.txt"

var (
	doneCh chan struct{}
	msgCh  chan []byte
)

func main() { // nolint:gocognit
	flag.Parse()

	ch := make(chan os.Signal)
	ossginal.Notify(ch, os.Interrupt, os.Kill)

	go func() {
		select {
		case sig := <-ch:
			logger.Infof("Got %s signal. Aborting...\n", sig)
			os.Exit(1)
		}
	}()

	go func() {
		ip := "0.0.0.0:6061"
		if err := http.ListenAndServe(ip, nil); err != nil {
			fmt.Printf("start pprof failed on %s\n", ip)
			os.Exit(1)
		}
	}()

	doneCh = make(chan struct{})
	msgCh = make(chan []byte)

	sess := session.SessionNew()
	sess.Listen(onConnect, onMessage, func(s *session.Session) {
		onClose(s)
		restart()
	})

	select {}
}

func restart() {
	sess := session.SessionNew()
	sess.Listen(onConnect, onMessage, onClose)
}

func sendFile(s *session.Session, path string) {
	f, err := os.Open(path)
	if err != nil {
		logger.Infof("OpenFile error: %v", err)
		return
	}
	defer f.Close()

	buffer := make([]byte, 16384)
	for {
		n, err := f.Read(buffer)
		if err != nil {
			logger.Infof("Read error: %v", err)
			break
		} else {
			s.Send(buffer[:n])
		}
	}
	logger.Infof("Send done.")
	//time.AfterFunc(10*time.Second, func() {
	//	s.Close()
	//})
}

func recvFile(path string, doneCh chan struct{}, msgCh chan []byte) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		logger.Infof("OpenFile error: %v", err)
		return
	}
	defer f.Close()

	for {
		select {
		case <-doneCh:
			logger.Infof("Recv file done.")
			return
		case msg := <-msgCh:
			n, err := f.Write(msg)
			if err != nil {
				logger.Infof("Write error: %v", err)
			} else {
				logger.Infof("Write %d bytes.", n)
			}
		}
	}
}

func onConnect(s *session.Session) {
	logger.Infof("New connect.")

	go recvFile(recvFilePath, doneCh, msgCh)

	go sendFile(s, sendFilePath)
}

func onClose(s *session.Session) {
	logger.Infof("disconnected.")
	doneCh <- struct{}{}
	s.Close()
}

func onMessage(s *session.Session, data []byte) {
	logger.Infof("recv %d bytes", len(data))
	msgCh <- data
}
