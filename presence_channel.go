package pusher

import (
	"encoding/json"
	"errors"
	"fmt"
)

var (
	// ErrNotSubscribed is returned by functions that require the channel to be
	// subscribed before being called.
	ErrNotSubscribed = errors.New("not subscribed")
	// ErrMissingMe indicates a presence channel was subscribed to, but the pusher
	// server violated the message protocol by not providing a member for the
	// current user.
	ErrMissingMe = errors.New("missing member for current user")
)

// Member represents a channel member.
type Member struct {
	ID string
	// Info is the JSON set by the auth server. Do not modify the underlying byte
	// slice, it's shared by all instances of the member.
	Info json.RawMessage
}

// PresenceChannel provides information about the users that are currently
// subscribed.
//
// Note: Bind() does not fire pusher:member_added/removed, use
// BindMemberAdded/Removed() instead.
type PresenceChannel interface {
	Channel

	// BindMemberAdded returns a channel that receives a Member value when a user
	// joins the channel. Events may be delivered out of order. Use
	// UnbindMemberAdded when finished listening to events.
	BindMemberAdded() chan Member

	// UnbindMemberAdded removes bindings created by BindMemberAdded(). If chans
	// are passed, only those bindings will be removed. Otherwise, all bindings
	// for this event will be removed.
	UnbindMemberAdded(...chan Member)

	// BindMemberRemoved returns a channel that receives a user ID when a user
	// leaves the channel. Events may be delivered out of order. Use
	// UnbindMemberRemoved when finished listening for events.
	BindMemberRemoved() chan string

	// UnbindMemberRemoved removes bindings created by UnbindMemberRemoved(). If
	// chans are passed, only those bindings will be removed. Otherwise, all
	// bindings for this event will be removed.
	UnbindMemberRemoved(...chan string)

	// Members returns the member info of each user currently subscribed to the
	// channel.
	Members() map[string]Member

	// Member returns a representation of the channel member with the given ID.
	// `nil` is returned if the member isn't in the channel.
	Member(id string) *Member

	// Me returns the member for the current user.
	//
	// Possible errors:
	//  - not subscribed - subscription must succeed before calling Me()
	//  - invalid channel data - pusher server provided invalid JSON
	//  - missing member for current user - pusher server violated protocol
	Me() (*Member, error)

	// MemberCount returns the number of users connected to the channel.
	MemberCount() int
}

// presenceChannel implements the internalChannel and PresenceChannel interfaces
type presenceChannel struct {
	*privateChannel

	memberAddedChans   map[chan Member]chanContext
	memberRemovedChans map[chan string]chanContext
	members            map[string]Member
}

func newPresenceChannel(baseChannel *channel) *presenceChannel {
	privateChannel := &privateChannel{channel: baseChannel}
	return &presenceChannel{
		privateChannel:     privateChannel,
		memberAddedChans:   map[chan Member]chanContext{},
		memberRemovedChans: make(map[chan string]chanContext),
		members:            map[string]Member{},
	}
}

// presenceChannelSubscriptionData is sent from Pusher to the client in the
// subscription_succeeded event.
// https://pusher.com/docs/channels/library_auth_reference/pusher-websockets-protocol/#pusher_internalsubscription_succeeded-pusher-channels-greater-client
type presenceChannelSubscriptionData struct {
	Presence struct {
		IDs   []string                   `json:"ids"`
		Hash  map[string]json.RawMessage `json:"hash"`
		Count int                        `json:"count"`
	} `json:"presence"`
}

type presenceChannelData struct {
	UserID string `json:"user_id"`
}

type presenceChannelMemberAddedData struct {
	UserID   string          `json:"user_id"`
	UserInfo json.RawMessage `json:"user_info"`
}

type presenceChannelMemberRemovedData struct {
	UserID string `json:"user_id"`
}

func (c *presenceChannel) handleEvent(event string, data json.RawMessage) {
	if event == pusherInternalMemberAdded {
		var member presenceChannelMemberAddedData
		err := UnmarshalDataString(data, &member)
		if err != nil {
			c.client.sendError(fmt.Errorf("decoding member added event data: %w", err))
			return
		}

		c.mutex.Lock()
		c.members[member.UserID] = Member{
			ID:   member.UserID,
			Info: member.UserInfo,
		}

		sendMemberAdded(c.memberAddedChans, c.members[member.UserID])
		c.mutex.Unlock()

		return
	}

	if event == pusherInternalMemberRemoved {
		var member presenceChannelMemberRemovedData
		err := UnmarshalDataString(data, &member)
		if err != nil {
			c.client.sendError(fmt.Errorf("decoding member removed event data: %w", err))
			return
		}

		c.mutex.Lock()
		delete(c.members, member.UserID)

		sendMemberRemoved(c.memberRemovedChans, member.UserID)
		c.mutex.Unlock()

		return
	}

	if event == pusherInternalSubSucceeded {
		var subscriptionData presenceChannelSubscriptionData
		err := UnmarshalDataString(data, &subscriptionData)
		if err != nil {
			c.client.sendError(fmt.Errorf("decoding sub succeeded event data: %w", err))
		}

		members := make(map[string]Member, len(subscriptionData.Presence.Hash))
		for id, info := range subscriptionData.Presence.Hash {
			member := Member{
				ID:   id,
				Info: info,
			}
			members[id] = member
		}

		c.mutex.Lock()
		c.members = members
		c.subscribed = true

		select {
		case c.subscribeSuccess <- struct{}{}:
		default:
		}

		for _, member := range c.members {
			sendMemberAdded(c.memberAddedChans, member)
		}
		c.mutex.Unlock()

		event = pusherSubSucceeded
	}

	c.mutex.RLock()
	sendDataMessage(c.boundEvents[event], data)
	c.mutex.RUnlock()
}

func sendMemberAdded(channels map[chan Member]chanContext, member Member) {
	for ch, chanCtx := range channels {
		go func(ch chan Member, member Member, chanCtx chanContext) {
			select {
			case ch <- member:
			case <-chanCtx.ctx.Done():
			}
		}(ch, member, chanCtx)
	}
}

func sendMemberRemoved(channels map[chan string]chanContext, id string) {
	for ch, chanCtx := range channels {
		go func(ch chan string, id string, chanCtx chanContext) {
			select {
			case ch <- id:
			case <-chanCtx.ctx.Done():
			}
		}(ch, id, chanCtx)
	}
}

func (c *presenceChannel) BindMemberAdded() chan Member {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	ch := make(chan Member)
	c.memberAddedChans[ch] = newChanContext()

	return ch
}

func (c *presenceChannel) UnbindMemberAdded(chans ...chan Member) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Remove all channels when no channels were specified
	if len(chans) == 0 {
		for _, chanCtx := range c.memberAddedChans {
			chanCtx.cancel()
		}
		c.memberAddedChans = map[chan Member]chanContext{}
		return
	}

	// Remove given channels
	for _, ch := range chans {
		chanCtx, exists := c.memberAddedChans[ch]
		if !exists {
			continue
		}

		chanCtx.cancel()
		delete(c.memberAddedChans, ch)
	}
}

func (c *presenceChannel) BindMemberRemoved() chan string {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	ch := make(chan string)
	c.memberRemovedChans[ch] = newChanContext()

	return ch
}

func (c *presenceChannel) UnbindMemberRemoved(chans ...chan string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Remove all channels when no channels were specified
	if len(chans) == 0 {
		for _, chanCtx := range c.memberRemovedChans {
			chanCtx.cancel()
		}
		c.memberRemovedChans = map[chan string]chanContext{}
		return
	}

	// Remove given channels
	for _, ch := range chans {
		chanCtx, exists := c.memberRemovedChans[ch]
		if !exists {
			continue
		}

		chanCtx.cancel()
		delete(c.memberRemovedChans, ch)
	}
}

func (c *presenceChannel) Members() map[string]Member {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	// Maps are passed by reference, so a copy must be made to avoid giving the
	// caller a reference to the internal map.
	members := make(map[string]Member, len(c.members))
	for id, member := range c.members {
		members[id] = member
	}

	return members
}

func (c *presenceChannel) Member(id string) *Member {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	member, ok := c.members[id]
	if ok {
		return &member
	}

	return nil
}

func (c *presenceChannel) Me() (*Member, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if !c.subscribed {
		return nil, ErrNotSubscribed
	}

	var data presenceChannelData
	err := UnmarshalDataString(c.channelData.ChannelData, &data)
	if err != nil {
		return nil, fmt.Errorf("invalid channel data: %w", err)
	}

	member, ok := c.members[data.UserID]
	if !ok {
		return nil, ErrMissingMe
	}

	return &member, nil
}

func (c *presenceChannel) MemberCount() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return len(c.members)
}
