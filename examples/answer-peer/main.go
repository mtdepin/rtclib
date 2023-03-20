package main

import (
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v3"
	"maitian.com/kepler/rtclib/peer"
)

func main() {
	var addr string
	var sendOrRecv string
	var sendFilePath string
	argc := 1
	if len(os.Args) > 1 {
		if os.Args[argc] == "send" || os.Args[argc] == "recv" {
			sendOrRecv = os.Args[argc]
			argc++
			if sendOrRecv == "send" {
				if argc == len(os.Args) {
					fmt.Println("Usage: answer [<send/recv> [sendfile]] [signalUrl]")
					os.Exit(1)
				}
				sendFilePath = os.Args[argc]
				argc++
			}
		}
	}
	if argc == len(os.Args) {
		addr = "ws://localhost:8001"
	} else {
		addr = os.Args[argc]
	}

	uid := uuid.New()
	hostId := uid.String()

	host, err := peer.NewRTCHost(hostId, addr, nil)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	host.OnPeer(func(p *peer.Peer) {
		fmt.Println("New peer: ", p.PeerId())

		doneCh := make(chan struct{})
		msgCh := make(chan []byte)
		pathCh := make(chan string)

		p.OnMessage(func(dcm webrtc.DataChannelMessage) {
			if dcm.IsString {
				fmt.Println(string(dcm.Data))
				if sendOrRecv == "recv" {
					pathCh <- string(dcm.Data)
				}
			} else {
				fmt.Printf("recv %d bytes\n", len(dcm.Data))
				if sendOrRecv == "recv" {
					msgCh <- dcm.Data
				}
			}
		})

		p.OnClose(func() {
			fmt.Println("Peer Closed")
			doneCh <- struct{}{}
		})

		if sendOrRecv == "" {
			p.SendText("Helo")
		} else if sendOrRecv == "send" {
			sendFile(sendFilePath, p)
			time.AfterFunc(10*time.Second, func() {
				host.Close()
			})
		} else if sendOrRecv == "recv" {
			recvFile(pathCh, doneCh, msgCh)
		}
	})

	err = host.ConnectSignal()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	select {}
}

func sendFile(path string, p *peer.Peer) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Println("Open error:", err)
		return
	}
	defer f.Close()

	err = p.SendText(path)
	if err != nil {
		fmt.Println("SendText error:", err)
		return
	}

	buffer := make([]byte, 16384)
	for {
		n, err := f.Read(buffer)
		if err != nil {
			fmt.Println("Read error:", err)
			break
		} else {
			err = p.Send(buffer[:n])
			if err != nil {
				fmt.Println("Send error:", err)
				break
			}
		}
	}
}

func recvFile(pathCh chan string, doneCh chan struct{}, msgCh chan []byte) {
	path := <-pathCh
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		fmt.Println("OpenFile error:", err)
		return
	}
	defer f.Close()

	for {
		select {
		case <-doneCh:
			fmt.Println("Recv file done.")
			return
		case msg := <-msgCh:
			_, err := f.Write(msg)
			if err != nil {
				fmt.Println("Write error:", err)
			}
		}
	}
}
