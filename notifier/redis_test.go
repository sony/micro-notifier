// +build redis_test redis_sentinel_test

package notifier

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRedisListChannels(t *testing.T) {
	s := initRedisTest(t)
	defer s.Finish()
	router := newRouter(s)

	rr := doRequest(t, router, "GET", "/apps/testapp/channels", "",
		http.StatusOK)
	require.Equal(t, J(`{"channels":{}}`), jsonBody(t, rr))
}

func TestRedisCreateAndGetChannel(t *testing.T) {
	s := initRedisTest(t)
	router := newRouter(s)
	defer s.Finish()

	rr := doRequest(t, router, "GET", "/apps/testapp/channels/chan0", "",
		http.StatusOK)
	require.Equal(t, J(`{}`), jsonBody(t, rr))

	rr = doRequest(t, router, "GET", "/apps/testapp/channels/chan1", "",
		http.StatusOK)
	require.Equal(t, J(`{}`), jsonBody(t, rr))

	rr = doRequest(t, router, "GET", "/apps/testapp/channels", "",
		http.StatusOK)
	require.Equal(t, J(`{`+
		`"channels":{`+
		`  "chan0":{"user_count":0},`+
		`  "chan1":{"user_count":0}`+
		`}}`), jsonBody(t, rr))
}

func TestLowlevelUserIDManager(t *testing.T) {
	s := initRedisTest(t)
	defer s.Finish()

	id, apperr := s.db.allocateUserID("testapp")
	require.Nil(t, apperr)
	require.Equal(t, 0, id)

	id, apperr = s.db.allocateUserID("testapp")
	require.Nil(t, apperr)
	require.Equal(t, 1, id)

	id, apperr = s.db.allocateUserID("testapp")
	require.Nil(t, apperr)
	require.Equal(t, 2, id)

	uids, apperr := s.db.GetAllUserIDs("testapp")
	require.Nil(t, apperr)
	require.Equal(t, []int{0, 1, 2}, uids)

	uids, apperr = s.db.GetAllUserIDs("testapp2")
	require.Nil(t, apperr)
	require.Equal(t, []int{}, uids)

	apperr = s.db.DeleteUserID("testapp", 5)
	require.Nil(t, apperr)
	uids, apperr = s.db.GetAllUserIDs("testapp")
	require.Nil(t, apperr)
	require.Equal(t, []int{0, 1, 2}, uids)

	apperr = s.db.DeleteUserID("testapp", 1)
	require.Nil(t, apperr)
	uids, apperr = s.db.GetAllUserIDs("testapp")
	require.Nil(t, apperr)
	require.Equal(t, []int{0, 2}, uids)

	id, apperr = s.db.allocateUserID("testapp")
	require.Nil(t, apperr)
	require.Equal(t, 1, id)

	apperr = s.db.DeleteUserID("testapp", 2)
	require.Nil(t, apperr)
	uids, apperr = s.db.GetAllUserIDs("testapp")
	require.Nil(t, apperr)
	require.Equal(t, []int{0, 1}, uids)

	apperr = s.db.DeleteUserID("testapp", 0)
	require.Nil(t, apperr)
	uids, apperr = s.db.GetAllUserIDs("testapp")
	require.Nil(t, apperr)
	require.Equal(t, []int{1}, uids)

	id, apperr = s.db.allocateUserID("testapp")
	require.Nil(t, apperr)
	require.Equal(t, 0, id)

	id, apperr = s.db.allocateUserID("testapp")
	require.Nil(t, apperr)
	require.Equal(t, 2, id)

	id, apperr = s.db.allocateUserID("testapp")
	require.Nil(t, apperr)
	require.Equal(t, 3, id)

	apperr = s.db.DeleteUserID("testapp", 0)
	require.Nil(t, apperr)
	apperr = s.db.DeleteUserID("testapp", 1)
	require.Nil(t, apperr)
	apperr = s.db.DeleteUserID("testapp", 2)
	require.Nil(t, apperr)
	apperr = s.db.DeleteUserID("testapp", 3)
	require.Nil(t, apperr)
	uids, apperr = s.db.GetAllUserIDs("testapp")
	require.Nil(t, apperr)
	require.Equal(t, []int{}, uids)
}

func TestLowlevelSubscriptions(t *testing.T) {
	s := initRedisTest(t)
	defer s.Finish()

	u, apperr := s.AddUser("testapp", nil)
	require.Nil(t, apperr)
	require.Equal(t, 0, u.ID)
	u, apperr = s.AddUser("testapp", nil)
	require.Nil(t, apperr)
	require.Equal(t, 1, u.ID)

	_, apperr = s.GetOrCreateChannel("testapp", "chan0")
	require.Nil(t, apperr)
	_, apperr = s.GetOrCreateChannel("testapp", "chan1")
	require.Nil(t, apperr)

	apperr = s.Subscribe("testapp", 0, "chan0")
	require.Nil(t, apperr)
	apperr = s.Subscribe("testapp", 0, "chan1")
	require.Nil(t, apperr)
	apperr = s.Subscribe("testapp", 1, "chan1")
	require.Nil(t, apperr)
	apperr = s.Subscribe("testapp", 0, "chan1")
	require.Nil(t, apperr)

	ch, apperr := s.GetChannel("testapp", "chan0")
	require.Nil(t, apperr)
	require.Equal(t, map[int]int{0: 1}, ch.Users)

	ch, apperr = s.GetChannel("testapp", "chan1")
	require.Nil(t, apperr)
	require.Equal(t, map[int]int{0: 2, 1: 1}, ch.Users)

	apperr = s.Unsubscribe("testapp", 0, "chan1")
	require.Nil(t, apperr)
	ch, apperr = s.GetChannel("testapp", "chan1")
	require.Nil(t, apperr)
	require.Equal(t, map[int]int{0: 1, 1: 1}, ch.Users)

	apperr = s.RemoveUser("testapp", 0)
	require.Nil(t, apperr)
	ch, apperr = s.GetChannel("testapp", "chan0")
	require.Nil(t, apperr)
	require.Equal(t, map[int]int{}, ch.Users)
	ch, apperr = s.GetChannel("testapp", "chan1")
	require.Nil(t, apperr)
	require.Equal(t, map[int]int{1: 1}, ch.Users)

	apperr = s.Unsubscribe("testapp", 1, "chan1")
	require.Nil(t, apperr)
	ch, apperr = s.GetChannel("testapp", "chan1")
	require.Nil(t, apperr)
	require.Equal(t, map[int]int{}, ch.Users)
}

func TestLowlevelBroadcast(t *testing.T) {
	s := initRedisTest(t)
	defer s.Finish()

	u, apperr := s.AddUser("testapp", nil)
	require.Nil(t, apperr)
	require.Equal(t, 0, u.ID)
	u, apperr = s.AddUser("testapp", nil)
	require.Nil(t, apperr)
	require.Equal(t, 1, u.ID)

	_, apperr = s.GetOrCreateChannel("testapp", "chan0")
	require.Nil(t, apperr)

	var ersave *EventRequest
	s.db.eventCallback = func(er *EventRequest) bool {
		ersave = er
		return false
	}

	a, _ := s.GetApp("testapp")
	apperr = s.Broadcast(a,
		&Event{Name: "event-name", Data: "event-data"},
		"chan0")
	require.Nil(t, apperr)

	time.Sleep(500 * time.Millisecond)

	require.Equal(t, &EventRequest{Name: "event-name",
		Data:        "event-data",
		Application: "testapp",
		Channel:     "chan0"},
		ersave)
}

func TestLowlevelSubscribeNonexitentChannel(t *testing.T) {
	s := initRedisTest(t)
	defer s.Finish()

	u, apperr := s.AddUser("testapp", nil)
	require.Nil(t, apperr)
	require.Equal(t, 0, u.ID)

	apperr = s.Subscribe("testapp", 0, "nosuchchannel")
	require.Nil(t, apperr)

	ch, apperr := s.GetChannel("testapp", "nosuchchannel")
	require.Nil(t, apperr)
	require.Equal(t, map[int]int{0: 1}, ch.Users)
}
