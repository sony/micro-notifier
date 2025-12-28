package notifier

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

// PusherEvent is a struct to receive Pusher message.
// See https://pusher.com/docs/channels/library_auth_reference/pusher-websockets-protocol#events
type PusherEvent struct {
	Event   string `json:"event"`
	Data    string `json:"data"`
	Channel string `json:"channel",omitempty`
}

// ConnectionEstablishedData is a struct to return pusher:connection_established
// event to the client.
type ConnectionEstablishedData struct {
	SocketID        string `json:"socket_id"`
	ActivityTimeout int    `json:"activity_timeout"`
}

func encodePusherEvent(eventName string, chanName string, data any) ([]byte, error) {
	s, ok := data.(string)
	if !ok {
		js, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		s = string(js)
	}
	js, err := json.Marshal(PusherEvent{
		Event:   eventName,
		Channel: chanName,
		Data:    s,
	})
	if err != nil {
		return nil, err
	}
	return js, nil
}

func (s *Supervisor) socketFinish(u *User, logmsg string, err error) {
	if err != nil {
		s.logger.Debugw(logmsg, "uid", u.ID, "err", err)
	} else {
		s.logger.Infow(logmsg, "uid", u.ID)
	}
	apperr := s.RemoveUser(u.App.Name, u.ID)
	if apperr != nil {
		s.logger.Infow("RemoveUser failed", "apperr", apperr)
	}
	_ = u.Connection.Close()
}

func (s *Supervisor) socketSend(u *User, eventName string, chanName string, data any) {
	msg, err := encodePusherEvent(eventName, chanName, data)
	if err != nil {
		s.socketFinish(u, "[internal] marshalling send packet error", err)
		return
	}
	err = u.Connection.WriteMessage(websocket.TextMessage, msg)
	if err != nil {
		s.socketFinish(u, "writeMessage error", err)
	}
}

func (s *Supervisor) socketSendInvalid(u *User, event string, received any) {
	s.logger.Debugw("invalid event",
		"event", event,
		"data", received)
	s.socketSend(u, "pusher:error", "", "unrecognized message")
}

func (s *Supervisor) checkSignature(u *User, channel string, socketID string, auth string) bool {
	appConfig := s.Config.GetApp(u.App.Name)
	if appConfig == nil {
		return false
	}
	signString := socketID + ":" + channel
	digest := hmac.New(sha256.New, []byte(appConfig.Secret))
	_, _ = digest.Write([]byte(signString))
	expected := appConfig.Key + ":" + hex.EncodeToString(digest.Sum(nil))

	s.logger.Infow("authenticate",
		"result", auth == expected,
		"auth", auth,
		"expects", expected)

	return auth == expected
}

func (s *Supervisor) socketSendUnauthorized(u *User) {
	s.logger.Debugw("user unauthorized",
		"user", u.ID, "app", u.App.Name, "socket", u.SocketID)
	s.socketSend(u, "pusher:error", "", "unauthorized")
}

func (s *Supervisor) socketMessageHandleLoop(u *User) {
	for {
		_, p, err := u.Connection.ReadMessage()
		if err != nil {
			s.socketFinish(u, "readMessage error", err)
			return
		}
		s.logger.Infow("received", "message", string(p))

		var ev struct {
			Name string `json:"event"`
			Data any    `json:"data"`
		}
		err = json.Unmarshal(p, &ev)
		if err != nil {
			s.socketFinish(u, "message decode error", err)
			return
		}

		switch ev.Name {
		case "pusher:ping":
			s.socketSend(u, "pusher:pong", "", "ok")
		case "pusher:subscribe":
			m, ok := ev.Data.(map[string]any)
			if !ok {
				s.socketSendInvalid(u, ev.Name, ev.Data)
				break
			}
			channel, ok := m["channel"]
			if !ok {
				s.socketSendInvalid(u, ev.Name, ev.Data)
				break
			}
			s.logger.Debugw("subscribe request",
				"channel", channel)

			if strings.HasPrefix(channel.(string), "private-") {
				auth, ok := m["auth"]
				if !ok || !s.checkSignature(u, channel.(string), u.SocketID, auth.(string)) {
					s.socketSendUnauthorized(u)
					break
				}
			}
			apperr := s.Subscribe(u.App.Name, u.ID, channel.(string))
			if apperr != nil {
				s.socketSendInvalid(u, ev.Name, ev.Data)
				break
			}
			s.socketSend(u, "pusher_internal:subscription_succeeded", channel.(string), "ok")
		case "pusher:unsubscribe":
			m, ok := ev.Data.(map[string]any)
			if !ok {
				s.socketSendInvalid(u, ev.Name, ev.Data)
				break
			}
			channel, ok := m["channel"]
			if !ok {
				s.socketSendInvalid(u, ev.Name, ev.Data)
				break
			}
			s.logger.Debugw("unsubscribe request",
				"channel", channel)
			_ = s.Unsubscribe(u.App.Name, u.ID, channel.(string))
		default:
			s.socketSend(u, "pusher:error", "", "not implemented")
		}
	}
}

func (s *Supervisor) establishConnection(w http.ResponseWriter, r *http.Request) {
	app, apperr := s.GetAppFromKey(mux.Vars(r)["key"])
	if apperr != nil {
		returnErr(s, w, apperr)
		return
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		returnErr(s, w, wrapErr(500, err))
		return
	}

	u, apperr := s.AddUser(app.Name, conn)
	if apperr != nil {
		returnErr(s, w, apperr)
		return
	}

	s.logger.Infow("New connection",
		"app", app.Name,
		"userId", u.ID)

	// Handkshake
	sockid := fmt.Sprintf("%d.%d", rand.Uint64(), rand.Uint64())
	u.SocketID = sockid
	msg, err := encodePusherEvent("pusher:connection_established", "",
		ConnectionEstablishedData{
			SocketID:        sockid,
			ActivityTimeout: 10000,
		})
	if err != nil {
		s.socketFinish(u, "pusher message encoding error", err)
		return
	}

	s.logger.Infow("sending", "msg", msg)

	err = conn.WriteMessage(websocket.TextMessage, msg)
	if err != nil {
		s.socketFinish(u, "pusher write message error", err)
		return
	}

	conn.SetCloseHandler(func(code int, text string) error {
		msg := fmt.Sprintf("peer closed connection (%d): %s",
			code, text)
		s.socketFinish(u, msg, nil)
		return nil
	})

	go s.socketMessageHandleLoop(u)
}

// Broadcast sends out the event to the users who subscribe the given channel.
// In distributed mode, we don't know which process is managing the user,
// so we use Redis Keyspace Notification.
func (s *Supervisor) Broadcast(a *Application, e *Event, cn string) error {
	if s.db != nil {
		s.logger.Debugw("queueing",
			"event", e,
			"channel", cn)
		return s.PublishRedisEvent(&EventRequest{
			Name:        e.Name,
			Data:        e.Data,
			Application: a.Name,
			Channel:     cn})
	}
	return s.realBroadcast(a, e, cn)
}

func (s *Supervisor) realBroadcast(a *Application, e *Event, cn string) error {
	s.logger.Debugw("broadcasting",
		"event", e,
		"channel", cn)
	ch, apperr := s.GetOrCreateChannel(a.Name, cn)
	if apperr != nil {
		return apperr
	}
	for uid := range ch.Users {
		u := a.GetUserByID(uid)
		if u != nil {
			s.socketSend(u, e.Name, cn, e.Data)
		}
	}
	return nil
}
