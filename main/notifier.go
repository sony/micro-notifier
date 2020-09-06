package main

import (
	"flag"
	"log"

	"github.com/sony/micro-notifier/notifier"
)

func main() {
	configFile := flag.String("c", "", "Config file name")

	flag.Parse()
	config, err := notifier.ReadConfigFile(*configFile)
	if err != nil {
		log.Fatalf("cannot read config file: %v", err)
	}

	s := notifier.NewSupervisor(config)
	defer s.Finish()

	server := notifier.NewServer(s)
	if config.Certificate != "" && config.PrivateKey != "" {
		err = server.ListenAndServeTLS(config.Certificate, config.PrivateKey)
	} else {
		err = server.ListenAndServe()
	}
	if err != nil {
		log.Fatalf("cannot listen and serve: %v", err)
	}
}
