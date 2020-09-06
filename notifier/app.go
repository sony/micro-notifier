package notifier

import (
	"fmt"

	mapset "github.com/deckarep/golang-set"
	"github.com/gorilla/websocket"
)

// User hold individial user (assigned with unique ID)
type User struct {
	ID         int
	Connection *websocket.Conn
	App        *Application
	SocketID   string
}

// Event is the actual event to be sent.
type Event struct {
	Name string
	Data string
}

// Channel corresponds to Pusher channel.  Channels are implicitly
// created when subscribed and/or triggered.
type Channel struct {
	Name  string
	Users map[int]int // user id -> subscription count
}

// Application is a namaspece for channels
type Application struct {
	Name     string
	Channels map[string]*Channel
	Users    mapset.Set // Set of User
}

//
// Supervisors
//

// InitApps populates Supervisor.Apps.
func (s *Supervisor) InitApps() {
	for _, ca := range s.Config.Applications {
		app := Application{
			Name:     ca.Name,
			Channels: make(map[string]*Channel),
			Users:    mapset.NewSet(),
		}
		s.Apps = append(s.Apps, &app)
	}
}

// GetApp returns the named application.
func (s *Supervisor) GetApp(name string) (*Application, *AppError) {
	for _, a := range s.Apps {
		if a.Name == name {
			return a, nil
		}
	}
	return nil, AppErr(404, "No such application")
}

// GetAppFromKey returns the application with specified key.
func (s *Supervisor) GetAppFromKey(key string) (*Application, *AppError) {
	ca := s.Config.GetAppFromKey(key)
	if ca != nil {
		return s.GetApp(ca.Name)
	}
	return nil, AppErr(404, "Unknown application key")
}

// GetChannels returns an array of channels in the given app
func (s *Supervisor) GetChannels(appname string) (map[string]*Channel, *AppError) {
	app, apperr := s.GetApp(appname)
	if apperr != nil {
		return nil, apperr
	}

	if s.db != nil {
		return s.db.GetChannels(appname)
	}
	return app.Channels, nil
}

// GetChannel returns the named channel in the named application.
// If there's no such channel, 404 error is returned.
func (s *Supervisor) GetChannel(appname string, channame string) (*Channel, *AppError) {
	a, apperr := s.GetApp(appname)
	if apperr != nil {
		return nil, apperr
	}
	if s.db != nil {
		return s.db.GetChannel(appname, channame)
	}
	return a.getChannel(channame)
}

// GetOrCreateChannel returns the named channel in the named application.
// If there's no such channel, create it.
func (s *Supervisor) GetOrCreateChannel(appname string, channame string) (*Channel, *AppError) {
	a, apperr := s.GetApp(appname)
	if apperr != nil {
		return nil, apperr
	}
	if s.db != nil {
		return s.db.GetOrCreateChannel(appname, channame)
	}
	return a.getOrCreateChannel(channame)
}

// AddUser creates a new user associated to an application, with the
// given connection.
func (s *Supervisor) AddUser(appname string, conn *websocket.Conn) (*User, *AppError) {
	a, apperr := s.GetApp(appname)
	if apperr != nil {
		return nil, apperr
	}

	var uid int
	if s.db != nil {
		uid, apperr = s.db.allocateUserID(appname)
		if apperr != nil {
			return nil, apperr
		}
	} else {
		uid = a.allocateUserID()
	}

	u := a.registerUser(uid, conn)
	return u, nil
}

// RemoveUser removes the specified user from the application and
// associated channels.
// NB: The user's connection must be cleaned up by the caller (see socket.go)
func (s *Supervisor) RemoveUser(appname string, uid int) *AppError {
	a, apperr := s.GetApp(appname)
	if apperr != nil {
		return apperr
	}
	u := a.GetUserByID(uid)
	if u == nil {
		return AppErr(500,
			fmt.Sprintf("RemoveUser called on an unmanaged user (app=%s, uid=%d)",
				appname, uid))
	}

	if s.db != nil {
		chs, apperr := s.db.GetChannels(appname)
		if apperr != nil {
			return apperr
		}
		for _, c := range chs {
			_, ok := c.Users[uid]
			if ok {
				apperr = s.db.DeleteUserIDFromChannel(appname,
					c.Name, uid)
				if apperr != nil {
					s.logger.Infow("DeleteUserIDFromChannels failed (app=%s, channel=%s, uid=%d)",
						appname, c.Name, uid)
				}
			}
		}
		apperr = s.db.DeleteUserID(appname, uid)
		if apperr != nil {
			return apperr
		}
	} else {
		for _, c := range u.App.Channels {
			c.UnsubscribeUser(uid)
		}
	}
	a.unregisterUser(uid)
	return nil
}

// Subscribe let the user subscribe the named channel
// The user with UID must be managed by this process (when socket.go calls
// this, it should.)
func (s *Supervisor) Subscribe(appname string, uid int, channame string) *AppError {
	a, apperr := s.GetApp(appname)
	if apperr != nil {
		return apperr
	}
	u := a.GetUserByID(uid)
	if u == nil {
		return AppErr(500,
			fmt.Sprintf("Subscribe called on an unmanaged user (app=%s, uid=%d, channel=%s)",
				appname, uid, channame))
	}

	if s.db != nil {
		apperr = s.db.AddUserIDToChannel(appname, channame, uid)
		if apperr != nil {
			return apperr
		}
	} else {
		ch, _ := u.App.getOrCreateChannel(channame)
		ch.SubscribeUser(uid)
	}
	return nil
}

// Unsubscribe let the user unsubscribe the named channel
// The user with UID must be managed by this process (when socket.go calls
// this, it should.)
func (s *Supervisor) Unsubscribe(appname string, uid int, channame string) *AppError {
	a, apperr := s.GetApp(appname)
	if apperr != nil {
		return apperr
	}
	u := a.GetUserByID(uid)
	if u == nil {
		return AppErr(500,
			fmt.Sprintf("Unsubscribe called on an unmanaged user (app=%s, uid=%d, channel=%s)",
				appname, uid, channame))
	}

	if s.db != nil {
		apperr = s.db.DeleteUserIDFromChannel(appname, channame, uid)
		if apperr != nil {
			return apperr
		}
	} else {
		ch, ok := u.App.Channels[channame]
		if ok {
			ch.UnsubscribeUser(uid)
		}
	}
	return nil
}

//
// Applications
//

// This is only used in standalone mode
func (a *Application) getChannel(channame string) (*Channel, *AppError) {
	ch, ok := a.Channels[channame]
	if ok {
		return ch, nil
	}
	return nil, AppErr(404, "No such channel")
}

// This is only used in standalone mode
func (a *Application) getOrCreateChannel(channame string) (*Channel, *AppError) {
	ch, ok := a.Channels[channame]
	if ok {
		return ch, nil
	}
	ch = &Channel{Name: channame, Users: make(map[int]int)}
	a.Channels[channame] = ch
	return ch, nil
}

func newUser(id int, conn *websocket.Conn) *User {
	return &User{
		ID:         id,
		Connection: conn,
	}
}

// GetUserByID returns a user with the given ID, or nil.
// (NB: Expect nil return value, for the user may not be managed by
// this process.)
func (a *Application) GetUserByID(ID int) *User {
	it := a.Users.Iterator()
	for u := range it.C {
		if u.(*User).ID == ID {
			it.Stop()
			return u.(*User)
		}
	}
	return nil
}

// This is only called in standalone mode.
func (a *Application) allocateUserID() int {
	maxid := 0
	for u := range a.Users.Iterator().C {
		if u.(*User).ID > maxid {
			maxid = u.(*User).ID
		}
	}
	ids := make([]bool, maxid+1)
	for u := range a.Users.Iterator().C {
		ids[u.(*User).ID] = true
	}
	var newID = -1
	for id, ok := range ids {
		if !ok {
			newID = id
			break
		}
	}
	if newID < 0 {
		newID = a.Users.Cardinality()
	}
	return newID
}

func (a *Application) registerUser(uid int, conn *websocket.Conn) *User {
	user := newUser(uid, conn)
	a.Users.Add(user)
	user.App = a
	return user
}

func (a *Application) unregisterUser(uid int) {
	u := a.GetUserByID(uid)
	if u != nil {
		a.Users.Remove(u)
	}
}

//
// Channels
//

// UserCount returns the number of users subscribing the channel.
func (c *Channel) UserCount() int {
	return len(c.Users)
}

// SubscriptionCount returns the number of subscriptions of the channel.
// A user can subscribe the same channel multiple times, so the count
// can be greater than UserCount.
func (c *Channel) SubscriptionCount() int {
	var sum = 0
	for _, cnt := range c.Users {
		sum += cnt
	}
	return sum
}

// SubscribeUser let the user of uid subscribe the channel.
// Returns the number of subscribers of the channel.
func (c *Channel) SubscribeUser(uid int) int {
	if c.Users == nil {
		c.Users = map[int]int{}
	}
	n, ok := c.Users[uid]
	if !ok {
		n = 0
	}
	c.Users[uid] = n + 1
	return n + 1
}

// UnsubscribeUser let the user of uid unsubscribe the channel.
// Returns the updated number of subscribers of the channel.
func (c *Channel) UnsubscribeUser(uid int) int {
	if c.Users == nil {
		c.Users = map[int]int{}
	}
	n, ok := c.Users[uid]
	if ok {
		if n == 1 {
			delete(c.Users, uid)
		} else {
			c.Users[uid] = n - 1
		}
		return n - 1
	}
	return 0
}
