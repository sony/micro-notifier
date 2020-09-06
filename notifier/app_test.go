package notifier

import (
	"testing"

	mapset "github.com/deckarep/golang-set"
	"github.com/stretchr/testify/require"
)

func TestRegsiterUser(t *testing.T) {
	app := Application{
		Name:     "testapp",
		Channels: make(map[string]*Channel),
		Users:    mapset.NewSet(),
	}

	_ = app.registerUser(1, nil)
	require.Equal(t, app.Users.Cardinality(), 1)
	_ = app.registerUser(10, nil)
	require.Equal(t, app.Users.Cardinality(), 2)
	_ = app.registerUser(30, nil)
	require.Equal(t, app.Users.Cardinality(), 3)
}
