package signal

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"gitlab.mty.wang/kepler/rtclib/logger"
)

type AnswerPeer struct {
	peerId   string
	seesions map[string]*Session
	conn     net.Conn
	wsOp     ws.OpCode
}

func (p *AnswerPeer) destroy() {
	for _, v := range p.seesions {
		v.destroy()
	}
	p.conn.Close()
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

type SignalServer struct {
	peers    map[string]*AnswerPeer
	bindAddr string
}

func NewSignalServer(bindAddr string) *SignalServer {
	s := SignalServer{
		peers:    make(map[string]*AnswerPeer),
		bindAddr: bindAddr,
	}
	return &s
}

func (s *SignalServer) Start() error {
	err := http.ListenAndServe(s.bindAddr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			return
		}

		go func() {
			defer conn.Close()

			for {
				msg, op, err := wsutil.ReadClientData(conn)
				if err != nil {
					continue
				}

				err = s.handleMessage(msg, conn, op)
				if err != nil {
					continue
				}
			}
		}()
	}))

	return err
}

func (s *SignalServer) Close() {
	for _, v := range s.peers {
		v.destroy()
	}
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
		if p, ok := s.peers[peerId]; ok {
			p.destroy()
			delete(s.peers, peerId)
		}
		peer := AnswerPeer{
			peerId:   peerId,
			seesions: make(map[string]*Session),
		}
		peer.conn = conn
		peer.wsOp = wsOp
		s.peers[peerId] = &peer

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
		peer, ok := s.peers[answerId]
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

		if s, ok := peer.seesions[peerId]; ok {
			s.destroy()
			delete(peer.seesions, peerId)
		}
		sess := Session{
			answerId:  answerId,
			offerId:   peerId,
			offerConn: conn,
			offerWsOp: wsOp,
			state:     "init",
		}
		peer.seesions[peerId] = &sess

	case "accept":
		offerId, ok := message["offerId"]
		if !ok {
			return errors.New("invalid data")
		}

		answer, ok := s.peers[peerId]
		if !ok {
			return errors.New("peer not exist")
		}
		sess, ok := answer.seesions[offerId]
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
			answer, ok := s.peers[peerId]
			if !ok {
				return errors.New("peer not exist")
			}
			sess, ok := answer.seesions[offerId]
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
			answer, ok := s.peers[answerId]
			if !ok {
				return errors.New("peer not exist")
			}
			sess, ok := answer.seesions[peerId]
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
			answer, ok := s.peers[peerId]
			if !ok {
				return errors.New("peer not exist")
			}
			sess, ok := answer.seesions[offerId]
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
			answer, ok := s.peers[answerId]
			if !ok {
				return errors.New("peer not exist")
			}
			sess, ok := answer.seesions[peerId]
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
