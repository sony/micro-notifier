package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/FZambia/sentinel"
	"github.com/gomodule/redigo/redis"
)

// Logical databases
//   0 - main database
//   1 - test database  (configurable)
// Keys
//   <application>/channels/<channel> - serialized Channel object
//   <application>/users              - array of user ids
//   events                           - pubsub channel for events

// DB encapsulates Redis operation from other parts
type DB struct {
	pool *redis.Pool // main connection pool

	// If set, called when event is broadcast via Redis PubSub.
	// Mainly used for testing.  Can return false to prevent
	// sending actual event to the browsers.
	eventCallback func(*EventRequest) bool
}

// UIDArray is kept in <application>/users
type UIDArray struct {
	UIDs []int
}

// EventRequest is a packet dispersed via pubsub channel
type EventRequest struct {
	Name        string // event name
	Data        string // event payload
	Application string // application name
	Channel     string // target channel name
}

func connectDB(ctx context.Context, config *Config) (redis.Conn, error) {
	options := []redis.DialOption{}
	if config.Redis.Secure {
		options = append(options, redis.DialUseTLS(true))
	}

	var c redis.Conn
	ch := make(chan error)
	go func() {
		var err error
		c, err = redis.Dial("tcp", config.Redis.Address, options...)
		ch <- err
	}()
	select {
	case err := <-ch:
		if err != nil {
			return nil, err
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if config.Redis.Password != "" {
		_, err := c.Do("AUTH", config.Redis.Password)
		if err != nil {
			return nil, err
		}
	}
	if config.Redis.Database != 0 {
		_, err := c.Do("SELECT", config.Redis.Database)
		if err != nil {
			return nil, err
		}
	}
	return c, nil
}

func newPool(config *Config) *redis.Pool {
	return &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		DialContext: func(ctx context.Context) (redis.Conn, error) {
			conn, err := connectDB(ctx, config)
			if err != nil {
				log.Printf("newPool: failed to connect DB: %+v", err)
			}
			return conn, err
		},
	}
}

// Sentinel provides a way to add high availability (HA) to Redis Pool using
// preconfigured addresses of Sentinel servers and name of master which Sentinels
// monitor. It works with Redis >= 2.8.12 (mostly because of ROLE command that
// was introduced in that version, it's possible though to support old versions
// using INFO command).
//
// import from https://github.com/FZambia/go-sentinel/blob/master/sentinel.go#L22
func newSentinelPool(config *Config) *redis.Pool {
	sntnl := &sentinel.Sentinel{
		Addrs:      []string{config.Redis.Address},
		MasterName: "mymaster",
		Dial: func(addr string) (redis.Conn, error) {
			c, err := redis.Dial("tcp", addr)
			if err != nil {
				return nil, err
			}
			return c, nil
		},
	}

	sntnl.Pool = func(addr string) *redis.Pool {
		return &redis.Pool{
			MaxIdle:     40,
			MaxActive:   40,
			Wait:        true,
			IdleTimeout: 10 * time.Minute,
			Dial: func() (redis.Conn, error) {
				return sntnl.Dial(addr)
			},
			TestOnBorrow: func(c redis.Conn, t time.Time) error {
				_, err := c.Do("PING")
				return err
			},
		}
	}

	return &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redis.Conn, error) {
			masterAddr, err := sntnl.MasterAddr()
			if err != nil {
				return nil, err
			}

			c, err := redis.Dial("tcp", masterAddr)
			if err != nil {
				return nil, err
			}

			if config.Redis.Password != "" {
				_, err := c.Do("AUTH", config.Redis.Password)
				if err != nil {
					return nil, err
				}
			}

			if config.Redis.Database != 0 {
				_, err := c.Do("SELECT", config.Redis.Database)
				if err != nil {
					return nil, err
				}
			}

			return c, nil
		},
	}
}

// InitDB establishes connections to Redis server according to the
// config parameters.
func InitDB(config *Config) *DB {
	var pool *redis.Pool

	if config.Redis.Sentinel {
		pool = newSentinelPool(config)
	} else {
		pool = newPool(config)
	}

	return &DB{pool: pool}
}

func (db *DB) getPool() (redis.Conn, error) {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return db.pool.GetContext(ctx)
}

// FlushDB flushes Redis commands
func (db *DB) FlushDB() {
	c, err := db.getPool()
	if err != nil {
		log.Printf("FlushDB: failed to get pool: %+v", err)
		return
	}
	defer c.Close()

	_, _ = c.Do("FLUSHDB")
}

// FinishDB cleans up Redis connection
func (db *DB) FinishDB() {
	c, err := db.getPool()
	if err != nil {
		log.Printf("FinishDB: failed to get pool: %+v", err)
		return
	}

	if err := c.Close(); err != nil {
		log.Printf("FinishDB: failed to close redis pool: %+v", err)
	}
}

// The first half of the transaction.  WATCH the key and fetch its value.
// On error, UNWATCH the key.
func (db *DB) watchAndGet(key string) (interface{}, error) {
	c, err := db.getPool()
	if err != nil {
		return nil, wrapErr(500, err)
	}
	defer c.Close()

	_, err = c.Do("WATCH", key)
	if err != nil {
		return nil, wrapErr(500, err)
	}
	r, err := c.Do("GET", key)
	if err != nil {
		_, _ = c.Do("UNWATCH")
		return nil, wrapErr(500, err)
	}
	return r, nil
}

// A common idiom to cancel the transacitons
func (db *DB) unwatch() {
	c, err := db.getPool()
	if err != nil {
		log.Printf("unwatch: failed to get pool: %+v", err)
		return
	}

	_, _ = c.Do("UNWATCH")
	_ = c.Close()
}

// The latter half of the transaction.  Assume the key is already
// WATCHed.  Encode payload by json and write it then commit,
// or discard everything if any step fails.  On success, returns
// what EXEC returns and nil.  On error, returns nil and *appError.
func (db *DB) updateAndCommit(key string, payload interface{}) (interface{}, error) {
	c, err := db.getPool()
	if err != nil {
		return nil, wrapErr(500, err)
	}
	defer c.Close()

	data, err := json.Marshal(payload)
	if err != nil {
		_, _ = c.Do("UNWATCH")
		return nil, wrapErr(500, err)
	}
	_, err = c.Do("MULTI")
	if err != nil {
		_, _ = c.Do("UNWATCH")
		return nil, wrapErr(500, err)
	}
	_, err = c.Do("SET", key, data)
	if err != nil {
		_, _ = c.Do("DISCARD")
		return nil, wrapErr(500, err)
	}
	r, err := c.Do("EXEC")
	if err != nil {
		_, _ = c.Do("DISCARD")
		return nil, wrapErr(500, err)
	}
	return r, nil
}

// GetChannels returns map of channel names to channels
func (db *DB) GetChannels(appname string) (map[string]*Channel, error) {
	c, err := db.getPool()
	if err != nil {
		return nil, wrapErr(500, err)
	}
	defer c.Close()

	pattern := appname + "/channels/*"
	cursor := "0"
	channels := make(map[string]*Channel)

	for {
		r, err := c.Do("SCAN", cursor, "MATCH", pattern)

		if err != nil {
			return nil, wrapErr(500, err)
		}
		ar, ok := r.([]interface{})
		if !ok {
			return nil, appErr(500, fmt.Sprintf("redis SCAN returned weird value: %v", r))
		}

		next := string(ar[0].([]byte))
		keys, ok := ar[1].([]interface{})
		if !ok {
			return nil, appErr(500, fmt.Sprintf("redis SCAN returned weird value: %v", r))
		}
		nkeys := len(keys)

		for i := 0; i < nkeys; i++ {
			key := string(keys[i].([]byte))
			r1, err := c.Do("GET", key)
			if err != nil {
				return nil, wrapErr(500, err)
			}
			var ch Channel
			err = json.Unmarshal(r1.([]byte), &ch)
			if err != nil {
				return nil, wrapErr(500, err)
			}
			channels[ch.Name] = &ch
		}

		if next == "0" {
			break
		}
		cursor = next
	}
	return channels, nil
}

// GetChannel returns the named channel.  The named channel must exist.
func (db *DB) GetChannel(appname string, channame string) (*Channel, error) {
	c, err := db.getPool()
	if err != nil {
		return nil, wrapErr(500, err)
	}
	defer c.Close()

	r, err := c.Do("GET", appname+"/channels/"+channame)
	if err != nil {
		return nil, wrapErr(500, err)
	}
	if r == nil {
		return nil, appErr(400, fmt.Sprintf("No such channel: %s in %s", channame, appname))
	}

	var ch Channel
	err = json.Unmarshal(r.([]byte), &ch)
	if err != nil {
		return nil, wrapErr(500, err)
	}
	return &ch, nil
}

// GetOrCreateChannel returns the named channel; if the named channel
// doesn't exist, create one.
func (db *DB) GetOrCreateChannel(appname string, channame string) (*Channel, error) {
	key := appname + "/channels/" + channame
	var ch Channel

	r, apperr := db.watchAndGet(key)
	if apperr != nil {
		return nil, apperr
	}
	if r != nil {
		db.unwatch()
		err := json.Unmarshal(r.([]byte), &ch)
		if err != nil {
			return nil, wrapErr(500, err)
		}
		return &ch, nil
	}
	ch = Channel{Name: channame, Users: make(map[int]int)}
	r, apperr = db.updateAndCommit(key, ch)
	if apperr != nil {
		return nil, apperr
	}
	if r == nil {
		// Somebody has created the channel.  Retry.
		return db.GetOrCreateChannel(appname, channame)
	}
	return &ch, nil
}

// AddUserIDToChannel adds UID to the list of subscribers in the specified
// channel.
func (db *DB) AddUserIDToChannel(appname string, channame string, uid int) error {
	key := appname + "/channels/" + channame
	var ch Channel

	r, apperr := db.watchAndGet(key)
	if apperr != nil {
		return apperr
	}
	if r == nil {
		ch = Channel{Name: channame, Users: make(map[int]int)}
	} else {
		err := json.Unmarshal(r.([]byte), &ch)
		if err != nil {
			db.unwatch()
			return wrapErr(500, err)
		}
	}
	ch.SubscribeUser(uid)
	r, apperr = db.updateAndCommit(key, ch)
	if apperr != nil {
		return apperr
	}
	if r == nil {
		// Somebody has created the channel.  Retry.
		return db.AddUserIDToChannel(appname, channame, uid)
	}
	return nil
}

// DeleteUserIDFromChannel removes the given uid from the subscribers
// of the specified channel.
func (db *DB) DeleteUserIDFromChannel(appname string, channame string, uid int) error {
	key := appname + "/channels/" + channame
	var ch Channel

	r, apperr := db.watchAndGet(key)
	if apperr != nil {
		return apperr
	}
	if r == nil {
		db.unwatch()
		return appErr(400,
			fmt.Sprintf("Attempt to unsubscribe nonexistent channel (application: %s, uid %d, channel: %s)",
				appname, uid, channame))
	}

	err := json.Unmarshal(r.([]byte), &ch)
	if err != nil {
		db.unwatch()
		return wrapErr(500, err)
	}
	ch.UnsubscribeUser(uid)
	r, apperr = db.updateAndCommit(key, ch)
	if apperr != nil {
		return apperr
	}
	if r == nil {
		// Somebody has created the channel.  Retry.
		return db.DeleteUserIDFromChannel(appname, channame, uid)
	}
	return nil
}

// Returns an unique nonnegative UID in the application.
func (db *DB) allocateUserID(appname string) (int, error) {
	key := appname + "/users"

	r, apperr := db.watchAndGet(key)
	if apperr != nil {
		return -1, apperr
	}

	var uid int
	var uids = UIDArray{UIDs: []int{}}

	if r == nil {
		// We're the first
		uid = 0
		uids.UIDs = []int{0}
	} else {
		err := json.Unmarshal(r.([]byte), &uids)
		if err != nil {
			db.unwatch()
			return -1, wrapErr(500, err)
		}
		maxid := 0
		for _, u := range uids.UIDs {
			if u > maxid {
				maxid = u
			}
		}
		ids := make([]bool, maxid+1)
		for _, u := range uids.UIDs {
			ids[u] = true
		}
		uid = -1
		for id, ok := range ids {
			if !ok {
				uid = id
				break
			}
		}
		if uid < 0 {
			uid = len(ids)
		}
		uids.UIDs = append(uids.UIDs, uid)
	}

	r, apperr = db.updateAndCommit(key, uids)
	if apperr != nil {
		return -1, apperr
	}
	if r == nil {
		// conflict.  retry.
		return db.allocateUserID(appname)
	}
	return uid, nil
}

// DeleteUserID deletes the given user id.  Note: The user must have been
// unsubscribed from all the channels.  Supervisor.RemoveUser takes care of that.
func (db *DB) DeleteUserID(appname string, uid int) error {
	key := appname + "/users"

	r, apperr := db.watchAndGet(key)
	if apperr != nil {
		return apperr
	}
	if r == nil {
		return nil
	}

	var uids UIDArray
	err := json.Unmarshal(r.([]byte), &uids)
	if err != nil {
		db.unwatch()
		return wrapErr(500, err)
	}

	for idx, u := range uids.UIDs {
		if u == uid {
			uids.UIDs = append(uids.UIDs[:idx], uids.UIDs[idx+1:]...)
			r, apperr = db.updateAndCommit(key, uids)
			if apperr != nil {
				return apperr
			}
			if r == nil {
				// conflict.  retry.
				return db.DeleteUserID(appname, uid)
			}
			return nil
		}
	}

	db.unwatch()
	// no such uid; we don't complain.
	return nil
}

// GetAllUserIDs returns all user IDs in the given app.
func (db *DB) GetAllUserIDs(appname string) ([]int, error) {
	c, err := db.getPool()
	if err != nil {
		return nil, wrapErr(500, err)
	}
	defer c.Close()

	key := appname + "/users"

	r, err := c.Do("GET", key)
	if err != nil {
		return nil, wrapErr(500, err)
	}
	if r == nil {
		return make([]int, 0), nil
	}

	var uids UIDArray
	err = json.Unmarshal(r.([]byte), &uids)
	if err != nil {
		return nil, wrapErr(500, err)
	}

	return uids.UIDs, nil
}

//
// Redis push event handling
//

func (s *Supervisor) redisSubscriberLoop() error {
	c, err := s.db.getPool()
	if err != nil {
		s.logger.Errorw("PubSubConn failed to get pool", "error", err)
		return err
	}
	defer c.Close()

	psc := redis.PubSubConn{Conn: c}
	err = psc.Subscribe("events")
	if err != nil {
		s.logger.Errorw("PubSubConn Subscribe failed", "error", err)
		return err
	}
	defer func() {
		if err := psc.Close(); err != nil {
			s.logger.Errorw("PubSubConn Close failed", "error", err)
			return
		}
	}()

	for {
		switch v := psc.Receive().(type) {
		case redis.Message:
			var er EventRequest
			err := json.Unmarshal(v.Data, &er)
			if err != nil {
				s.logger.Infow("redis message decoding error", "error", err, "message", v.Data)
			} else {
				apperr := s.handleRedisEventRequest(&er)
				if apperr != nil {
					s.logger.Infow("redis message handle error", "appError", apperr, "message", er)
				}
			}
		case error:
			return v
		}
	}
}

func (s *Supervisor) redisSubscriberLoopWithRetry() {
	for {
		err := s.redisSubscriberLoop()
		if strings.Contains(err.Error(), "use of closed network") {
			return
		}

		time.Sleep(5 * time.Second)
	}
}

func (s *Supervisor) handleRedisEventRequest(er *EventRequest) error {
	if s.db.eventCallback != nil {
		if !s.db.eventCallback(er) {
			return nil
		}
	}
	a, apperr := s.GetApp(er.Application)
	if apperr != nil {
		return apperr
	}
	ev := Event{Name: er.Name, Data: er.Data}
	return s.realBroadcast(a, &ev, er.Channel)
}

// KickRedisSubscription starts goroutine to handle Redis events.
func (s *Supervisor) KickRedisSubscription() {
	if s.db != nil {
		go s.redisSubscriberLoopWithRetry()
	}
}

// PublishRedisEvent pushes Redis events.
func (s *Supervisor) PublishRedisEvent(ev *EventRequest) error {
	if s.db != nil {
		data, err := json.Marshal(ev)
		if err != nil {
			return wrapErr(500, err)
		}
		c, err := s.db.getPool()
		if err != nil {
			return wrapErr(500, err)
		}
		_, err = c.Do("PUBLISH", "events", data)
		_ = c.Close()
		if err != nil {
			return wrapErr(500, err)
		}
	}
	return nil
}
