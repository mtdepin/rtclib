package main

import (
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v3"
	"gitlab.mty.wang/kepler/rtclib/offer"
)

const MaxBufferLen = 16 * 1024

func main() {
	var addr string
	var peerId string
	var sendOrRecv string
	var sendFilePath string
	if len(os.Args) < 2 {
		fmt.Println("Usage: offer [<send/recv> [sendfile]] <peerId> [signalUrl]")
		os.Exit(1)
	} else {
		argc := 1
		if os.Args[argc] == "send" || os.Args[argc] == "recv" {
			sendOrRecv = os.Args[argc]
			argc++
			if sendOrRecv == "send" {
				sendFilePath = os.Args[argc]
				argc++
			}
		}
		if argc == len(os.Args) {
			fmt.Println("Usage: offer [<send/recv> [sendfile]] <peerId> [signalUrl]")
			os.Exit(1)
		}
		peerId = os.Args[argc]
		argc++
		if argc == len(os.Args) {
			addr = "ws://localhost:8001"
		} else {
			addr = os.Args[argc]
		}
	}

	uid := uuid.New()
	hostId := uid.String()

	host, err := offer.NewRTCHost(hostId, addr, nil)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	host.OnPeer(func(p *offer.Peer) {
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
				if sendOrRecv == "recv" {
					msgCh <- dcm.Data
				}
			}
		})

		p.OnClose(func() {
			fmt.Println("Peer Closed")
			doneCh <- struct{}{}
		})

		go func() {
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
		}()
	})

	err = host.ConnectSignal(peerId)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	select {}
}

func sendFile(path string, p *offer.Peer) {
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

	buffer := make([]byte, MaxBufferLen)
	total := 0
	for {
		n, err := f.Read(buffer)
		if err != nil {
			fmt.Println("Read error:", err)
			break
		} else {
			if n != MaxBufferLen {
				fmt.Println("Read n:", n)
			}
			err = p.Send(buffer[:n])
			if err != nil {
				fmt.Println("Send error:", err)
				break
			}
			total += n
		}
	}
	fmt.Println("Send total:", total)
	for {
		buffered := p.BufferedAmount()
		fmt.Println("Buffered:", buffered)
		if buffered == 0 {
			break
		}
		time.Sleep(time.Second)
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

	total := 0
	for {
		select {
		case <-doneCh:
			fmt.Println("Recv file done.")
			fmt.Println("Write total:", total)
			return
		case msg := <-msgCh:
			n, err := f.Write(msg)
			if err != nil {
				fmt.Println("Write error:", err)
			}
			if n != MaxBufferLen {
				fmt.Println("Write n:", n)
			}
			total += n
		}
	}
}
