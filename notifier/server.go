package notifier

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	defaultPort = 8111
)

// NewServer creates a new HTTP server for MicroNotifier.
func NewServer(s *Supervisor) *http.Server {
	host := s.Config.Host
	port := s.Config.Port
	if port == 0 {
		port = defaultPort
	}

	router := handlers.LoggingHandler(os.Stdout, newRouter(s))

	s.logger.Infow("starting server", "host", host, "port", port,
		"num-applications", len(s.Config.Applications),
		"secure", s.Config.Certificate != "")

	return &http.Server{
		Handler:      router,
		Addr:         fmt.Sprintf("%s:%d", host, port),
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}
}

func newRouter(s *Supervisor) http.Handler {
	router := mux.NewRouter()

	// Meta functions
	router.HandleFunc("/apps", s.listApplications).Methods("GET")
	router.HandleFunc("/apps/{app}/channels", s.appChannels).Methods("GET")
	router.HandleFunc("/apps/{app}/channels/{chan}", s.getChannel).Methods("GET")
	router.HandleFunc("/apps/{app}/channels/{chan}/users", s.getChannelUsers).Methods("GET")
	router.HandleFunc("/apps/{app}/events", s.trigger).Methods("POST")

	router.HandleFunc("/app/{key}", s.establishConnection).Methods("GET")

	router.Handle("/metrics", promhttp.Handler()).Methods("GET")

	return handlers.LoggingHandler(os.Stdout, router)
}
