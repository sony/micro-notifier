package main

import (
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	pusher "github.com/pusher/pusher-http-go"
	"go.uber.org/zap"
)

var logger *zap.SugaredLogger

func main() {
	devlogger, err := zap.NewDevelopment()
	if err != nil {
		log.Fatalf("can't initialize zap logger: %v", err)
	}
	logger = devlogger.Sugar()
	logger.Infow("testapp on localhost:8151")

	router := mux.NewRouter()

	router.HandleFunc("/", serveContent).Methods("GET")
	router.HandleFunc("/auth", handleAuth).Methods("POST")

	server := http.Server{
		Handler:      router,
		Addr:         "localhost:8151",
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	log.Fatal(server.ListenAndServe())
}

func serveContent(w http.ResponseWriter, req *http.Request) {
	_, _ = w.Write([]byte(`
<head>
  <title>Pusher Test</title>
  <script src="https://js.pusher.com/4.4/pusher.min.js"></script>
  <script>

    // Enable pusher logging - don't include this in production
    Pusher.logToConsole = true;

    var pusher = new Pusher('1234567890', {
      wsHost: 'localhost',
      wsPort: 8150,
      authEndpoint: '/auth',
      forceTLS: false
    });

    var events = [];

    var channel = pusher.subscribe('private-my-channel');
    channel.bind('my-event', function(data) {
      alert(data)
    });
  </script>
</head>
<body>
  <h1>Pusher Test</h1>
  <p>
    Try publishing an event to channel <code>private-my-channel</code>
    with event name <code>my-event</code>.
  </p>
</body>
`))
}

func handleAuth(w http.ResponseWriter, req *http.Request) {
	params, _ := ioutil.ReadAll(req.Body)

	logger.Infow("signerHandler", "params", string(params))

	client := pusher.Client{
		AppID:  "testapp",
		Key:    "1234567890",
		Secret: "abcdefghij",
	}

	response, err := client.AuthenticatePrivateChannel(params)

	if err != nil {
		logger.Errorw("signerHandler", "authenticate error", err)
		http.Error(w, "bad request", 400)
		return
	}

	logger.Infow("signerHandler", "returning", string(response))
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(response)
}
