package pusher

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

type Channel interface {
	Subscribe() error
	Unsubscribe() error
	Bind(event string) chan json.RawMessage
	Unbind(event string, chans ...chan json.RawMessage)
	Trigger(event string, data interface{}) error

	handleEvent(event string, data json.RawMessage)
}

type boundDataChans map[chan json.RawMessage]struct{}

type channel struct {
	name        string
	boundEvents map[string]boundDataChans
	// TODO: implement global bindings
	// globalBindings boundDataChans
	client     *Client
	subscribed bool

	mutex sync.RWMutex
}

type subscribeData struct {
	Channel     string `json:"channel"`
	Auth        string `json:"auth,omitempty"`
	ChannelData string `json:"channel_data,omitempty"`
}

func (c *channel) Subscribe() error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if c.subscribed {
		return nil
	}
	return c.client.SendEvent(pusherSubscribe, subscribeData{
		Channel: c.name,
	}, "")
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
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if event == pusherInternalSubSucceeded {
		c.subscribed = true
		event = pusherSubSucceeded
	}

	for boundChan := range c.boundEvents[event] {
		go func(boundChan chan json.RawMessage, data json.RawMessage) {
			boundChan <- data
		}(boundChan, data)
	}
}

func (c *channel) Trigger(event string, data interface{}) error {
	return c.client.SendEvent(event, data, c.name)
}

type privateChannel struct {
	*channel
}

func (c *privateChannel) Subscribe() error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if c.subscribed {
		return nil
	}

	body := url.Values{}
	body.Set("socket_id", c.client.socketID)
	body.Set("channel_name", c.name)
	res, err := http.Post(c.client.AuthURL, "application/x-www-form-urlencoded", strings.NewReader(body.Encode()))
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

	return c.client.SendEvent(pusherSubscribe, chanData, "")
}
