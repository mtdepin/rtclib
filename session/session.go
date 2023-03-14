package session

import (
	"sync"
	"time"

	"github.com/pion/webrtc/v3"
	"maitian.com/kepler/rtclib/conf"
	"maitian.com/kepler/rtclib/logger"
	"maitian.com/kepler/rtclib/server"
	"maitian.com/kepler/rtclib/utils"
)

const bufferedThreshold = 5 * 1024 * 1024
const maxBufferedAmount = 20 * 1024 * 1024

type ConnectionHandler func(*Session)
type MessageHandler func(*Session, []byte)
type CloseHandler func(*Session)

type Session struct {
	Done              chan struct{}
	peerConnection    *webrtc.PeerConnection
	dataChannel       *webrtc.DataChannel
	onConnection      ConnectionHandler
	onMessage         MessageHandler
	onClose           CloseHandler
	stunServers       []string
	offerCh           chan webrtc.SessionDescription
	candidateCh       chan string
	candidatesMux     sync.Mutex
	pendingCandidates []*webrtc.ICECandidate
	sendMoreCh        chan struct{}
}

func SessionNew() *Session {
	sess := Session{
		Done:        make(chan struct{}),
		stunServers: []string{},
		offerCh:     make(chan webrtc.SessionDescription),
		candidateCh: make(chan string),
		sendMoreCh:  make(chan struct{}),
	}
	return &sess
}

func (s *Session) Listen(onConnection ConnectionHandler, onMessage MessageHandler, onClose CloseHandler) {
	s.peerConnection = s.GenPeerConnection()
	s.onConnection = onConnection
	s.onMessage = onMessage
	s.onClose = onClose
	server.InitConnection()

	go server.SignalConnect(s.offerCh, s.candidateCh)
	go s.handleSignal()
}

func (s *Session) handleSignal() {
	for {
		select {
		case offer := <-s.offerCh:
			{
				logger.Info("recv offer")
				if err := s.peerConnection.SetRemoteDescription(offer); err != nil {
					panic(err)
				}

				answer, err := s.peerConnection.CreateAnswer(nil)
				if err != nil {
					panic(err)
				}

				data := make(map[string]interface{}, 0)
				data["roomId"] = conf.GetConfig().RoomId
				data["id"] = conf.GetConfig().Id
				data["to"] = server.GetCurOfferUserId()
				data["sdpType"] = answer.Type
				data["sdp"] = answer.SDP

				request := make(map[string]interface{}, 0)
				request["data"] = data
				request["type"] = "answer"

				payload := utils.Marshal(request)
				err = server.SendMessage(payload)
				if err != nil {
					logger.Info(err.Error())
				}

				err = s.peerConnection.SetLocalDescription(answer)
				if err != nil {
					panic(err)
				}

				s.candidatesMux.Lock()
				for _, candi := range s.pendingCandidates {
					onICECandidateErr := s.signalCandidate(candi)
					if onICECandidateErr != nil {
						panic(onICECandidateErr)
					}
				}
				s.candidatesMux.Unlock()
			}
		case candidate := <-s.candidateCh:
			logger.Info("recv candidate")
			candi, _ := utils.ParseCandidateFromString(candidate)
			if utils.FilterN2NAddress(candi.Address()) {
				continue
			}

			if candidateErr := s.peerConnection.AddICECandidate(webrtc.ICECandidateInit{Candidate: candidate}); candidateErr != nil {
				panic(candidateErr)
			}
		}
	}
}

func (s *Session) signalCandidate(c *webrtc.ICECandidate) error {
	if utils.FilterN2NAddress(c.Address) {
		return nil
	}

	candidate := []byte(c.ToJSON().Candidate)
	logger.Infof("stats: answer candidate: %s", candidate)

	data := make(map[string]interface{}, 0)
	data["roomId"] = conf.GetConfig().RoomId
	data["to"] = server.GetCurOfferUserId()
	data["candidate"] = string(candidate)

	request := make(map[string]interface{}, 0)
	request["data"] = data
	request["type"] = "candidate"

	payload := utils.Marshal(request)
	err := server.SendMessage(payload)
	if err != nil {
		logger.Info(err.Error())
	}

	return nil
}

func (s *Session) GenPeerConnection() *webrtc.PeerConnection {
	// Everything below is the Pion WebRTC API! Thanks for using it ❤️.

	s.pendingCandidates = make([]*webrtc.ICECandidate, 0)
	// Prepare the configuration
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				// 47.98.195.183 47.110.83.232 "turn:60.12.185.212:3478"
				URLs: []string{"turn:stun.mty.wang:3478", "stun:stun.mty.wang:3478"},
				//URLs:       []string{"turn:stun.mty.wang:3478", "stun:stun.mty.wang:3478"},
				Username:   "user",
				Credential: "123456",
				//CredentialType: webrtc.ICECredentialTypePassword,
			},
		},
	}

	// Create a new RTCPeerConnection
	peerConnection, err := webrtc.NewPeerConnection(config)
	if err != nil {
		panic(err)
	}

	// When an ICE candidate is available send to the other Pion instance
	// the other Pion instance will add this candidate by calling AddICECandidate
	peerConnection.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}

		s.candidatesMux.Lock()
		defer s.candidatesMux.Unlock()

		desc := peerConnection.RemoteDescription()
		if desc == nil {
			s.pendingCandidates = append(s.pendingCandidates, c)
		} else if onICECandidateErr := s.signalCandidate(c); onICECandidateErr != nil {
			panic(onICECandidateErr)
		}
	})

	// Set the handler for Peer connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnConnectionStateChange(func(pcState webrtc.PeerConnectionState) {
		logger.Infof("Peer Connection State has changed: %s\n", pcState.String())

		if pcState == webrtc.PeerConnectionStateFailed {
			// Wait until PeerConnection has had no network activity for 30 seconds or another failure. It may be reconnected using an ICE Restart.
			// Use webrtc.PeerConnectionStateDisconnected if you are interested in detecting faster timeout.
			// Note that the PeerConnection may come back from PeerConnectionStateDisconnected.
			logger.Info("Peer Connection has gone to failed")
		}

		if pcState == webrtc.PeerConnectionStateClosed {
			logger.Info(time.Now().String(), "peer conn is colsing...")
		}

		if pcState == webrtc.PeerConnectionStateConnected {
			logger.Infof("PeerConnectionStateConnected")
		}
	})

	// Register data channel creation handling
	peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
		logger.Infof("New DataChannel %s %d", d.Label(), d.ID())
		s.dataChannel = d
		d.SetBufferedAmountLowThreshold(bufferedThreshold)

		// Register channel opening handling
		d.OnOpen(func() {
			logger.Infof("Data channel '%s'-'%d' open.", d.Label(), d.ID())
			s.onConnection(s)
		})

		// Register text message handling
		d.OnMessage(func(msg webrtc.DataChannelMessage) {
			s.onMessage(s, msg.Data)
		})

		d.OnClose(func() {
			logger.Info("data channel closing...")
			s.onClose(s)
		})

		d.OnBufferedAmountLow(func() {
			s.sendMoreCh <- struct{}{}
		})
	})

	return peerConnection
}

func (s *Session) closePeerConnection(peerConnection *webrtc.PeerConnection) {
	if err := s.peerConnection.Close(); err != nil {
		logger.Infof("cannot close peerConnection: %v\n", err)
	}
}

func (s *Session) Send(data []byte) error {
	if s.dataChannel.BufferedAmount() > maxBufferedAmount {
		select {
		case <-s.sendMoreCh:
		case <-time.After(time.Millisecond * 100):
		}
	}
	err := s.dataChannel.Send(data)
	return err
}

func (s *Session) Close() {
	s.closePeerConnection(s.peerConnection)
	server.Close()
}
