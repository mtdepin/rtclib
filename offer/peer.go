package offer

import (
	"sync"
	"time"

	"github.com/pion/webrtc/v3"
	"gitlab.mty.wang/kepler/rtclib/logger"
)

const (
	MaxBufferedAmount          = 20 * 1024 * 1024
	BufferedAmountLowThreshold = 512 * 1024
)

type Peer struct {
	peerId            string
	peerConnection    *webrtc.PeerConnection
	dataChannel       *webrtc.DataChannel
	pendingCandidates []*webrtc.ICECandidate
	candidatesMux     sync.Mutex
	sendCandidate     func(*webrtc.ICECandidate)
	onMessage         func(webrtc.DataChannelMessage)
	onMessageCh       chan struct{}
	onClose           func()
	onConnect         func()
	onIceConnectFail  func()
	sendMoreCh        chan struct{}
}

func NewPeer(peerId string, iceServers *[]webrtc.ICEServer,
	sendSdp func(*webrtc.SessionDescription)) (*Peer, error) {
	peer := Peer{
		peerId:      peerId,
		sendMoreCh:  make(chan struct{}),
		onMessageCh: make(chan struct{}),
	}

	err := peer.createPeerConnection(iceServers, sendSdp)
	if err != nil {
		return nil, err
	}
	return &peer, nil
}

func (p *Peer) PeerId() string {
	return p.peerId
}

func (p *Peer) createPeerConnection(iceServers *[]webrtc.ICEServer,
	sendSdp func(*webrtc.SessionDescription)) error {
	p.pendingCandidates = make([]*webrtc.ICECandidate, 0)
	config := webrtc.Configuration{
		ICEServers: *iceServers,
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return err
	}

	pc.OnICECandidate(func(i *webrtc.ICECandidate) {
		if i == nil {
			return
		}
		p.candidatesMux.Lock()
		defer p.candidatesMux.Unlock()

		sdp := pc.RemoteDescription()
		if sdp == nil {
			p.pendingCandidates = append(p.pendingCandidates, i)
		} else {
			p.sendCandidate(i)
		}
	})

	pc.OnConnectionStateChange(func(pcs webrtc.PeerConnectionState) {
		logger.Info("PeerConnectionState:", pcs.String())
		if pcs == webrtc.PeerConnectionStateFailed {
			pc.Close()
		} else if pcs == webrtc.PeerConnectionStateClosed {
			if p.onClose != nil {
				p.onClose()
			}
		}
	})

	pc.OnICEConnectionStateChange(func(is webrtc.ICEConnectionState) {
		logger.Info("ICEConnectionState:", is.String())
		if is == webrtc.ICEConnectionStateFailed {
			if p.onIceConnectFail != nil {
				p.onIceConnectFail()
			}
		}
	})

	pc.OnICEGatheringStateChange(func(is webrtc.ICEGathererState) {
		logger.Info("ICEGathererState:", is.String())
	})

	dataChannel, err := pc.CreateDataChannel("data", nil)
	if err != nil {
		return err
	}
	p.dataChannel = dataChannel

	dataChannel.SetBufferedAmountLowThreshold(BufferedAmountLowThreshold)
	dataChannel.OnBufferedAmountLow(func() {
		select {
		case p.sendMoreCh <- struct{}{}:
		default:
		}
	})

	dataChannel.OnOpen(func() {
		logger.Info("dataChannel.OnOpen")
		if p.onConnect == nil {
			panic("dataChannel.OnOpen: p.onConnect == nil")
		}
		p.onConnect()
	})

	dataChannel.OnClose(func() {
		if p.onClose != nil {
			p.onClose()
		}
	})

	dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
		if p.onMessage == nil {
			<-p.onMessageCh
		}
		p.onMessage(msg)
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return err
	}
	logger.Info("CreateOffer")

	pc.SetLocalDescription(offer)
	sendSdp(&offer)

	p.peerConnection = pc
	return nil
}

func (p *Peer) SetRemoteDescription(sdp webrtc.SessionDescription,
	sendCandidate func(*webrtc.ICECandidate)) error {
	p.candidatesMux.Lock()
	defer p.candidatesMux.Unlock()
	pc := p.peerConnection
	err := pc.SetRemoteDescription(sdp)
	if err != nil {
		return err
	}

	for _, c := range p.pendingCandidates {
		sendCandidate(c)
	}
	p.sendCandidate = sendCandidate
	return nil
}

func (p *Peer) AddICECandidate(candidate string) error {
	err := p.peerConnection.AddICECandidate(webrtc.ICECandidateInit{Candidate: candidate})
	if err != nil {
		return err
	}
	return nil
}

func (p *Peer) Close() error {
	return p.peerConnection.Close()
}

func (p *Peer) OnClose(f func()) {
	logger.Info("OnClose")
	p.onClose = f
}

func (p *Peer) OnConnect(f func()) {
	logger.Info("OnConnect")
	p.onConnect = f
}

func (p *Peer) OnIceConnectFail(f func()) {
	p.onIceConnectFail = f
}

func (p *Peer) OnMessage(f func(webrtc.DataChannelMessage)) {
	logger.Info("OnMessage")
	p.onMessage = f
	select {
	case p.onMessageCh <- struct{}{}:
	default:
	}
}

func (p *Peer) threshold() {
	if p.dataChannel.BufferedAmount() > MaxBufferedAmount {
		select {
		case <-p.sendMoreCh:
		case <-time.After(time.Millisecond * 100):
		}
	}
}

func (p *Peer) Send(data []byte) error {
	p.threshold()
	return p.dataChannel.Send(data)
}

func (p *Peer) SendText(s string) error {
	p.threshold()
	return p.dataChannel.SendText(s)
}

func (p *Peer) BufferedAmount() uint64 {
	return p.dataChannel.BufferedAmount()
}
