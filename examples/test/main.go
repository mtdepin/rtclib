package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v3"
	"gitlab.mty.wang/kepler/rtclib/offer"
	"gitlab.mty.wang/kepler/rtclib/peer"
)

const MaxBufferLen = 16 * 1024

var (
	Help             bool
	SignalUrl        string
	ICEServers       *[]webrtc.ICEServer
	SendFilePath     string
	ICEConfigPath    string
	Role             string
	AnswerId         string
	Nthreads         int
	Mode             string
	PenetrationCount int
)

func init() {
	flag.BoolVar(&Help, "h", false, "help.")
	flag.StringVar(&SignalUrl, "s", "ws://signal.mty.wang:8001", "set signal url.")
	flag.StringVar(&SendFilePath, "f", "", "set send file path.")
	flag.StringVar(&ICEConfigPath, "i", "", "ice configure file.")
	flag.StringVar(&Role, "r", "offer", "set offser or answer role.")
	flag.StringVar(&AnswerId, "a", "", "set answer peer id.")
	flag.IntVar(&Nthreads, "n", 0, "set number of threads.")
	flag.StringVar(&Mode, "m", "", "set more task mode.")
	flag.IntVar(&PenetrationCount, "p", 100, "set penetration count.")
	flag.Usage = usage
}

func usage() {
	fmt.Printf("Usage: rtctest [-s signal] [-f file] [-i ice-config] [-r role] [-a answer-id] [-n threads]\nOptions:\n")
	flag.PrintDefaults()
}

func main() {
	flag.Parse()
	if Help {
		flag.Usage()
		os.Exit(0)
	}

	if Role == "offer" && AnswerId == "" {
		fmt.Println("offer need set answer peer id.")
		flag.Usage()
		os.Exit(1)
	}

	if ICEConfigPath != "" {
		s, err := readIceConfig(ICEConfigPath)
		if err != nil {
			fmt.Println("ICE parse error:", err)
			os.Exit(1)
		}
		ICEServers = s
	}

	if Role == "offer" {
		if Mode == "penetration" {
			count := PenetrationCount
			if count < 1 {
				count = 1
			}

			err := penetration(AnswerId, count)
			if err != nil {
				fmt.Println("penetration error:", err)
				os.Exit(1)
			}
		} else {
			n := Nthreads
			if n < 1 {
				n = 1
			}

			wg := sync.WaitGroup{}

			for i := 0; i < n; i++ {
				wg.Add(1)
				go func() {
					err := connectPeer(AnswerId, &wg)
					if err != nil {
						fmt.Println("Connect peer error:", err)
						wg.Done()
					}
				}()
			}

			wg.Wait()
		}

	} else if Role == "answer" {
		err := startAnswer()
		if err != nil {
			fmt.Println("Start answer error:", err)
			os.Exit(1)
		}

		select {}
	}
}

func connectPeer(peerId string, wg *sync.WaitGroup) error {
	uid := uuid.New()
	hostId := uid.String()
	fmt.Println("connectPeer:", peerId, hostId, SignalUrl, ICEServers)

	host, err := offer.NewRTCHost(hostId, SignalUrl, ICEServers)
	if err != nil {
		return err
	}

	host.OnPeer(func(p *offer.Peer) {
		fmt.Printf("host %s connect peer %s suceess.\n", hostId, peerId)

		totalRecv := 0

		p.OnMessage(func(dcm webrtc.DataChannelMessage) {
			if dcm.IsString {
				fmt.Println(string(dcm.Data))
			}
			totalRecv += len(dcm.Data)
		})

		go func() {
			err := sendFile(p.Send)
			if err != nil {
				fmt.Println("sendFile() error:", err)
				return
			}

			for {
				buffered := p.BufferedAmount()
				if buffered == 0 {
					break
				}
				time.Sleep(time.Second)
			}
			time.AfterFunc(10*time.Second, func() {
				host.Close()
			})
		}()
	})

	err = host.ConnectSignal(peerId)
	if err != nil {
		return err
	}

	host.OnClose(func() {
		fmt.Printf("host %s closed, state %s\n", hostId, host.State)
		wg.Done()
	})

	return nil
}

func penetration(peerId string, count int) error {
	uid := uuid.New()
	hostId := uid.String()
	success := 0

	f := func() (time.Duration, error) {
		var elapsed time.Duration
		host, err := offer.NewRTCHost(hostId, SignalUrl, ICEServers)
		if err != nil {
			return elapsed, err
		}
		start := time.Now()

		doneCh := make(chan struct{})

		host.OnPeer(func(p *offer.Peer) {
			elapsed = time.Since(start)
			success += 1
			time.AfterFunc(time.Second, func() {
				host.Close()
			})
		})

		err = host.ConnectSignal(peerId)
		if err != nil {
			return elapsed, err
		}

		host.OnClose(func() {
			fmt.Printf("host %s closed, state %s\n", hostId, host.State)
			close(doneCh)
		})

		select {
		case <-doneCh:
		case <-time.After(30 * time.Second):
			host.Close()
			<-doneCh
		}

		return elapsed, nil
	}

	var totalElapsed time.Duration
	for i := 0; i < count; i++ {
		timeElapsed, err := f()
		if err != nil {
			fmt.Println("error:", err)
			return err
		}
		fmt.Println("timeElapsed:", timeElapsed)
		totalElapsed += timeElapsed
	}
	fmt.Printf("total connect: %d, success %d, elapsed %s\n", count, success, totalElapsed)

	return nil
}

func startAnswer() error {
	uid := uuid.New()
	hostId := uid.String()

	host, err := peer.NewRTCHost(hostId, SignalUrl, ICEServers)
	if err != nil {
		return nil
	}

	host.OnPeer(func(p *peer.Peer) {
		peerId := p.PeerId()
		fmt.Println("New peer: ", peerId)

		p.OnMessage(func(dcm webrtc.DataChannelMessage) {
			if dcm.IsString {
				fmt.Println(string(dcm.Data))
			} else {
				err := p.Send(dcm.Data)
				if err != nil {
					fmt.Printf("Send to %s error: %v", peerId, err)
				}
			}
		})
	})

	host.OnPeerClose(func(p *peer.Peer) {
		peerId := p.PeerId()
		fmt.Println("Peer Closed:", peerId)
	})

	host.OnSignalClose(func() {
		fmt.Println("Signal disconnected")
	})

	err = host.ConnectSignal()
	if err != nil {
		return err
	}

	return nil
}

func sendFile(sendFn func([]byte) error) error {
	f, err := os.Open(SendFilePath)
	if err != nil {
		return err
	}
	defer f.Close()

	buffer := make([]byte, MaxBufferLen)
	var sendErr error
	for {
		_, sendErr := f.Read(buffer)
		if sendErr != nil {
			break
		} else {
			sendErr = sendFn(buffer)
			if sendErr != nil {
				break
			}
		}
	}
	if sendErr != nil {
		return sendErr
	}

	return nil
}

type IceServerConfig struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type IceServersConfig struct {
	IceServers []IceServerConfig `json:"ice_servers"`
}

func readIceConfig(path string) (*[]webrtc.ICEServer, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	conf := IceServersConfig{}
	err = json.Unmarshal(data, &conf)
	if err != nil {
		return nil, err
	}

	iceServers := []webrtc.ICEServer{}
	for _, server := range conf.IceServers {
		iceServer := webrtc.ICEServer{
			URLs:       server.URLs,
			Username:   server.Username,
			Credential: server.Credential,
		}
		iceServers = append(iceServers, iceServer)
	}

	return &iceServers, nil
}
