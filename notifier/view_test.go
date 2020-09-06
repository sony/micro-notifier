package notifier

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	simplejson "github.com/bitly/go-simplejson"
	"github.com/stretchr/testify/require"
)

// config options
const (
	DefaultConfig = 0
	SecureConfig  = 1
	RedisConfig   = 2
)

func initTest(t *testing.T, kind int) *Supervisor {
	path := "../config/sample.json"
	switch kind {
	case SecureConfig:
		path = "../config/sample-secure.json"
	case RedisConfig:
		path = "../config/sample-redis-test.json"
	}
	config, err := ReadConfigFile(path)
	require.Nil(t, err)

	return NewSupervisor(config)
}

func jsonBody(t *testing.T, rr *httptest.ResponseRecorder) *simplejson.Json {
	json, err := simplejson.NewJson(rr.Body.Bytes())
	require.Nil(t, err)
	return json
}

func J(jsonstr string) *simplejson.Json {
	json, err := simplejson.NewJson([]byte(jsonstr))
	if err != nil {
		log.Fatalf("Json literal parse error: %v", err)
	}
	return json
}

func doRequest(t *testing.T, router http.Handler,
	method string, path string, body string,
	expectedCode int) *httptest.ResponseRecorder {
	req, err := http.NewRequest(method, path, strings.NewReader(body))
	require.Nil(t, err)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, expectedCode, rr.Code,
		fmt.Sprintf("%s %s: %s", method, path, body))
	return rr
}

func TestListApplications(t *testing.T) {
	s := initTest(t, DefaultConfig)
	router := newRouter(s)

	rr := doRequest(t, router, "GET", "/apps", "", http.StatusOK)
	require.Equal(t, J(`{"applications":[`+
		` "testapp",`+
		` "testapp2"`+
		`]}`), jsonBody(t, rr))

	_ = doRequest(t, router, "GET", "/apps/nosuchapp/channels", "",
		http.StatusNotFound)
}

func TestListChannels(t *testing.T) {
	s := initTest(t, DefaultConfig)
	router := newRouter(s)

	rr := doRequest(t, router, "GET", "/apps/testapp/channels", "",
		http.StatusOK)
	require.Equal(t, J(`{"channels":{}}`), jsonBody(t, rr))
}

func TestGetChannel(t *testing.T) {
	s := initTest(t, DefaultConfig)
	router := newRouter(s)

	rr := doRequest(t, router, "GET", "/apps/testapp/channels/testchan", "", http.StatusOK)
	require.Equal(t, J(`{}`), jsonBody(t, rr))

	c, err := s.GetOrCreateChannel("testapp", "testchan")
	require.Nil(t, err)
	u := newUser(0, nil)
	c.SubscribeUser(u.ID)

	rr = doRequest(t, router, "GET", "/apps/testapp/channels/testchan", "", http.StatusOK)
	require.Equal(t, J(`{"occupied": true, "subscription_count": 1}`), jsonBody(t, rr))

	c, err = s.GetOrCreateChannel("testapp", "presence-testchan")
	require.Nil(t, err)
	c.SubscribeUser(u.ID)
	c.SubscribeUser(u.ID)

	rr = doRequest(t, router, "GET", "/apps/testapp/channels/presence-testchan", "", http.StatusOK)
	require.Equal(t, J(`{"occupied": true, "subscription_count": 2, "user_count": 1}`), jsonBody(t, rr))
}
