package server

import (
	"net/url"
	"os"
	ossginal "os/signal"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
	"maitian.com/kepler/rtclib/conf"
	"maitian.com/kepler/rtclib/logger"
	"maitian.com/kepler/rtclib/utils"
)

var c *websocket.Conn
var curOfferUserId string

func InitConnection() {
	u := url.URL{Scheme: "ws", Host: conf.GetConfig().SignalIp + ":" + conf.GetConfig().SignalPort, Path: "/ws"}
	logger.Infof("connecting to %s", u.String())

	var err error
	c, _, err = websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		logger.Error("dial:", err)
	}
	//defer c.Close()
}

func GetConnection() *websocket.Conn {
	return c
}

func Close() {
	c.Close()
}

func GetCurOfferUserId() string {
	return curOfferUserId
}

func SignalConnect(
	offerCh chan webrtc.SessionDescription,
	candidateCh chan string) {

	interrupt := make(chan os.Signal, 1)
	ossginal.Notify(interrupt, os.Interrupt)

	joinRoom()

	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				logger.Info("read:", err)
				return
			}
			logger.Infof("recv: %s", message)

			// 处理数据
			request, err := utils.Unmarshal(string(message))
			if err != nil {
				logger.Infof("解析Json数据Unmarshal错误 %v", err)
				return
			}

			switch request["type"] {
			case "offer":
				var data map[string]interface{} = nil
				tmp, found := request["data"]
				if !found {
					logger.Infof("没有发现数据")
					return
				}
				data = tmp.(map[string]interface{})
				roomId := data["roomId"].(string)
				logger.Infof("房间Id:%v", roomId)
				curOfferUserId = data["id"].(string)
				sdp := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: data["sdp"].(string)}

				offerCh <- sdp
			case "candidate":
				var data map[string]interface{} = nil
				tmp, found := request["data"]
				if !found {
					logger.Infof("没有发现数据")
					return
				}
				data = tmp.(map[string]interface{})
				roomId := data["roomId"].(string)
				logger.Infof("房间Id:%v", roomId)
				candidate := data["candidate"].(string)

				candidateCh <- candidate
			case "updateUserList":
				var data []interface{} = nil
				tmp, found := request["data"]
				if !found {
					logger.Infof("没有发现数据")
					return
				}
				data = tmp.([]interface{})
				logger.Infof("cur roomUsers: %s", data)

				break
			case "leaveRoom":
				break
			default:
				{
					logger.Infof("未知的类型 %v", request["type"])
				}
				break
			}
		}
	}()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		//case t := <-ticker.C:
		//logger.Info("send msg: %s", t.String())
		//err := c.WriteMessage(websocket.TextMessage, []byte(t.String()))
		//if err != nil {
		//	logger.Info("write:", err)
		//	return
		//}
		case <-interrupt:
			logger.Info("interrupt")

			// Cleanly close the connection by sending a close message and then
			// waiting (with timeout) for the server to close the connection.
			err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				logger.Info("write close:", err)
				return
			}
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return
		}
	}
}

func joinRoom() {
	data := make(map[string]interface{}, 0)
	data["roomId"] = conf.GetConfig().RoomId
	data["id"] = conf.GetConfig().Id
	data["name"] = conf.GetConfig().Name

	request := make(map[string]interface{}, 0)
	request["data"] = data
	request["type"] = "joinRoom"

	payload := utils.Marshal(request)
	err := SendMessage(payload)
	if err != nil {
		logger.Info(err.Error())
	}
}

func SendMessage(message string) error {
	return GetConnection().WriteMessage(websocket.TextMessage, []byte(message))
}
