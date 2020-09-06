package notifier

import (
	"log"

	"go.uber.org/zap"
)

// Supervisor is the root of runtime data structure.
type Supervisor struct {
	Port   int
	Apps   []*Application
	Config *Config

	db     *DB
	logger *zap.SugaredLogger
}

// NewSupervisor creates a new Supervisor.
func NewSupervisor(config *Config) *Supervisor {
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("can't initialize zap logger: %v", err)
	}
	s := &Supervisor{Config: config, logger: logger.Sugar()}

	if config.Redis.Address != "" {
		s.db = InitDB(config)
	}

	s.InitApps()
	s.KickRedisSubscription()
	return s
}

// Finish finalizes the Supervisor.
func (s *Supervisor) Finish() {
	_ = s.logger.Sync()
	if s.db != nil {
		s.db.FinishDB()
	}
}
