//go:build redis_sentinel_test
// +build redis_sentinel_test

package notifier

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func initRedisTest(t *testing.T) *Supervisor {
	path := "../config/sample-redis-sentinel-test.json"
	config, err := ReadConfigFile(path)
	require.Nil(t, err)

	s := NewSupervisor(config)
	s.db.FlushDB()
	return s
}
