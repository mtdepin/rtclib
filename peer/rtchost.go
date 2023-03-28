package peer

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"

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

const ReconnectSignalTime = 30

type RTCHost struct {
	hostId        string
	signalUrl     string
	iceServers    []webrtc.ICEServer
	conn          net.Conn
	peers         map[string]*Peer
	peersMux      sync.Mutex
	onPeer        func(*Peer)
	onPeerClose   func(*Peer)
	onSignalClose func()
	signalState   string
	signalMux     sync.Mutex
	signalTimer   *time.Timer
}

func NewRTCHost(hostId string, signalUrl string, iceServers *[]webrtc.ICEServer) (*RTCHost, error) {
	host := RTCHost{
		hostId:      hostId,
		signalUrl:   signalUrl,
		peers:       make(map[string]*Peer),
		signalState: "init",
	}
	if iceServers != nil {
		host.iceServers = append(host.iceServers, *iceServers...)
	} else {
		host.iceServers = append(host.iceServers, DefaultICEServer...)
	}

	return &host, nil
}

func (h *RTCHost) ConnectSignal() error {
	_, err := h.createSignalConnect()
	if err != nil {
		return err
	}

	go h.handleSignalConnect()

	return nil
}

func (h *RTCHost) createSignalConnect() (conn net.Conn, err error) {
	ctx := context.Background()

	conn, _, _, err = ws.DefaultDialer.Dial(ctx, h.signalUrl)
	if err != nil {
		return
	}
	h.conn = conn

	err = h.registerToSignal()
	if err != nil {
		conn.Close()
	}

	return
}

func (h *RTCHost) handleSignalConnect() {
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

	h.conn.Close()
	h.signalMux.Lock()
	if h.signalState == "closed" {
		h.signalMux.Unlock()
		return
	}
	h.signalState = "disconnected"
	h.signalMux.Unlock()
	if h.onSignalClose != nil {
		h.onSignalClose()
	}

	h.signalTimer = time.AfterFunc(time.Second*ReconnectSignalTime, func() {
		err := h.ConnectSignal()
		if err != nil {
			h.signalTimer.Reset(time.Second * ReconnectSignalTime)
		}
	})
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

func (h *RTCHost) OnPeerClose(f func(*Peer)) {
	h.onPeerClose = f
}

func (h *RTCHost) OnSignalClose(f func()) {
	h.onSignalClose = f
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
				h.signalMux.Lock()
				h.signalState = "registered"
				if h.signalTimer != nil {
					h.signalTimer.Stop()
					h.signalTimer = nil
				}
				h.signalMux.Unlock()
			}
		}

	case "connect":
		_, err := h.handlePeerConnection(peerId)
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
			h.ClosePeer(peerId)
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

func (h *RTCHost) addPeer(peer *Peer) {
	h.peersMux.Lock()
	defer h.peersMux.Unlock()
	h.peers[peer.PeerId()] = peer
}

func (h *RTCHost) removePeer(peerId string) {
	h.peersMux.Lock()
	defer h.peersMux.Unlock()
	delete(h.peers, peerId)
}

func (h *RTCHost) handlePeerConnection(peerId string) (*Peer, error) {
	if _, ok := h.peers[peerId]; ok {
		err := h.ClosePeer(peerId)
		if err != nil {
			return nil, err
		}
	}
	peer, err := NewPeer(peerId, &h.iceServers)
	if err != nil {
		return nil, err
	}
	h.addPeer(peer)
	peer.OnConnect(func() {
		if h.onPeer != nil {
			h.onPeer(peer)
		} else {
			peer.Close()
			h.removePeer(peerId)
		}
	})
	peer.OnClose(func() {
		if h.onPeerClose != nil {
			h.onPeerClose(peer)
		}
		h.removePeer(peerId)
	})
	return peer, nil
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
	h.signalMux.Lock()
	var err error
	if h.signalState != "closed" {
		if h.signalState != "disconnected" {
			err = h.conn.Close()
		}
		h.signalState = "closed"
	}
	h.signalMux.Unlock()
	if err != nil {
		return err
	}

	h.peersMux.Lock()
	defer h.peersMux.Unlock()
	for k, v := range h.peers {
		err = v.Close()
		if err != nil {
			return err
		}
		delete(h.peers, k)
	}

	return nil
}

func (h *RTCHost) ClosePeer(peerId string) error {
	h.peersMux.Lock()
	defer h.peersMux.Unlock()
	peer, ok := h.peers[peerId]
	if !ok {
		return errors.New("peer not exist")
	}

	err := peer.Close()
	if err != nil {
		return err
	}

	delete(h.peers, peerId)
	return nil
}
