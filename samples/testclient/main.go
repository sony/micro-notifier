package main

import (
	"log"

	pusher "github.com/pusher/pusher-http-go"
)

func main() {
	client := pusher.Client{
		AppID:  "testapp",
		Key:    "1234567890",
		Host:   "localhost:8150",
		Secret: "abcdefghij",
		Secure: false,
	}

	data := map[string]string{"message": "hello world"}
	err := client.Trigger("my-channel", "my-event", data)
	if err != nil {
		log.Printf("pushder.Client.Trigger error: %v", err)
	}
}
