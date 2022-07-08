package pusher

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestChannelSubscribe(t *testing.T) {
	t.Run("subscribed", func(t *testing.T) {
		ch := &channel{
			subscribed: true,
		}

		err := ch.Subscribe()
		if err != nil {
			panic(err)
		}
	})

	t.Run("subscribeSuccess", func(t *testing.T) {
		wantChannel := "foo"

		srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
			var event Event
			err := websocket.JSON.Receive(ws, &event)
			if err != nil {
				panic(err)
			}

			if event.Event != pusherSubscribe {
				t.Errorf("Expected to get subscribe event, got %+v", event)
			}
			if !reflect.DeepEqual(event.Data, json.RawMessage(`{"channel":"`+wantChannel+`"}`)) {
				t.Errorf("Expected subscribe data to have channel %q, got %q", wantChannel, event.Data)
			}

			err = websocket.JSON.Send(ws, Event{
				Event:   pusherInternalSubSucceeded,
				Channel: wantChannel,
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

		ch := &channel{
			name:       wantChannel,
			subscribed: false,
			client: &Client{
				ws:        ws,
				connected: true,
			},
		}
		ch.client.subscribedChannels = subscribedChannels{wantChannel: ch}
		defer ch.client.Disconnect()

		go ch.client.listen()

		successTimeout := 10 * time.Millisecond
		err = ch.Subscribe(WithSuccessTimeout(successTimeout))
		if err != nil {
			panic(err)
		}
	})

	t.Run("subscribeTimeout", func(t *testing.T) {
		srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {}))
		defer srv.Close()
		wsURL := strings.Replace(srv.URL, "http", "ws", 1)
		ws, err := websocket.Dial(wsURL, "ws", localOrigin)
		if err != nil {
			panic(err)
		}

		ch := &channel{
			client: &Client{
				ws:        ws,
				connected: true,
			},
		}
		defer ch.client.Disconnect()

		successTimeout := 10 * time.Millisecond
		waitTimeout := 2 * successTimeout
		timer := time.NewTimer(waitTimeout)

		go func() {
			<-timer.C
			t.Errorf("Expected to timeout in %s, waited %s with no timeout", successTimeout, waitTimeout)
		}()

		err = ch.Subscribe(WithSuccessTimeout(successTimeout))
		timer.Stop()
		if err != ErrTimedOut {
			t.Errorf("Expected to get error %s, got %v", ErrTimedOut, err)
		}
	})
}

func TestChannelUnsubscribe(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(1)
	srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		var event Event
		err := websocket.JSON.Receive(ws, &event)
		if err != nil {
			panic(err)
		}

		if event.Event != pusherUnsubscribe {
			t.Errorf("Expected to get subscribe event, got %+v", event)
		}
		if !reflect.DeepEqual(event.Data, json.RawMessage(`{"channel":"foo"}`)) {
			t.Errorf("Expected unsubscribe data to have channel 'foo', got %s", event.Data)
		}

		wg.Done()
	}))
	defer srv.Close()
	wsURL := strings.Replace(srv.URL, "http", "ws", 1)
	ws, err := websocket.Dial(wsURL, "ws", localOrigin)
	if err != nil {
		panic(err)
	}

	ch := &channel{
		name:       "foo",
		subscribed: true,
		client: &Client{
			ws: ws,
		},
	}
	defer ch.client.Disconnect()

	err = ch.Unsubscribe()
	if err != nil {
		panic(err)
	}

	if ch.subscribed != false {
		t.Errorf("Expected channel subscribe to be false, got true")
	}

	wg.Wait()
}

func TestChannelBind(t *testing.T) {
	ch := &channel{boundEvents: map[string]boundDataChans{}}
	boundChan := ch.Bind("foo")

	dataBoundChans, ok := ch.boundEvents["foo"]
	if !ok {
		t.Errorf("Expected channel bound events to contain 'foo', got %+v instead", ch.boundEvents)
	}
	_, ok = dataBoundChans[boundChan]
	if !ok {
		t.Errorf("Expected data bound channels to contain returned channel, got %+v instead", dataBoundChans)
	}
}

func TestChannelUnbind(t *testing.T) {
	t.Run("eventOnly", func(t *testing.T) {
		ch := &channel{boundEvents: map[string]boundDataChans{
			"foo": {make(chan json.RawMessage): make(chan struct{})},
		}}
		ch.Unbind("foo")

		if _, ok := ch.boundEvents["foo"]; ok {
			t.Errorf("Expected channel bound events not to contain 'foo', got %+v instead", ch.boundEvents)
		}
	})

	t.Run("eventWithChans", func(t *testing.T) {
		ch1 := make(chan json.RawMessage)
		ch2 := make(chan json.RawMessage)
		ch3 := make(chan json.RawMessage)
		ch := &channel{boundEvents: map[string]boundDataChans{
			"foo": {
				ch1: make(chan struct{}),
				ch2: make(chan struct{}),
				ch3: make(chan struct{}),
			},
		}}
		ch.Unbind("foo", ch1, ch3)

		dataBoundChans, ok := ch.boundEvents["foo"]
		if !ok {
			t.Errorf("Expected channel bound events to contain 'foo', got %+v instead", ch.boundEvents)
		}
		_, ok = dataBoundChans[ch1]
		if ok {
			t.Errorf("Expected data bound channels not to contain ch1, got %+v instead", dataBoundChans)
		}
		_, ok = dataBoundChans[ch3]
		if ok {
			t.Errorf("Expected data bound channels not to contain ch3, got %+v instead", dataBoundChans)
		}
		_, ok = dataBoundChans[ch2]
		if !ok {
			t.Errorf("Expected data bound channels to contain ch3, got %+v instead", dataBoundChans)
		}
	})
}

func TestChannelHandleEvent(t *testing.T) {
	t.Run("boundEvent", func(t *testing.T) {
		wantData := json.RawMessage(`{"hello":"world"}`)
		wantEvent := "foo"

		dataChan := make(chan json.RawMessage)
		ch := &channel{
			boundEvents: map[string]boundDataChans{
				wantEvent: {dataChan: make(chan struct{})},
			},
		}

		ch.handleEvent(wantEvent, wantData)

		if gotData := <-dataChan; !reflect.DeepEqual(gotData, wantData) {
			t.Errorf("Expected to receive data %+v, got %+v", wantData, gotData)
		}
	})

	t.Run("subscribeSuccess", func(t *testing.T) {
		ch := &channel{
			subscribed: false,
		}

		ch.handleEvent(pusherInternalSubSucceeded, nil)

		if ch.subscribed != true {
			t.Errorf("Expected to channel subscribed to be true, got false")
		}
	})
}

func TestChannelTrigger(t *testing.T) {
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

	ch := &channel{
		name:   wantEvent.Channel,
		client: client,
	}

	err = ch.Trigger(wantEvent.Event, wantEvent.Data)
	if err != nil {
		panic(err)
	}

	wg.Wait()
}

func TestAuthErrorError(t *testing.T) {
	err := AuthError{
		Status: 123,
		Body:   "foo",
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "status code 123") {
		t.Errorf("Expected error message to contain 'status code 123', got %s", errMsg)
	}
	if !strings.Contains(errMsg, `body: "foo"`) {
		t.Errorf(`Expected error message to contain 'body: "foo"', got %s`, errMsg)
	}
}

func TestPrivateChannelSubscribe(t *testing.T) {
	t.Run("subscribed", func(t *testing.T) {
		ch := &privateChannel{
			&channel{
				subscribed: true,
			},
		}

		err := ch.Subscribe()
		if err != nil {
			panic(err)
		}
	})

	t.Run("subscribeSuccess", func(t *testing.T) {
		wantChannel := "foo"
		wantSocketID := "bar"
		wantAuth := "baz"
		wantParams := url.Values{"foo": {"bar"}}
		wantHeaders := http.Header{"Authorization": {"Bearer baz"}}

		wg := &sync.WaitGroup{}
		wg.Add(1)
		srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
			var event Event
			err := websocket.JSON.Receive(ws, &event)
			if err != nil {
				panic(err)
			}

			if event.Event != pusherSubscribe {
				t.Errorf("Expected to get subscribe event, got %+v", event)
			}

			data := channelData{}
			err = json.Unmarshal(event.Data, &data)
			if err != nil {
				panic(err)
			}

			if data.Channel != wantChannel {
				t.Errorf("Expected subscribe data to have channel %q, got %q", wantChannel, data.Channel)
			}
			if data.Auth != wantAuth {
				t.Errorf("Expected subscribe data to have auth %q, got %q", wantAuth, data.Auth)
			}

			err = websocket.JSON.Send(ws, Event{
				Event:   pusherInternalSubSucceeded,
				Channel: wantChannel,
			})
			if err != nil {
				panic(err)
			}

			wg.Done()
		}))
		defer srv.Close()
		wsURL := strings.Replace(srv.URL, "http", "ws", 1)
		ws, err := websocket.Dial(wsURL, "ws", localOrigin)
		if err != nil {
			panic(err)
		}

		authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if gotSocketID := r.PostFormValue("socket_id"); gotSocketID != wantSocketID {
				t.Errorf("Expected socket_id param to be %q, got %q", wantSocketID, gotSocketID)
			}
			if gotChannel := r.PostFormValue("channel_name"); gotChannel != wantChannel {
				t.Errorf("Expected channel param to be %q, got %q", wantChannel, gotChannel)
			}
			for key := range wantParams {
				wantVal := wantParams.Get(key)
				if gotVal := r.PostFormValue(key); gotVal != wantVal {
					t.Errorf("Expected param %q to be %q, got %q", key, wantVal, gotVal)
				}
			}
			for key := range wantHeaders {
				wantVal := wantHeaders.Get(key)
				if gotVal := r.Header.Get(key); gotVal != wantVal {
					t.Errorf("Expected header %q to be %q, got %q", key, wantVal, gotVal)
				}
			}

			err = json.NewEncoder(w).Encode(channelData{
				Auth: wantAuth,
			})
			if err != nil {
				panic(err)
			}
		}))
		defer authSrv.Close()

		ch := &privateChannel{
			&channel{
				name:       wantChannel,
				subscribed: false,
				client: &Client{
					ws:          ws,
					connected:   true,
					socketID:    wantSocketID,
					AuthURL:     authSrv.URL,
					AuthParams:  wantParams,
					AuthHeaders: wantHeaders,
				},
			},
		}
		ch.client.subscribedChannels = subscribedChannels{wantChannel: ch}
		defer ch.client.Disconnect()

		go ch.client.listen()

		successTimeout := 100 * time.Millisecond
		err = ch.Subscribe(WithSuccessTimeout(successTimeout))
		if err != nil {
			panic(err)
		}

		wg.Wait()
	})

	t.Run("authFail", func(t *testing.T) {
		wantStatus := http.StatusForbidden

		authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(wantStatus)
		}))
		defer authSrv.Close()

		ch := &privateChannel{
			&channel{
				subscribed: false,
				client: &Client{
					AuthURL: authSrv.URL,
				},
			},
		}

		err := ch.Subscribe()
		authErr := err.(AuthError)
		if authErr.Status != wantStatus {
			t.Errorf("Expected auth error status to be %d, got %d", wantStatus, authErr.Status)
		}
	})
}

func TestChannelIsSubscribed(t *testing.T) {
	ch := &channel{
		subscribed: true,
	}

	if ch.IsSubscribed() != true {
		t.Errorf("Expected channel subscribe to be true, got false")
	}
}
