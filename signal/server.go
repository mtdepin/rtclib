package signal

import (
	"encoding/json"
	"errors"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"gitlab.mty.wang/kepler/rtclib/logger"
	"net"
	"net/http"
	"sync"
)

type AnswerPeer struct {
	peerId   string
	sessions sync.Map
	conn     net.Conn
	wsOp     ws.OpCode
}

func (p *AnswerPeer) destroy() {
	p.sessions.Range(func(key, value any) bool {
		if peer, ok := value.(*Session); ok {
			peer.destroy()
		}
		return true
	})
	p.conn.Close()
}
func (p *AnswerPeer) getSession(peerId string) (*Session, bool) {
	if value, ok := p.sessions.Load(peerId); ok {
		if peer, ok := value.(*Session); ok {
			return peer, ok
		}
	}
	return nil, false
}
func (p *AnswerPeer) deleteSession(peerId string) {
	p.sessions.Delete(peerId)
}
func (p *AnswerPeer) storeSession(peerId string, v *Session) {
	p.sessions.Store(peerId, v)
}

type Session struct {
	answerId  string
	offerId   string
	offerConn net.Conn
	offerWsOp ws.OpCode
	state     string
}

func (s *Session) destroy() {
	if s.offerConn != nil {
		s.offerConn.Close()
	}
}
func (s *Session) getRemoteAddr() string {
	if s.offerConn != nil {
		return s.offerConn.RemoteAddr().String()
	}
	return ""
}

type SignalServer struct {
	peers    sync.Map
	bindAddr string
}

func NewSignalServer(bindAddr string) *SignalServer {
	s := SignalServer{
		bindAddr: bindAddr,
	}
	return &s
}

func (s *SignalServer) Start() error {
	err := http.ListenAndServe(s.bindAddr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			logger.Info("UpgradeHTTP:", err)
			return
		}
		remoteAddr := conn.RemoteAddr().String()
		logger.Info("UpgradeHTTP:", remoteAddr)
		go func() {
			defer conn.Close()
			for {
				msg, op, err := wsutil.ReadClientData(conn)
				if err != nil {
					logger.Error("wsutil.ReadClientData:", err, remoteAddr)
					break
				}
				err = s.handleMessage(msg, conn, op)
				if err != nil {
					logger.Error("handleMessage:", err, remoteAddr)
					continue
				}
			}
		}()
	}))

	return err
}

func (s *SignalServer) Close() {
	s.peers.Range(func(key, value any) bool {
		if peer, ok := value.(*AnswerPeer); ok {
			peer.destroy()
		}
		return true
	})
}
func (s *SignalServer) getPeer(peerId string) (*AnswerPeer, bool) {
	if value, ok := s.peers.Load(peerId); ok {
		if peer, ok := value.(*AnswerPeer); ok {
			return peer, ok
		}
	}
	return nil, false
}
func (s *SignalServer) deletePeer(peerId string) {
	s.peers.Delete(peerId)
}
func (s *SignalServer) storePeer(peerId string, v *AnswerPeer) {
	s.peers.Store(peerId, v)
}
func (s *SignalServer) handleMessage(data []byte, conn net.Conn, wsOp ws.OpCode) error {
	message := make(map[string]string, 0)
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
		if p, ok := s.getPeer(peerId); ok {
			p.destroy()
			s.deletePeer(peerId)
		}
		peer := AnswerPeer{
			peerId: peerId,
		}
		peer.conn = conn
		peer.wsOp = wsOp
		s.storePeer(peerId, &peer)

		resp := make(map[string]string)
		resp["peerId"] = peerId
		resp["op"] = "register"
		resp["result"] = "ok"
		respData, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		err = wsutil.WriteServerMessage(conn, wsOp, respData)
		if err != nil {
			return err
		}

	case "connect":
		answerId, ok := message["answerId"]
		if !ok {
			return errors.New("invalid data")
		}
		peer, ok := s.getPeer(answerId)
		if !ok {
			return errors.New("peer not exist")
		}

		req := make(map[string]string)
		req["peerId"] = peerId
		req["op"] = "connect"
		reqData, err := json.Marshal(req)
		if err != nil {
			return err
		}
		err = wsutil.WriteServerMessage(peer.conn, peer.wsOp, reqData)
		if err != nil {
			return err
		}
		if s, ok := peer.getSession(peerId); ok {
			if conn.RemoteAddr().String() != s.getRemoteAddr() {
				logger.Warnf("The peerId address has changed,old %s --->now  %s", s.getRemoteAddr(), conn.RemoteAddr().String())
				s.destroy()
				peer.deleteSession(peerId)
			}
		}
		sess := Session{
			answerId:  answerId,
			offerId:   peerId,
			offerConn: conn,
			offerWsOp: wsOp,
			state:     "init",
		}
		peer.storeSession(peerId, &sess)

	case "accept":
		offerId, ok := message["offerId"]
		if !ok {
			return errors.New("invalid data")
		}
		answer, ok := s.getPeer(peerId)
		if !ok {
			return errors.New("peer not exist")
		}
		sess, ok := answer.getSession(offerId)
		if !ok {
			return errors.New("peer not exist")
		}

		resp := make(map[string]string)
		resp["peerId"] = peerId
		resp["op"] = "accept"
		resp["result"] = "refuse"
		if result, ok := message["result"]; ok {
			resp["result"] = result
		}
		respData, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		err = wsutil.WriteServerMessage(sess.offerConn, sess.offerWsOp, respData)
		if err != nil {
			return err
		}

		sess.state = "connecting"

	case "sdp":
		sdp, ok := message["sdp"]
		if !ok {
			return errors.New("invalid data")
		}

		if offerId, ok := message["offerId"]; ok {
			answer, ok := s.getPeer(peerId)
			if !ok {
				return errors.New("peer not exist")
			}
			sess, ok := answer.getSession(offerId)
			if !ok {
				return errors.New("peer not exist")
			}

			resp := make(map[string]string)
			resp["peerId"] = peerId
			resp["op"] = "sdp"
			resp["sdp"] = sdp
			respData, err := json.Marshal(resp)
			if err != nil {
				return err
			}
			err = wsutil.WriteServerMessage(sess.offerConn, sess.offerWsOp, respData)
			if err != nil {
				return err
			}
		} else if answerId, ok := message["answerId"]; ok {
			answer, ok := s.getPeer(answerId)
			if !ok {
				return errors.New("peer not exist")
			}
			sess, ok := answer.getSession(peerId)
			if !ok {
				return errors.New("peer not exist")
			}
			if sess.state != "connecting" {
				return errors.New("invalid data")
			}

			resp := make(map[string]string)
			resp["peerId"] = peerId
			resp["op"] = "sdp"
			resp["sdp"] = sdp
			respData, err := json.Marshal(resp)
			if err != nil {
				return err
			}
			err = wsutil.WriteServerMessage(answer.conn, answer.wsOp, respData)
			if err != nil {
				return err
			}
		}

	case "candidate":
		candidate, ok := message["candidate"]
		if !ok {
			return errors.New("invalid data")
		}

		if offerId, ok := message["offerId"]; ok {
			answer, ok := s.getPeer(peerId)
			if !ok {
				return errors.New("peer not exist")
			}
			sess, ok := answer.getSession(offerId)
			if !ok {
				return errors.New("peer not exist")
			}

			resp := make(map[string]string)
			resp["peerId"] = peerId
			resp["op"] = "candidate"
			resp["candidate"] = candidate
			respData, err := json.Marshal(resp)
			if err != nil {
				return err
			}
			err = wsutil.WriteServerMessage(sess.offerConn, sess.offerWsOp, respData)
			if err != nil {
				return err
			}
		} else if answerId, ok := message["answerId"]; ok {
			answer, ok := s.getPeer(answerId)
			if !ok {
				return errors.New("peer not exist")
			}
			sess, ok := answer.getSession(peerId)
			if !ok {
				return errors.New("peer not exist")
			}
			if sess.state != "connecting" {
				return errors.New("invalid data")
			}

			resp := make(map[string]string)
			resp["peerId"] = peerId
			resp["op"] = "candidate"
			resp["candidate"] = candidate
			respData, err := json.Marshal(resp)
			if err != nil {
				return err
			}
			err = wsutil.WriteServerMessage(answer.conn, answer.wsOp, respData)
			if err != nil {
				return err
			}
		}
	}
	return err
}
