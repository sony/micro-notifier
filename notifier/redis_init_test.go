//go:build redis_test

package notifier

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func initRedisTest(t *testing.T) *Supervisor {
	path := "../config/sample-redis-test.json"
	config, err := ReadConfigFile(path)
	require.Nil(t, err)

	s := NewSupervisor(config)
	s.db.FlushDB()
	return s
}
