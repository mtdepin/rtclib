package peer

import (
	"context"
	"encoding/json"
	"errors"
	"net"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/pion/webrtc/v3"
	"maitian.com/kepler/rtclib/logger"
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
	peers      map[string]*Peer
	onPeer     func(*Peer)
	state      string
}

func NewRTCHost(hostId string, signalUrl string, iceServers *[]webrtc.ICEServer) (*RTCHost, error) {
	host := RTCHost{
		hostId:    hostId,
		signalUrl: signalUrl,
		peers:     make(map[string]*Peer),
		state:     "init",
	}
	if iceServers != nil {
		host.iceServers = append(host.iceServers, *iceServers...)
	} else {
		host.iceServers = append(host.iceServers, DefaultICEServer...)
	}

	return &host, nil
}

func (h *RTCHost) ConnectSignal() error {
	ctx := context.Background()

	conn, _, _, err := ws.DefaultDialer.Dial(ctx, h.signalUrl)
	if err != nil {
		return err
	}
	h.conn = conn

	err = h.registerToSignal()
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

func (h *RTCHost) registerToSignal() error {
	message := make(map[string]string)
	message["peerId"] = h.hostId
	message["op"] = "register"

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
	case "register":
		if peerId != h.hostId {
			return nil
		}
		if r, ok := message["result"]; ok {
			if r == "ok" {
				h.state = "registered"
			}
		}

	case "connect":
		err = h.handlePeerConnection(peerId)
		if err != nil {
			return err
		}

		resp := make(map[string]string)
		resp["peerId"] = h.hostId
		resp["op"] = "accept"
		resp["offerId"] = peerId
		resp["result"] = "ok"
		err = h.sendToSignal(resp)
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

func (h *RTCHost) handlePeerConnection(peerId string) error {
	if _, ok := h.peers[peerId]; ok {
		return errors.New("peer exist")
	}
	peer, err := NewPeer(peerId, &h.iceServers)
	if err != nil {
		return err
	}
	h.peers[peerId] = peer
	peer.OnConnect(func() {
		h.onPeer(peer)
	})
	return nil
}

func (h *RTCHost) handleSDP(peerId string, sdp string) error {
	peer, ok := h.peers[peerId]
	if !ok {
		return errors.New("peer not exist")
	}

	s := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdp}
	err := peer.SetRemoteDescription(s, func(sd *webrtc.SessionDescription) {
		message := make(map[string]string)
		message["peerId"] = h.hostId
		message["op"] = "sdp"
		message["sdp"] = sd.SDP
		message["offerId"] = peerId
		h.sendToSignal(message)
	}, func(i *webrtc.ICECandidate) {
		c := i.ToJSON().Candidate
		message := make(map[string]string)
		message["peerId"] = h.hostId
		message["op"] = "candidate"
		message["candidate"] = c
		message["offerId"] = peerId
		h.sendToSignal(message)
	})
	if err != nil {
		return err
	}
	return nil
}

func (h *RTCHost) handleCandidate(peerId string, candidate string) error {
	peer, ok := h.peers[peerId]
	if !ok {
		return errors.New("peer not exist")
	}

	err := peer.AddICECandidate(candidate)
	if err != nil {
		return err
	}
	return nil
}

func (h *RTCHost) Close() error {
	err := h.conn.Close()
	if err != nil {
		return err
	}
	for _, v := range h.peers {
		err = v.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
