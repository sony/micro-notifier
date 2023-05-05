//go:build chrome_test
// +build chrome_test

package notifier

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/gorilla/mux"
	pusher "github.com/pusher/pusher-http-go"
	"github.com/stretchr/testify/require"
)

type TestServers struct {
	Notifier     *httptest.Server
	Content      *httptest.Server
	PusherClient *pusher.Client

	testContent []byte
}

type TestContentProvider func(*TestServers, bool) []byte

func spawnServers(t *testing.T, contentProvider TestContentProvider, isSecure bool) *TestServers {
	// Start notifier server
	configType := DefaultConfig
	if isSecure {
		configType = SecureConfig
	}
	s := initTest(t, configType)
	router := newRouter(s)

	var notifier *httptest.Server
	var client *http.Client
	if isSecure {
		nts := httptest.NewUnstartedServer(router)
		nts.StartTLS()
		notifier = nts
		client = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	} else {
		notifier = httptest.NewServer(router)
		client = &http.Client{}
	}

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
	ts.testContent = contentProvider(&ts, isSecure)

	tsrouter := mux.NewRouter()
	tsrouter.HandleFunc("/", ts.serveContent).Methods("GET")
	tsrouter.HandleFunc("/auth", ts.signerHandler).Methods("POST")
	ts.Content = httptest.NewServer(tsrouter)

	var httpClient *http.Client
	if isSecure {
		httpClient = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	}

	ts.PusherClient = &pusher.Client{
		AppID:      "testapp",
		Key:        "1234567890",
		Secret:     "abcdefghij",
		Host:       regexp.MustCompile("^http.*://").ReplaceAllString(ts.Notifier.URL, ""),
		Secure:     isSecure,
		HTTPClient: httpClient,
	}

	return &ts
}

func (ts *TestServers) Close() {
	ts.Notifier.Close()
	ts.Content.Close()
}

func (ts *TestServers) notifierPort() string {
	return regexp.MustCompile(`:(\d+)$`).FindStringSubmatch(ts.Notifier.URL)[1]
}

func (ts *TestServers) serveContent(w http.ResponseWriter, req *http.Request) {
	w.Write(ts.testContent)
}

func (ts *TestServers) signerHandler(w http.ResponseWriter, req *http.Request) {
	params, _ := ioutil.ReadAll(req.Body)
	response, err := ts.PusherClient.AuthenticatePrivateChannel(params)

	if err != nil {
		log.Printf("signerHandler: authenticate error: %#v", err)
		http.Error(w, "bad request", 400)
		return
	}

	log.Printf("signerHandler: returning %s", string(response))
	w.Header().Set("Content-Type", "application/json")
	w.Write(response)
}

// Landing page for TestWebsocketConnection
func websocketConnectionContent(ts *TestServers, isSecure bool) []byte {
	return []byte(`
<head>
  <title>Pusher Test</title>
  <script src="https://js.pusher.com/4.4/pusher.min.js"></script>
  <script>

    // Enable pusher logging - don't include this in production
    Pusher.logToConsole = true;

    var pusher = new Pusher('1234567890', {
      wsHost: 'localhost',
      wsPort: ` + ts.notifierPort() + `,
      wssPort: ` + ts.notifierPort() + `,
      forceTLS: ` + strconv.FormatBool(isSecure) + `
    });

    var events = [];

    var channel = pusher.subscribe('my-channel');
    channel.bind('my-event', function(data) {
      events.push(data.message);
    });
  </script>
</head>
<body>
  <h1>Pusher Test</h1>
  <p>
    Try publishing an event to channel <code>my-channel</code>
    with event name <code>my-event</code>.
  </p>
</body>
`)
}

func TestWebsocketConnection(t *testing.T) {
	for _, isSecure := range []bool{false, true} {
		ts := spawnServers(t, websocketConnectionContent, isSecure)
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
		require.True(t, ok, fmt.Sprintf("secure is %v", isSecure))

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
}

// Landing page for TestWebsocketSignature
func websocketSignatureContent(ts *TestServers, isSecure bool) []byte {
	return []byte(`
<head>
  <title>Pusher Test</title>
  <script src="https://js.pusher.com/4.4/pusher.min.js"></script>
  <script>

    // Enable pusher logging - don't include this in production
    Pusher.logToConsole = true;

    var pusher = new Pusher('1234567890', {
      wsHost: 'localhost',
      wsPort: ` + ts.notifierPort() + `,
      wssPort: ` + ts.notifierPort() + `,
      authEndpoint: '/auth',
      forceTLS: ` + strconv.FormatBool(isSecure) + `
    });

    var events = [];

    var channel = pusher.subscribe('private-my-channel');
    channel.bind('my-event', function(data) {
      events.push(data);
    });
  </script>
</head>
<body>
  <h1>Pusher Test</h1>
  <p>
    Try publishing an event to channel <code>my-channel</code>
    with event name <code>my-event</code>.
  </p>
</body>
`)
}

func TestWebsocketSignature(t *testing.T) {
	for _, isSecure := range []bool{false, true} {
		ts := spawnServers(t, websocketSignatureContent, isSecure)
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
		require.True(t, ok, fmt.Sprintf("secure is %v", isSecure))
	}
}
