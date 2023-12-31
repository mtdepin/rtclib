package offer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/pion/webrtc/v3"
	"gitlab.mty.wang/kepler/rtclib/logger"
)

var DefaultICEServer = []webrtc.ICEServer{
	{
		URLs:       []string{"turn:stun.mty.wang:3478"},
		Username:   "user",
		Credential: "123456",
	},
	{
		URLs: []string{"stun:stun.mty.wang:3478"},
	},
}

type RTCHost struct {
	hostId     string
	signalUrl  string
	iceServers []webrtc.ICEServer
	conn       net.Conn
	peer       *Peer
	onPeer     func(*Peer)
	onClose    func()
	State      string
	Predict    int
}

func NewRTCHost(hostId string, signalUrl string, iceServers *[]webrtc.ICEServer) (*RTCHost, error) {
	host := RTCHost{
		hostId:    hostId,
		signalUrl: signalUrl,
		State:     "init",
	}
	if iceServers != nil {
		host.iceServers = append(host.iceServers, *iceServers...)
	} else {
		host.iceServers = append(host.iceServers, DefaultICEServer...)
	}

	return &host, nil
}

func (h *RTCHost) ConnectSignal(peerId string) error {
	ctx := context.Background()

	conn, _, _, err := ws.DefaultDialer.Dial(ctx, h.signalUrl)
	if err != nil {
		return err
	}
	h.conn = conn

	err = h.connectPeer(peerId)
	if err != nil {
		conn.Close()
		return err
	}

	go func() {
		defer conn.Close()

		for {
			data, _, err := wsutil.ReadServerData(h.conn)
			if err != nil {
				logger.Info("wsutil.ReadServerData", err)
				break
			}

			err = h.handleMessage(data)
			if err != nil {
				logger.Info("handleMessage", err)
				continue
			}
		}
	}()

	return nil
}

func (h *RTCHost) connectPeer(peerId string) error {
	message := make(map[string]string)
	message["peerId"] = h.hostId
	message["op"] = "connect"
	message["answerId"] = peerId
	if h.Predict != 0 {
		message["predict"] = fmt.Sprintf("%d", h.Predict)
	}

	return h.sendToSignal(message)
}

func (h *RTCHost) sendToSignal(message map[string]string) error {
	logger.Info("sendToSignal", message)
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}

	err = wsutil.WriteClientText(h.conn, data)
	if err != nil {
		return err
	}
	return nil
}

func (h *RTCHost) OnPeer(f func(*Peer)) {
	h.onPeer = f
}

func (h *RTCHost) OnClose(f func()) {
	h.onClose = f
}

func (h *RTCHost) handleMessage(data []byte) error {
	message := make(map[string]string)
	err := json.Unmarshal(data, &message)
	if err != nil {
		return err
	}
	logger.Info("handleMessage:", message)

	peerId, ok := message["peerId"]
	if !ok {
		return errors.New("invalid data")
	}
	op, ok := message["op"]
	if !ok {
		return errors.New("invalid data")
	}
	switch op {
	case "accept":
		if r, ok := message["result"]; ok {
			if r == "ok" {
				h.State = "accepted"
			}
		}

		err = h.createPeer(peerId)
		if err != nil {
			return err
		}

	case "sdp":
		sdp, ok := message["sdp"]
		if !ok {
			return errors.New("invalid data")
		}

		err = h.handleSDP(peerId, sdp)
		if err != nil {
			return err
		}

	case "candidate":
		c, ok := message["candidate"]
		if !ok {
			return errors.New("invalid data")
		}

		err = h.handleCandidate(peerId, c)
		if err != nil {
			return err
		}
	}

	return nil
}

func (h *RTCHost) createPeer(peerId string) error {
	peer, err := NewPeer(peerId, &h.iceServers, func(sd *webrtc.SessionDescription) {
		message := make(map[string]string)
		message["peerId"] = h.hostId
		message["op"] = "sdp"
		message["sdp"] = sd.SDP
		message["answerId"] = peerId
		h.sendToSignal(message)
	})
	if err != nil {
		return err
	}

	peer.OnConnect(func() {
		h.onPeer(peer)
	})

	peer.OnIceConnectFail(func() {
		peer.Close()
		h.peer = nil
		h.State = "iceFail"
	})

	peer.OnClose(func() {
		logger.Info("peer.OnClose")
		h.peer = nil
		h.Close()
		if h.onClose != nil {
			h.onClose()
		}
	})

	h.peer = peer
	return nil
}

func (h *RTCHost) handleSDP(peerId string, sdp string) error {
	if peerId != h.peer.PeerId() {
		return errors.New("invalid peer")
	}
	s := webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp}
	err := h.peer.SetRemoteDescription(s, func(i *webrtc.ICECandidate) {
		c := i.ToJSON().Candidate
		message := make(map[string]string)
		message["peerId"] = h.hostId
		message["op"] = "candidate"
		message["candidate"] = c
		message["answerId"] = peerId
		h.sendToSignal(message)
	})
	if err != nil {
		return err
	}
	return nil
}

func (h *RTCHost) handleCandidate(peerId string, candidate string) error {
	if peerId != h.peer.PeerId() {
		return errors.New("invalid peer")
	}

	err := h.peer.AddICECandidate(candidate)
	if err != nil {
		return err
	}
	return nil
}

func (h *RTCHost) Close() error {
	logger.Info("h.Close")
	err := h.conn.Close()
	if err != nil {
		return err
	}
	if h.peer != nil {
		logger.Info("peer.Close")
		err = h.peer.Close()
		if err == nil {
			h.peer = nil
		}
	}
	return err
}
