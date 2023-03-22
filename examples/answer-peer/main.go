package main

import (
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v3"
	"gitlab.mty.wang/kepler/rtclib/peer"
)

const MaxBufferLen = 16 * 1024

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

	peerDoneChs := make(map[string]chan struct{})

	host.OnPeer(func(p *peer.Peer) {
		peerId := p.PeerId()
		fmt.Println("New peer: ", peerId)

		doneCh := make(chan struct{})
		msgCh := make(chan []byte)
		pathCh := make(chan string)
		peerDoneChs[peerId] = doneCh

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

		go func() {
			if sendOrRecv == "" {
				p.SendText("Helo")
			} else if sendOrRecv == "send" {
				sendFile(sendFilePath, p)
				time.AfterFunc(10*time.Second, func() {
					host.ClosePeer(peerId)
				})
			} else if sendOrRecv == "recv" {
				recvFile(pathCh, doneCh, msgCh)
			}
		}()
	})

	host.OnPeerClose(func(p *peer.Peer) {
		peerId := p.PeerId()
		fmt.Println("Peer Closed:", peerId)
		if ch, ok := peerDoneChs[peerId]; ok {
			close(ch)
			delete(peerDoneChs, peerId)
		}
	})

	err = host.ConnectSignal()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	host.OnSignalClose(func() {
		fmt.Println("Signal disconnected")
	})

	select {}
}

func sendFile(path string, p *peer.Peer) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Println("Open error:", err)
		return
	}
	defer f.Close()

	start := time.Now()

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
	timeElapsed := time.Since(start)
	fmt.Println("timeElapsed:", timeElapsed)
}

func recvFile(pathCh chan string, doneCh chan struct{}, msgCh chan []byte) {
	path := <-pathCh
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		fmt.Println("OpenFile error:", err)
		return
	}
	defer f.Close()

	start := time.Now()

	total := 0
	for {
		select {
		case <-doneCh:
			fmt.Println("Recv file done.")
			fmt.Println("Write total:", total)
			timeElapsed := time.Since(start)
			fmt.Println("timeElapsed:", timeElapsed)
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
