package notifier

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigReadFile(t *testing.T) {
	config, err := ReadConfigFile("../config/sample.json")
	require.Nil(t, err)
	require.Equal(t,
		&ConfigApplication{
			Name:   "testapp",
			Key:    "1234567890",
			Secret: "abcdefghij",
		},
		config.GetApp("testapp"))
	require.Equal(t,
		&ConfigApplication{
			Name:   "testapp2",
			Key:    "anystringwilldo",
			Secret: "xyzzy",
		},
		config.GetApp("testapp2"))
	require.Nil(t, config.GetApp("nosuchapp"))

	require.Equal(t,
		&ConfigApplication{
			Name:   "testapp",
			Key:    "1234567890",
			Secret: "abcdefghij",
		},
		config.GetAppFromKey("1234567890"))
	require.Equal(t,
		&ConfigApplication{
			Name:   "testapp2",
			Key:    "anystringwilldo",
			Secret: "xyzzy",
		},
		config.GetAppFromKey("anystringwilldo"))
	require.Nil(t, config.GetAppFromKey("nosuchkey"))

	require.Equal(t, config.Host, "localhost")
	require.Equal(t, config.Port, 8150)
}
