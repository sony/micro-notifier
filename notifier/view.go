package notifier

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
)

// NB: JSON mapping of those structures must match pusher API

type listAppsResponse struct {
	Applications []string `json:"applications"`
}

type errorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type errorBody struct {
	Error errorResponse `json:"error"`
}

type channelsResponse struct {
	Channels map[string]channelsResponseItem `json:"channels"`
}

type channelsResponseItem struct {
	UserCount int `json:"user_count"`
}

type userResponse struct {
	User []userResponseItem `json:"users"`
}

type userResponseItem struct {
	ID string `json:"id"`
}

type eventPayload struct {
	Name     string   `json:"name"`
	Channels []string `json:"channels"`
	Data     string   `json:"data"`
	SocketID *string  `json:socket_id,omitempty"`
}

type getChannelResponse struct {
	Occupied          bool `json:"occupied,omitempty"`
	UserCount         int  `json:"user_count,omitempty"`
	SubscriptionCount int  `json:"subscription_count,omitempty"`
}

func returnJSON(w http.ResponseWriter, val interface{}) {
	js, err := json.Marshal(val)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(js)
}

func returnErr(s *Supervisor, w http.ResponseWriter, err error) {
	apperr, ok := err.(*AppError)
	if !ok {
		apperr = &AppError{Code: 500, Message: "not application error"}
	}

	if apperr.Code == 500 {
		s.logger.Errorw("Returning Internal Error (500):",
			"message", apperr.Message,
			"error", apperr.Internal)
	} else if apperr.Code >= 400 {
		s.logger.Infow("Returning Client Error:",
			"code", apperr.Code,
			"message", apperr.Error())
	}

	body := errorBody{
		Error: errorResponse{
			Code:    apperr.Code,
			Message: apperr.Error(),
		},
	}
	bodybytes, _ := json.Marshal(body)
	http.Error(w, string(bodybytes), apperr.Code)
}

func (s *Supervisor) listApplications(w http.ResponseWriter, r *http.Request) {
	apps := make([]string, 0)
	for _, a := range s.Apps {
		apps = append(apps, a.Name)
	}
	returnJSON(w, listAppsResponse{Applications: apps})
}

func (s *Supervisor) appChannels(w http.ResponseWriter, r *http.Request) {
	channels, apperr := s.GetChannels(mux.Vars(r)["app"])
	if apperr != nil {
		returnErr(s, w, apperr)
		return
	}
	cl := channelsResponse{Channels: make(map[string]channelsResponseItem)}
	for _, c := range channels {
		cl.Channels[c.Name] =
			channelsResponseItem{UserCount: c.UserCount()}
	}
	returnJSON(w, cl)
}

func (s *Supervisor) getChannel(w http.ResponseWriter, r *http.Request) {
	ch, apperr := s.GetOrCreateChannel(mux.Vars(r)["app"],
		mux.Vars(r)["chan"])
	if apperr != nil {
		returnErr(s, w, apperr)
		return
	}
	resp := &getChannelResponse{Occupied: ch.UserCount() > 0, SubscriptionCount: ch.SubscriptionCount()}
	if strings.HasPrefix(ch.Name, "presence-") {
		resp.UserCount = ch.UserCount()
	}
	returnJSON(w, resp)
}

func (s *Supervisor) getChannelUsers(w http.ResponseWriter, r *http.Request) {
	ch, apperr := s.GetOrCreateChannel(mux.Vars(r)["app"],
		mux.Vars(r)["chan"])
	if apperr != nil {
		returnErr(s, w, apperr)
		return
	}

	us := userResponse{}
	for uid := range ch.Users {
		uu := userResponseItem{ID: string(uid)}
		us.User = append(us.User, uu)
	}
	returnJSON(w, us)
}

func (s *Supervisor) trigger(w http.ResponseWriter, r *http.Request) {
	var ev eventPayload
	err := json.NewDecoder(r.Body).Decode(&ev)
	if err != nil {
		returnErr(s, w, WrapErr(400, err))
		return
	}

	a, apperr := s.GetApp(mux.Vars(r)["app"])
	if apperr != nil {
		returnErr(s, w, apperr)
		return
	}

	e := &Event{Name: ev.Name, Data: ev.Data}

	for _, cn := range ev.Channels {
		apperr := s.Broadcast(a, e, cn)
		if apperr != nil {
			s.logger.Errorw("Broadcast error",
				"app", a.Name,
				"channel", cn,
				"error", apperr)
		}
	}
	returnJSON(w, nil)
}
