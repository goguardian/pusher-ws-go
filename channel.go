package pusher

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
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
}

// internalChannel represents the Channel interface used internally
type internalChannel interface {
	Channel

	handleEvent(event string, data json.RawMessage)
}

type boundDataChans map[chan json.RawMessage]chan struct{}

type channel struct {
	name        string
	boundEvents map[string]boundDataChans
	// TODO: implement global bindings
	// globalBindings boundDataChans
	client           *Client
	subscribed       bool
	subscribeSuccess chan struct{}
	// channelData is populated for authorized channels (presence and private
	// channels). It's set by sendSubscriptionRequest. The channelData is invalid
	// until subscribed is set to true.
	channelData channelData

	mutex sync.RWMutex
}

type channelData struct {
	Channel     string          `json:"channel"`
	Auth        string          `json:"auth,omitempty"`
	ChannelData json.RawMessage `json:"channel_data,omitempty"`
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

// ErrTimedOut is the error returned when there is a timeout waiting for a subscription
// confirmation from Pusher
var ErrTimedOut = errors.New("timed out")

func (c *channel) sendSubscriptionRequest(data channelData, o *subscribeOptions) error {
	c.mutex.Lock()
	c.subscribeSuccess = make(chan struct{})
	c.channelData = data
	c.mutex.Unlock()

	doneChan := make(chan error)

	go func() {
		var err error

		timer := time.NewTimer(o.successTimeout)
		defer timer.Stop()

		select {
		case <-c.subscribeSuccess:
			err = nil
		case <-timer.C:
			err = ErrTimedOut
		}

		// try to send on the channel, but don't block if nothing is listening, such
		// as when there is an error calling SendEvent
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

	return c.sendSubscriptionRequest(channelData{Channel: c.name}, o)
}

func (c *channel) Unsubscribe() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.subscribed = false
	return c.client.SendEvent(pusherUnsubscribe, channelData{
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

	c.boundEvents[event][boundChan] = make(chan struct{})

	return boundChan
}

func (c *channel) Unbind(event string, chans ...chan json.RawMessage) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if len(chans) == 0 {
		for _, doneChan := range c.boundEvents[event] {
			close(doneChan)
		}
		delete(c.boundEvents, event)
		return
	}

	eventBoundChans := c.boundEvents[event]
	for _, boundChan := range chans {
		doneChan, exists := eventBoundChans[boundChan]
		if !exists {
			continue
		}

		close(doneChan)
		delete(eventBoundChans, boundChan)
	}
}

func (c *channel) handleEvent(event string, data json.RawMessage) {
	if event == pusherInternalSubSucceeded {
		// try to send on the channel, but don't block if nothing is listening
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
	sendDataMessage(c.boundEvents[event], data)
	c.mutex.RUnlock()
}

func sendDataMessage(channels boundDataChans, data json.RawMessage) {
	for boundChan, doneChan := range channels {
		go func(boundChan chan json.RawMessage, data json.RawMessage, doneChan chan struct{}) {
			select {
			case boundChan <- data:
			case <-doneChan:
			}
		}(boundChan, data, doneChan)
	}
}

func (c *channel) Trigger(event string, data interface{}) error {
	return c.client.SendEvent(event, data, c.name)
}

type privateChannel struct {
	*channel
}

// An AuthError is returned when a non-200 status code is returned in a channel
// subscription authentication request.
type AuthError struct {
	Status int
	Body   string
}

func (e AuthError) Error() string {
	return fmt.Sprintf("Auth error: status code %d, response body: %q", e.Status, e.Body)
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
		var body []byte
		var bodyStr string
		body, err = ioutil.ReadAll(res.Body)
		if err != nil {
			bodyStr = fmt.Sprintf("Error reading response body: %s", err)
		} else {
			bodyStr = string(body)
		}

		return AuthError{
			Status: res.StatusCode,
			Body:   bodyStr,
		}
	}

	chanData := channelData{}
	if err = json.NewDecoder(res.Body).Decode(&chanData); err != nil {
		return err
	}
	chanData.Channel = c.name

	return c.sendSubscriptionRequest(chanData, o)
}
