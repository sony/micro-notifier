// +build chrome_test
// +build redis_test

package notifier

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/gorilla/mux"
	pusher "github.com/pusher/pusher-http-go"
	"github.com/stretchr/testify/require"
)

func spawnServersRedis(t *testing.T, contentProvider TestContentProvider) *TestServers {
	// Start notifier server
	s := initTest(t, RedisConfig)
	router := newRouter(s)

	var notifier *httptest.Server
	var client *http.Client
	notifier = httptest.NewServer(router)
	client = &http.Client{}

	// Wait for server to spin up
	var ok = false
	for i := 0; i < 10; i++ {
		_, err := client.Get(notifier.URL + "/apps")
		if err == nil {
			ok = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.True(t, ok)

	ts := TestServers{Notifier: notifier}

	// Content server
	ts.testContent = contentProvider(&ts, false)

	tsrouter := mux.NewRouter()
	tsrouter.HandleFunc("/", ts.serveContent).Methods("GET")
	tsrouter.HandleFunc("/auth", ts.signerHandler).Methods("POST")
	ts.Content = httptest.NewServer(tsrouter)

	var httpClient *http.Client
	ts.PusherClient = &pusher.Client{
		AppID:      "testapp",
		Key:        "1234567890",
		Secret:     "abcdefghij",
		Host:       regexp.MustCompile("^http.*://").ReplaceAllString(ts.Notifier.URL, ""),
		Secure:     false,
		HTTPClient: httpClient,
	}

	return &ts
}

func TestWebsocketConnectionRedis(t *testing.T) {
	ts := spawnServersRedis(t, websocketConnectionContent)
	defer ts.Close()
	client := ts.PusherClient

	users, err := client.GetChannelUsers("my-channel")
	require.Nil(t, err)
	require.Equal(t, 0, len(users.List))

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("allow-insecure-localhost", true),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	chromedp.ListenTarget(ctx, func(ev interface{}) {
		if ev, ok := ev.(*page.EventJavascriptDialogOpening); ok {
			fmt.Println("closing alert:", ev.Message)
			go func() {
				if err := chromedp.Run(ctx,
					page.HandleJavaScriptDialog(true),
				); err != nil {
					panic(err)
				}
			}()
		}
		if msg, ok := ev.(*cdproto.Message); ok {
			fmt.Println("message: ", string(msg.Result))
		}
	})

	err = chromedp.Run(ctx,
		chromedp.Navigate(ts.Content.URL),
	)
	require.Nil(t, err)

	// Wait for subscription
	var ok = false
	for i := 0; i < 10; i++ {
		time.Sleep(50 * time.Millisecond)
		users, err = client.GetChannelUsers("my-channel")
		require.Nil(t, err)
		if len(users.List) == 1 {
			ok = true
			break
		}
	}
	require.True(t, ok)

	data := map[string]string{"message": "knock, knock"}
	client.Trigger("my-channel", "my-event", data)

	var res []string
	for i := 0; i < 10; i++ {
		time.Sleep(50 * time.Millisecond)
		err = chromedp.Run(ctx,
			chromedp.Evaluate("events", &res),
		)
		require.Nil(t, err)
		if len(res) > 0 {
			break
		}
	}
	require.Equal(t, 1, len(res))
	require.Equal(t, "knock, knock", res[0])

	data = map[string]string{"message": "who's there?"}
	client.Trigger("my-channel", "my-event", data)

	for i := 0; i < 10; i++ {
		time.Sleep(50 * time.Millisecond)
		err = chromedp.Run(ctx,
			chromedp.Evaluate("events", &res),
		)
		require.Nil(t, err)
		if len(res) > 1 {
			break
		}
	}
	require.Equal(t, 2, len(res))
	require.Equal(t, "who's there?", res[1])
}

func TestWebsocketSignatureRedis(t *testing.T) {
	ts := spawnServersRedis(t, websocketSignatureContent)
	defer ts.Close()
	client := ts.PusherClient

	users, err := client.GetChannelUsers("private-my-channel")
	require.Nil(t, err)
	require.Equal(t, 0, len(users.List))

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("allow-insecure-localhost", true),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	//	ctx, cancel := chromedp.NewContext(context.Background(),
	//		chromedp.WithBrowserOption(chromedp.WithBrowserDebugf(log.Printf)))
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	chromedp.ListenTarget(ctx, func(ev interface{}) {
		if ev, ok := ev.(*page.EventJavascriptDialogOpening); ok {
			fmt.Println("closing alert:", ev.Message)
			go func() {
				if err := chromedp.Run(ctx,
					page.HandleJavaScriptDialog(true),
				); err != nil {
					panic(err)
				}
			}()
		}
	})

	err = chromedp.Run(ctx,
		chromedp.Navigate(ts.Content.URL),
	)
	require.Nil(t, err)

	// Wait for subscription
	var ok = false
	for i := 0; i < 10; i++ {
		time.Sleep(50 * time.Millisecond)
		users, err = client.GetChannelUsers("private-my-channel")
		require.Nil(t, err)
		if len(users.List) == 1 {
			ok = true
			break
		}
	}
	require.True(t, ok)
}
