package pusher

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Channel represents a subscription to a Pusher channel.
type Channel interface {
	// IsSubscribed indicates if the channel is currently subscribed
	IsSubscribed() bool
	// Subscribe attempts to subscribe to the channel if the subscription is not
	// already active. Authentication will be attempted for private and presence
	// channels.
	Subscribe(...SubscribeOption) error
	// Unsubscribe attempts to unsubscribe from the channel. Note that a nil error
	// does not mean that the unsubscription was successful, just that the request
	// was sent.
	Unsubscribe() error
	// Bind returns a channel to which all the data from all matching events received
	// on the channel will be sent.
	Bind(event string) chan json.RawMessage
	// Unbind removes bindings for an event. If chans are passed, only those bindings
	// will be removed. Otherwise, all bindings for an event will be removed.
	Unbind(event string, chans ...chan json.RawMessage)
	// Trigger sends an event to the channel.
	Trigger(event string, data interface{}) error

	handleEvent(event string, data json.RawMessage)
}

type boundDataChans map[chan json.RawMessage]struct{}

type channel struct {
	name        string
	boundEvents map[string]boundDataChans
	// TODO: implement global bindings
	// globalBindings boundDataChans
	client           *Client
	subscribed       bool
	subscribeSuccess chan struct{}

	mutex sync.RWMutex
}

type subscribeData struct {
	Channel     string `json:"channel"`
	Auth        string `json:"auth,omitempty"`
	ChannelData string `json:"channel_data,omitempty"`
}

func (c *channel) IsSubscribed() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return c.subscribed
}

type subscribeOptions struct {
	successTimeout time.Duration
}

// SubscribeOption is a configuration option for subscribing to a channel
type SubscribeOption func(*subscribeOptions)

const defaultSuccessTimeout = 10 * time.Second

// WithSuccessTimeout returns a SubscribeOption that sets the time that a subscription
// request will wait for a success response from Pusher before timing out. The
// default is 10 seconds.
func WithSuccessTimeout(d time.Duration) SubscribeOption {
	return func(o *subscribeOptions) {
		o.successTimeout = d
	}
}

// ErrTimedOut is the error returned when there is a timeour waiting for a subscription
// confirmation from Pusher
var ErrTimedOut = errors.New("timed out")

func (c *channel) sendSubscriptionRequest(data subscribeData, o *subscribeOptions) error {
	c.mutex.Lock()
	c.subscribeSuccess = make(chan struct{})
	c.mutex.Unlock()

	doneChan := make(chan error)

	go func() {
		var err error
		select {
		case <-c.subscribeSuccess:
			err = nil
		case <-time.After(o.successTimeout):
			err = ErrTimedOut
		}
		select {
		case doneChan <- err:
		default:
		}
	}()

	err := c.client.SendEvent(pusherSubscribe, data, "")
	if err != nil {
		return fmt.Errorf("error sending subscription request: %s", err)
	}

	return <-doneChan
}

func (c *channel) Subscribe(opts ...SubscribeOption) error {
	if c.IsSubscribed() {
		return nil
	}

	o := &subscribeOptions{
		successTimeout: defaultSuccessTimeout,
	}

	for _, opt := range opts {
		opt(o)
	}

	return c.sendSubscriptionRequest(subscribeData{Channel: c.name}, o)
}

func (c *channel) Unsubscribe() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.subscribed = false
	return c.client.SendEvent(pusherUnsubscribe, subscribeData{
		Channel: c.name,
	}, "")
}

func (c *channel) Bind(event string) chan json.RawMessage {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	boundChan := make(chan json.RawMessage)

	if c.boundEvents[event] == nil {
		c.boundEvents[event] = boundDataChans{}
	}
	c.boundEvents[event][boundChan] = struct{}{}

	return boundChan
}

func (c *channel) Unbind(event string, chans ...chan json.RawMessage) {
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

func (c *channel) handleEvent(event string, data json.RawMessage) {
	if event == pusherInternalSubSucceeded {
		select {
		case c.subscribeSuccess <- struct{}{}:
		default:
		}

		c.mutex.Lock()
		c.subscribed = true
		c.mutex.Unlock()

		event = pusherSubSucceeded
	}

	c.mutex.RLock()
	for boundChan := range c.boundEvents[event] {
		go func(boundChan chan json.RawMessage, data json.RawMessage) {
			boundChan <- data
		}(boundChan, data)
	}
	c.mutex.RUnlock()
}

func (c *channel) Trigger(event string, data interface{}) error {
	return c.client.SendEvent(event, data, c.name)
}

type privateChannel struct {
	*channel
}


func (c *privateChannel) Subscribe(opts ...SubscribeOption) error {
	if c.IsSubscribed() {
		return nil
	}

	o := &subscribeOptions{
		successTimeout: defaultSuccessTimeout,
	}

	for _, opt := range opts {
		opt(o)
	}

	body := url.Values{}
	body.Set("socket_id", c.client.socketID)
	body.Set("channel_name", c.name)
	for key, vals := range c.client.AuthParams {
		for _, val := range vals {
			body.Add(key, val)
		}
	}

	req, err := http.NewRequest(http.MethodPost, c.client.AuthURL, strings.NewReader(body.Encode()))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for key, vals := range c.client.AuthHeaders {
		for _, val := range vals {
			req.Header.Add(key, val)
		}
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		// TODO: add the response body to the error message
		return fmt.Errorf("Got non-200 status code from auth endpoint: %d", res.StatusCode)
	}

	chanData := subscribeData{}
	if err = json.NewDecoder(res.Body).Decode(&chanData); err != nil {
		return err
	}
	chanData.Channel = c.name

	return c.sendSubscriptionRequest(chanData, o)
}
