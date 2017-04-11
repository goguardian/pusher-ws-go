package pusher

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

const (
	pingPayload = `{"event":"pusher:ping","data":"{}"}`
	pongPayload = `{"event":"pusher:pong","data":"{}"}`

	pusherPing                 = "pusher:ping"
	pusherPong                 = "pusher:pong"
	pusherError                = "pusher:error"
	pusherSubscribe            = "pusher:subscribe"
	pusherUnsubscribe          = "pusher:unsubscribe"
	pusherConnEstablished      = "pusher:connection_established"
	pusherSubSucceeded         = "pusher:subscription_succeeded"
	pusherInternalSubSucceeded = "pusher_internal:subscription_succeeded"

	localOrigin = "http://localhost/"

	connURLFormat     = "%s://%s:%d/app/%s?protocol=%s"
	secureScheme      = "wss"
	securePort        = 443
	insecureScheme    = "ws"
	insecurePort      = 80
	defaultHost       = "ws.pusherapp.com"
	clusterHostFormat = "ws-%s.pusher.com"
	protocolVersion   = "7"
)

type boundEventChans map[chan Event]struct{}

type subscribedChannels map[string]Channel

type Client struct {
	AuthURL  string
	Insecure bool
	Cluster  string

	Errors chan error

	socketID string
	// TODO: make this configurable
	activityTimeout time.Duration
	// TODO: implement timeout logic
	// pongTimeout time.Duration

	ws                 *websocket.Conn
	connected          bool
	activityTimer      *time.Timer
	activityTimerReset chan struct{}
	boundEvents        map[string]boundEventChans
	// TODO: implement global bindings
	// globalBindings     boundEventChans
	subscribedChannels subscribedChannels

	mutex sync.RWMutex
}

type connectionData struct {
	SocketID        string `json:"socket_id"`
	ActivityTimeout int    `json:"activity_timeout"`
}

func UnmarshalDataString(data json.RawMessage, dest interface{}) error {
	var dataStr string
	err := json.Unmarshal(data, &dataStr)
	if err != nil {
		return err
	}

	return json.Unmarshal([]byte(dataStr), dest)
}

func (c *Client) Connect(appKey string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	scheme, port := secureScheme, securePort
	if c.Insecure {
		scheme, port = insecureScheme, insecurePort
	}

	host := defaultHost
	if c.Cluster != "" {
		host = fmt.Sprintf(clusterHostFormat, c.Cluster)
	}

	connURL := fmt.Sprintf(connURLFormat, scheme, host, port, appKey, protocolVersion)

	var err error
	c.ws, err = websocket.Dial(connURL, "", localOrigin)
	if err != nil {
		return err
	}

	var event Event
	err = websocket.JSON.Receive(c.ws, &event)
	if err != nil {
		return err
	}

	switch event.Event {
	case pusherError:
		return extractEventError(event)
	case pusherConnEstablished:
		var connData connectionData
		err = UnmarshalDataString(event.Data, &connData)
		if err != nil {
			return err
		}
		c.connected = true
		c.socketID = connData.SocketID
		c.activityTimeout = time.Duration(connData.ActivityTimeout) * time.Second
		c.activityTimer = time.NewTimer(c.activityTimeout)
		c.boundEvents = map[string]boundEventChans{}
		c.subscribedChannels = subscribedChannels{}

		go c.heartbeat()
		go c.listen()

		return nil
	default:
		return fmt.Errorf("Got unknown event type from Pusher: %s", event.Event)
	}
}

func (c *Client) isConnected() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return c.connected
}

func (c *Client) resetActivityTimer() {
	go func() { c.activityTimerReset <- struct{}{} }()
}

func (c *Client) heartbeat() {
	for c.isConnected() {
		select {
		case <-c.activityTimerReset:
			if !c.activityTimer.Stop() {
				<-c.activityTimer.C
			}
			c.activityTimer.Reset(c.activityTimeout)
		case <-c.activityTimer.C:
			websocket.Message.Send(c.ws, pingPayload)
			// TODO: implement timeout/reconnect logic
		}
	}
}

func (c *Client) sendError(err error) {
	select {
	case c.Errors <- err:
	default:
	}
}

func (c *Client) listen() {
	for c.isConnected() {
		var event Event
		err := websocket.JSON.Receive(c.ws, &event)
		if err != nil {
			// If the websocket connection was closed, Receive will return an error.
			// This is expected for an explicit disconnect.
			if !c.isConnected() {
				return
			}
			log.Println(err)
			c.sendError(err)
			continue
		}

		c.resetActivityTimer()

		switch event.Event {
		case pusherPing:
			websocket.Message.Send(c.ws, pongPayload)
		case pusherPong:
			// TODO: stop pong timeout timer
		case pusherError:
			c.sendError(extractEventError(event))
		default:
			c.mutex.RLock()
			for boundChan := range c.boundEvents[event.Event] {
				go func(boundChan chan Event, event Event) {
					boundChan <- event
				}(boundChan, event)
			}
			if subChan, ok := c.subscribedChannels[event.Channel]; ok {
				subChan.handleEvent(event.Event, event.Data)
			}
			c.mutex.RUnlock()
		}
	}
}

func (c *Client) Subscribe(channelName string) (Channel, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	ch, ok := c.subscribedChannels[channelName]

	if !ok {
		baseChan := &channel{
			name:        channelName,
			boundEvents: map[string]boundDataChans{},
			client:      c,
		}
		switch {
		case strings.HasPrefix(channelName, "private-"):
			ch = &privateChannel{baseChan}
		case strings.HasPrefix(channelName, "presence-"):
			ch = &privateChannel{baseChan}
			// TODO: implement presence channels
			// ch = presenceChannel{baseChan}
		default:
			ch = baseChan
		}
	}

	return ch, ch.Subscribe()
}

func (c *Client) Unsubscribe(channelName string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	ch, ok := c.subscribedChannels[channelName]
	if !ok {
		return nil
	}

	delete(c.subscribedChannels, channelName)
	return ch.Unsubscribe()
}

func (c *Client) Bind(event string) chan Event {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	boundChan := make(chan Event)

	if c.boundEvents[event] == nil {
		c.boundEvents[event] = boundEventChans{}
	}
	c.boundEvents[event][boundChan] = struct{}{}

	return boundChan
}

func (c *Client) Unbind(event string, chans ...chan Event) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if len(chans) == 0 {
		delete(c.boundEvents, event)
		return
	}

	eventBoundChans := c.boundEvents[event]
	for _, boundChan := range chans {
		delete(eventBoundChans, boundChan)
	}
}

func (c *Client) SendEvent(event string, data interface{}, channelName string) error {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return err
	}

	e := Event{
		Event:   event,
		Data:    dataJSON,
		Channel: channelName,
	}

	c.resetActivityTimer()

	return websocket.JSON.Send(c.ws, e)
}

func (c *Client) Disconnect() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.connected = false

	return c.ws.Close()
}
