package pusher

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

// TODO Implement Client.Error channel in all tests and fail the test if any
// errors occur

func TestUnmarshalDataString(t *testing.T) {
	dest := map[string]interface{}{}
	err := UnmarshalDataString(json.RawMessage(`"{\"foo\":\"A\",\"bar\":1}"`), &dest)
	if err != nil {
		t.Errorf("Expected error to be `nil`, got %v", err)
	}

	wantData := map[string]interface{}{
		"foo": "A",
		"bar": 1.0,
	}
	if !reflect.DeepEqual(dest, wantData) {
		t.Errorf("Expected dest to deep-equal %+v, got %+v", wantData, dest)
	}
}

func TestClientIsConnected(t *testing.T) {
	t.Run("false", func(t *testing.T) {
		client := &Client{connected: false}
		if isConnected := client.isConnected(); isConnected != false {
			t.Errorf("Expected isConnected to return false, got %v", isConnected)
		}
	})
	t.Run("true", func(t *testing.T) {
		client := &Client{connected: true}
		if isConnected := client.isConnected(); isConnected != true {
			t.Errorf("Expected isConnected to return true, got %v", isConnected)
		}
	})
}

func TestClientResetActivityTimer(t *testing.T) {
	resetChan := make(chan struct{})
	client := &Client{activityTimerReset: resetChan}

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		<-resetChan
		wg.Done()
	}()

	client.resetActivityTimer()

	wg.Wait()
}

func TestClientSendError(t *testing.T) {
	errChan := make(chan error)
	wantErr := errors.New("foo")
	client := &Client{Errors: errChan}

	var gotErr error
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() { gotErr = <-errChan; wg.Done() }()
	runtime.Gosched()
	client.sendError(wantErr)
	wg.Wait()

	if !reflect.DeepEqual(gotErr, wantErr) {
		t.Errorf("Expected to value from error chan to be %+v, got %+v", wantErr, gotErr)
	}
}

func TestClientBind(t *testing.T) {
	wantChan := "foo"
	client := Client{boundEvents: map[string]boundEventChans{}}
	boundChan := client.Bind(wantChan)

	eventBoundChans, ok := client.boundEvents[wantChan]
	if !ok {
		t.Errorf("Expected client bound events to contain %q, got %+v instead", wantChan, client.boundEvents)
	}
	_, ok = eventBoundChans[boundChan]
	if !ok {
		t.Errorf("Expected event bound channels to contain returned channel, got %+v instead", eventBoundChans)
	}
}

func TestClientUnbind(t *testing.T) {
	wantChan := "foo"
	t.Run("eventOnly", func(t *testing.T) {
		client := Client{boundEvents: map[string]boundEventChans{
			wantChan: {make(chan Event): struct{}{}},
		}}
		client.Unbind(wantChan)

		if _, ok := client.boundEvents[wantChan]; ok {
			t.Errorf("Expected client bound events not to contain %q, got %+v instead", wantChan, client.boundEvents)
		}
	})

	t.Run("eventWithChans", func(t *testing.T) {
		wantChan := "foo"
		ch1 := make(chan Event)
		ch2 := make(chan Event)
		ch3 := make(chan Event)
		client := Client{boundEvents: map[string]boundEventChans{
			wantChan: {
				ch1: struct{}{},
				ch2: struct{}{},
				ch3: struct{}{},
			},
		}}
		client.Unbind(wantChan, ch1, ch3)

		eventBoundChans, ok := client.boundEvents[wantChan]
		if !ok {
			t.Errorf("Expected client bound events to contain %q, got %+v instead", wantChan, client.boundEvents)
		}
		_, ok = eventBoundChans[ch1]
		if ok {
			t.Errorf("Expected event bound channels not to contain ch1, got %+v instead", eventBoundChans)
		}
		_, ok = eventBoundChans[ch3]
		if ok {
			t.Errorf("Expected event bound channels not to contain ch3, got %+v instead", eventBoundChans)
		}
		_, ok = eventBoundChans[ch2]
		if !ok {
			t.Errorf("Expected event bound channels to contain ch3, got %+v instead", eventBoundChans)
		}
	})
}

func TestClientSendEvent(t *testing.T) {
	wantEvent := Event{
		Channel: "foo",
		Event:   "bar",
		Data:    json.RawMessage(`{"bar":1,"foo":"A"}`),
	}

	wg := &sync.WaitGroup{}
	wg.Add(1)
	srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		event := Event{}
		err := websocket.JSON.Receive(ws, &event)
		if err != nil {
			panic(err)
		}
		if !reflect.DeepEqual(event, wantEvent) {
			t.Errorf("Expected received event to deep-equal %+v, got %+v", wantEvent, event)
		}
		wg.Done()
	}))
	defer srv.Close()
	wsURL := strings.Replace(srv.URL, "http", "ws", 1)
	ws, err := websocket.Dial(wsURL, "ws", localOrigin)
	if err != nil {
		panic(err)
	}

	client := &Client{
		ws:                 ws,
		activityTimerReset: make(chan struct{}),
	}
	defer client.Disconnect()

	err = client.SendEvent(wantEvent.Event, wantEvent.Data, wantEvent.Channel)
	if err != nil {
		panic(err)
	}
	wg.Wait()

	<-client.activityTimerReset
}

func TestClientSubscribe(t *testing.T) {
	t.Run("existingSubscription", func(t *testing.T) {
		srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {}))
		defer srv.Close()
		wsURL := strings.Replace(srv.URL, "http", "ws", 1)
		ws, err := websocket.Dial(wsURL, "ws", localOrigin)
		if err != nil {
			panic(err)
		}

		channelName := "foo"
		ch := &channel{name: channelName, subscribed: true}
		client := &Client{
			subscribedChannels: map[string]internalChannel{channelName: ch},
			ws:                 ws,
		}
		defer client.Disconnect()
		ch.client = client
		subCh, err := client.Subscribe(channelName)
		if err != nil {
			panic(err)
		}

		if ch != subCh.(*channel) {
			t.Errorf("Expected subCh to be %+v, got %+v", ch, subCh)
		}
	})

	t.Run("newPublicChannel", func(t *testing.T) {
		channelName := "foo"

		srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
			var evt Event
			err := websocket.JSON.Receive(ws, &evt)
			if err != nil {
				panic(err)
			}
			if evt.Event != pusherSubscribe {
				t.Errorf("Expected event to be %q, got %q", pusherSubscribe, evt.Event)
			}

			err = websocket.JSON.Send(ws, Event{
				Event:   pusherInternalSubSucceeded,
				Channel: channelName,
			})
			if err != nil {
				panic(err)
			}
		}))
		defer srv.Close()
		wsURL := strings.Replace(srv.URL, "http", "ws", 1)
		ws, err := websocket.Dial(wsURL, "ws", localOrigin)
		if err != nil {
			panic(err)
		}

		client := &Client{
			subscribedChannels: map[string]internalChannel{},
			ws:                 ws,
			connected:          true,
		}
		defer client.Disconnect()

		go client.listen()

		subCh, err := client.Subscribe(channelName)
		if err != nil {
			panic(err)
		}

		baseCh := subCh.(*channel)
		if baseCh.name != channelName {
			t.Errorf("Expected channel name to be %q, got %q", channelName, baseCh.name)
		}
	})

	t.Run("newPrivateChannel", func(t *testing.T) {
		channelName := "private-foo"

		srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
			var evt Event
			err := websocket.JSON.Receive(ws, &evt)
			if err != nil {
				panic(err)
			}
			if evt.Event != pusherSubscribe {
				t.Errorf("Expected event to be %q, got %q", pusherSubscribe, evt.Event)
			}

			err = websocket.JSON.Send(ws, Event{
				Event:   pusherInternalSubSucceeded,
				Channel: channelName,
			})
			if err != nil {
				panic(err)
			}
		}))
		defer srv.Close()
		wsURL := strings.Replace(srv.URL, "http", "ws", 1)
		ws, err := websocket.Dial(wsURL, "ws", localOrigin)
		if err != nil {
			panic(err)
		}

		authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{}`))
		}))

		client := &Client{
			subscribedChannels: map[string]internalChannel{},
			ws:                 ws,
			connected:          true,
			AuthURL:            authSrv.URL,
		}
		defer client.Disconnect()

		go client.listen()

		subCh, err := client.Subscribe(channelName)
		if err != nil {
			panic(err)
		}

		baseCh := subCh.(*privateChannel)
		if baseCh.name != channelName {
			t.Errorf("Expected channel name to be %q, got %q", channelName, baseCh.name)
		}
	})

	t.Run("newPresenceChannel", func(t *testing.T) {
		channelName := "presence-foo"

		srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
			var evt Event
			err := websocket.JSON.Receive(ws, &evt)
			if err != nil {
				panic(err)
			}
			if evt.Event != pusherSubscribe {
				t.Errorf("Expected event to be %q, got %q", pusherSubscribe, evt.Event)
			}

			data, err := json.Marshal(`
				{
					"presence": {
						"ids": ["1", "2"],
						"hash": {
							"1": { "name": "name-1" },
							"2": { "name": "name-2" }
						},
						"count": 2
					}
				}
			`)
			if err != nil {
				t.Fatal("error marshaling data: ", err)
			}

			err = websocket.JSON.Send(ws, Event{
				Event:   pusherInternalSubSucceeded,
				Channel: channelName,
				Data:    data,
			})
			if err != nil {
				panic(err)
			}
		}))
		defer srv.Close()
		wsURL := strings.Replace(srv.URL, "http", "ws", 1)
		ws, err := websocket.Dial(wsURL, "ws", localOrigin)
		if err != nil {
			panic(err)
		}

		authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{}`))
		}))

		client := &Client{
			subscribedChannels: map[string]internalChannel{},
			ws:                 ws,
			connected:          true,
			AuthURL:            authSrv.URL,
		}
		defer client.Disconnect()

		go client.listen()

		subCh, err := client.SubscribePresence(channelName)
		if err != nil {
			panic(err)
		}

		baseCh := subCh.(*presenceChannel)
		if baseCh.name != channelName {
			t.Errorf("Expected channel name to be %q, got %q", channelName, baseCh.name)
		}
	})
}

func TestClientUnsubscribe(t *testing.T) {
	srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {}))
	defer srv.Close()
	wsURL := strings.Replace(srv.URL, "http", "ws", 1)
	ws, err := websocket.Dial(wsURL, "ws", localOrigin)
	if err != nil {
		panic(err)
	}

	ch := &channel{name: "foo"}
	client := &Client{
		subscribedChannels: map[string]internalChannel{"foo": ch},
		ws:                 ws,
	}
	defer client.Disconnect()
	ch.client = client
	err = client.Unsubscribe("foo")
	if err != nil {
		panic(err)
	}

	if _, ok := client.subscribedChannels["foo"]; ok {
		t.Errorf("Expected client subscribed channels not to contain 'foo', got %+v", client.subscribedChannels)
	}
}

func TestClientDisconnect(t *testing.T) {
	srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {}))
	defer srv.Close()
	wsURL := strings.Replace(srv.URL, "http", "ws", 1)
	ws, err := websocket.Dial(wsURL, "ws", localOrigin)
	if err != nil {
		panic(err)
	}

	client := &Client{
		connected: true,
		ws:        ws,
	}

	err = client.Disconnect()
	if err != nil {
		panic(err)
	}

	if client.connected {
		t.Errorf("Expected client connected to be false, got true")
	}
	if err = ws.Close(); err == nil {
		t.Errorf("Expected websocket connection to have been closed, got no error closing again")
	}
}

func TestClientHeartbeat(t *testing.T) {
	t.Run("notConnected", func(t *testing.T) {
		timeChan := make(chan time.Time)
		client := &Client{
			connected:          false,
			activityTimerReset: make(chan struct{}),
			activityTimer:      &time.Timer{C: timeChan},
		}

		go func() {
			client.activityTimerReset <- struct{}{}
			t.Errorf("Expected not to block on send to activityTimerReset, but message was received")
		}()
		go func() {
			timeChan <- time.Now()
			t.Errorf("Expected not to block on send to activityTimer chan, but message was received")
		}()

		client.heartbeat()
	})

	t.Run("timerReset", func(t *testing.T) {
		srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {}))
		defer srv.Close()
		wsURL := strings.Replace(srv.URL, "http", "ws", 1)
		ws, err := websocket.Dial(wsURL, "ws", localOrigin)
		if err != nil {
			panic(err)
		}

		client := &Client{
			connected:          true,
			activityTimerReset: make(chan struct{}),
			activityTimer:      time.NewTimer(1 * time.Hour),
			activityTimeout:    0,
			ws:                 ws,
		}

		go client.heartbeat()
		runtime.Gosched()

		client.Disconnect()
		client.activityTimerReset <- struct{}{}

		<-client.activityTimer.C

	})

	t.Run("timerExpire", func(t *testing.T) {
		wg := &sync.WaitGroup{}
		wg.Add(1)
		srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
			var event Event
			err := websocket.JSON.Receive(ws, &event)
			if err != nil {
				panic(err)
			}

			if event.Event != pusherPing {
				t.Errorf("Expected to get ping event, got %+v", event)
			}
			wg.Done()
		}))
		defer srv.Close()
		wsURL := strings.Replace(srv.URL, "http", "ws", 1)
		ws, err := websocket.Dial(wsURL, "ws", localOrigin)
		if err != nil {
			panic(err)
		}

		client := &Client{
			connected:     true,
			activityTimer: time.NewTimer(0),
			ws:            ws,
		}
		defer client.Disconnect()

		go client.heartbeat()

		wg.Wait()
	})
}

func TestClientListen(t *testing.T) {
	t.Run("notConnected", func(t *testing.T) {
		client := &Client{
			connected: false,
		}

		client.heartbeat()
	})

	t.Run("receivePing", func(t *testing.T) {
		wg := &sync.WaitGroup{}
		wg.Add(1)
		srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
			websocket.Message.Send(ws, pingPayload)

			var event Event
			err := websocket.JSON.Receive(ws, &event)
			if err != nil {
				panic(err)
			}

			if event.Event != pusherPong {
				t.Errorf("Expected to get pong event, got %+v", event)
			}
			wg.Done()
		}))
		defer srv.Close()
		wsURL := strings.Replace(srv.URL, "http", "ws", 1)
		ws, err := websocket.Dial(wsURL, "ws", localOrigin)
		if err != nil {
			panic(err)
		}

		client := &Client{
			connected: true,
			ws:        ws,
		}
		defer client.Disconnect()

		go client.listen()

		wg.Wait()
	})

	t.Run("receiveEvent", func(t *testing.T) {
		wantData := json.RawMessage(`{"hello":"world"}`)
		wantEvent := Event{
			Event:   "foo",
			Channel: "bar",
			Data:    wantData,
		}
		srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
			websocket.JSON.Send(ws, wantEvent)
		}))
		defer srv.Close()
		wsURL := strings.Replace(srv.URL, "http", "ws", 1)
		ws, err := websocket.Dial(wsURL, "ws", localOrigin)
		if err != nil {
			panic(err)
		}

		eventChan := make(chan Event)
		dataChan := make(chan json.RawMessage)
		client := &Client{
			connected: true,
			ws:        ws,
			boundEvents: map[string]boundEventChans{
				wantEvent.Event: {eventChan: struct{}{}},
			},
			subscribedChannels: map[string]internalChannel{
				wantEvent.Channel: &channel{
					boundEvents: map[string]boundDataChans{
						wantEvent.Event: {dataChan: make(chan struct{})},
					},
				},
			},
		}
		defer client.Disconnect()

		wg := &sync.WaitGroup{}
		wg.Add(1)
		go func() {
			if gotEvent := <-eventChan; !reflect.DeepEqual(gotEvent, wantEvent) {
				t.Errorf("Expected to receive event %+v, got %+v", wantEvent, gotEvent)
			}
			if gotData := <-dataChan; !reflect.DeepEqual(gotData, wantData) {
				t.Errorf("Expected to receive data %+v, got %+v", wantData, gotData)
			}
			wg.Done()
		}()

		go client.listen()

		wg.Wait()
	})

	t.Run("receiveError", func(t *testing.T) {
		wantError := EventError{
			Code:    1234,
			Message: "foo",
		}
		errData, err := json.Marshal(wantError)
		if err != nil {
			panic(err)
		}
		srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
			for {
				websocket.JSON.Send(ws, Event{Event: pusherError, Data: errData})
			}
		}))
		defer srv.Close()
		wsURL := strings.Replace(srv.URL, "http", "ws", 1)
		ws, err := websocket.Dial(wsURL, "ws", localOrigin)
		if err != nil {
			panic(err)
		}

		client := &Client{
			connected: true,
			ws:        ws,
			Errors:    make(chan error),
		}
		defer client.Disconnect()

		wg := &sync.WaitGroup{}
		wg.Add(1)
		go func() {
			if gotError := <-client.Errors; !reflect.DeepEqual(gotError, wantError) {
				t.Errorf("Expected to receive event %+v, got %+v", wantError, gotError)
			}
			wg.Done()
		}()

		go client.listen()

		wg.Wait()
	})
}

func TestClientGenerateConnURL(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		wantAppKey := "foo"
		client := &Client{}
		gotURL := client.generateConnURL(wantAppKey)
		if !strings.Contains(gotURL, secureScheme) {
			t.Errorf("Expected connection URL to have secure scheme, got %q", gotURL)
		}
		if !strings.Contains(gotURL, fmt.Sprint(securePort)) {
			t.Errorf("Expected connection URL to have secure port, got %q", gotURL)
		}
		if !strings.Contains(gotURL, defaultHost) {
			t.Errorf("Expected connection URL to have default host, got %q", gotURL)
		}
		if !strings.Contains(gotURL, wantAppKey) {
			t.Errorf("Expected connection URL to have app key, got %q", gotURL)
		}
	})

	t.Run("custom", func(t *testing.T) {
		wantAppKey := "foo"
		client := &Client{
			Insecure: true,
			Cluster:  "bar",
		}
		gotURL := client.generateConnURL(wantAppKey)
		if !strings.Contains(gotURL, insecureScheme) {
			t.Errorf("Expected connection URL to have insecure scheme, got %q", gotURL)
		}
		if !strings.Contains(gotURL, fmt.Sprint(insecurePort)) {
			t.Errorf("Expected connection URL to have insecure port, got %q", gotURL)
		}
		if !strings.Contains(gotURL, "pusher.com") {
			t.Errorf("Expected connection URL to have pusher.com, got %q", gotURL)
		}
		if !strings.Contains(gotURL, client.Cluster) {
			t.Errorf("Expected connection URL to have custom cluster, got %q", gotURL)
		}
		if !strings.Contains(gotURL, wantAppKey) {
			t.Errorf("Expected connection URL to have app key, got %q", gotURL)
		}
	})

	t.Run("override", func(t *testing.T) {
		client := &Client{
			overrideHost: "foo.bar",
			overridePort: 1234,
		}

		gotURL := client.generateConnURL("")
		if !strings.Contains(gotURL, client.overrideHost) {
			t.Errorf("Expected connection URL to have override host, got %q", gotURL)
		}
		if !strings.Contains(gotURL, fmt.Sprint(client.overridePort)) {
			t.Errorf("Expected connection URL to have override port, got %q", gotURL)
		}
	})
}

func TestClientConnect(t *testing.T) {
	t.Run("pusherError", func(t *testing.T) {
		wantError := EventError{
			Code:    1234,
			Message: "foo",
		}
		errData, err := json.Marshal(wantError)
		if err != nil {
			panic(err)
		}
		srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
			for {
				websocket.JSON.Send(ws, Event{Event: pusherError, Data: errData})
			}
		}))
		defer srv.Close()

		host, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
		if err != nil {
			panic(err)
		}
		portNum, err := strconv.Atoi(port)
		if err != nil {
			panic(err)
		}

		client := &Client{
			Insecure:     true,
			overrideHost: host,
			overridePort: portNum,
		}
		defer client.Disconnect()

		connectErr := client.Connect("")
		if !reflect.DeepEqual(connectErr, wantError) {
			t.Errorf("Expected error to deep-equal %+v, got %+v", wantError, connectErr)
		}
	})

	t.Run("connectionEstablished", func(t *testing.T) {
		wantConnData := connectionData{
			SocketID:        "foo",
			ActivityTimeout: 1234,
		}
		connData, err := json.Marshal(wantConnData)
		if err != nil {
			panic(err)
		}
		connDataStr, err := json.Marshal(string(connData))
		if err != nil {
			panic(err)
		}
		srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
			for {
				websocket.JSON.Send(ws, Event{Event: pusherConnEstablished, Data: connDataStr})
			}
		}))
		defer srv.Close()

		host, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
		if err != nil {
			panic(err)
		}
		portNum, err := strconv.Atoi(port)
		if err != nil {
			panic(err)
		}

		client := &Client{
			Insecure:     true,
			overrideHost: host,
			overridePort: portNum,
		}
		defer client.Disconnect()

		err = client.Connect("")
		if err != nil {
			panic(err)
		}

		if client.connected != true {
			t.Errorf("Expected client connected to be true, got false")
		}
		if client.socketID != wantConnData.SocketID {
			t.Errorf("Expected client socket ID to be %v, got %v", wantConnData.SocketID, client.socketID)
		}
		wantTimeout := time.Duration(wantConnData.ActivityTimeout) * time.Second
		if client.activityTimeout != wantTimeout {
			t.Errorf("Expected client activity timeout to be %v, got %v", wantTimeout, client.activityTimeout)
		}
	})
}
