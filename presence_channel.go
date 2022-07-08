package pusher

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
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

	membersMutex       sync.RWMutex
	memberAddedChans   map[chan Member]chan struct{}
	memberRemovedChans map[chan string]chan struct{}
	members            map[string]Member
}

func newPresenceChannel(baseChannel *channel) *presenceChannel {
	privateChannel := &privateChannel{channel: baseChannel}
	return &presenceChannel{
		privateChannel:     privateChannel,
		memberAddedChans:   map[chan Member]chan struct{}{},
		memberRemovedChans: map[chan string]chan struct{}{},
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

func (pc *presenceChannel) handleEvent(event string, data json.RawMessage) {
	switch event {
	case pusherInternalMemberAdded:
		var member presenceChannelMemberAddedData
		err := UnmarshalDataString(data, &member)
		if err != nil {
			pc.privateChannel.channel.client.sendError(fmt.Errorf("decoding member added event data: %w", err))
			return
		}

		pc.membersMutex.Lock()
		pc.members[member.UserID] = Member{
			ID:   member.UserID,
			Info: member.UserInfo,
		}

		sendMemberAdded(pc.memberAddedChans, pc.members[member.UserID])
		pc.membersMutex.Unlock()

	case pusherInternalMemberRemoved:
		var member presenceChannelMemberRemovedData
		err := UnmarshalDataString(data, &member)
		if err != nil {
			pc.privateChannel.channel.client.sendError(fmt.Errorf("decoding member removed event data: %w", err))
			return
		}

		pc.membersMutex.Lock()
		delete(pc.members, member.UserID)

		sendMemberRemoved(pc.memberRemovedChans, member.UserID)
		pc.membersMutex.Unlock()

	case pusherInternalSubSucceeded:
		var subscriptionData presenceChannelSubscriptionData
		err := UnmarshalDataString(data, &subscriptionData)
		if err != nil {
			pc.privateChannel.channel.client.sendError(fmt.Errorf("decoding sub succeeded event data: %w", err))
		}

		members := make(map[string]Member, len(subscriptionData.Presence.Hash))
		for id, info := range subscriptionData.Presence.Hash {
			member := Member{
				ID:   id,
				Info: info,
			}
			members[id] = member
		}

		pc.membersMutex.Lock()
		pc.members = members

		pc.privateChannel.channel.handleEvent(event, data)

		for _, member := range pc.members {
			sendMemberAdded(pc.memberAddedChans, member)
		}
		pc.membersMutex.Unlock()

	default:
		pc.privateChannel.channel.handleEvent(event, data)
	}
}

func sendMemberAdded(channels map[chan Member]chan struct{}, member Member) {
	for ch, doneChan := range channels {
		go func(ch chan Member, member Member, doneChan chan struct{}) {
			select {
			case ch <- member:
			case <-doneChan:
			}
		}(ch, member, doneChan)
	}
}

func sendMemberRemoved(channels map[chan string]chan struct{}, id string) {
	for ch, doneChan := range channels {
		go func(ch chan string, id string, doneChan chan struct{}) {
			select {
			case ch <- id:
			case <-doneChan:
			}
		}(ch, id, doneChan)
	}
}

func (pc *presenceChannel) BindMemberAdded() chan Member {
	pc.membersMutex.Lock()
	defer pc.membersMutex.Unlock()

	ch := make(chan Member)
	pc.memberAddedChans[ch] = make(chan struct{})

	return ch
}

func (pc *presenceChannel) UnbindMemberAdded(chans ...chan Member) {
	pc.membersMutex.Lock()
	defer pc.membersMutex.Unlock()

	// Remove all channels when no channels were specified
	if len(chans) == 0 {
		for _, doneChan := range pc.memberAddedChans {
			close(doneChan)
		}
		pc.memberAddedChans = map[chan Member]chan struct{}{}
		return
	}

	// Remove given channels
	for _, ch := range chans {
		doneChan, exists := pc.memberAddedChans[ch]
		if !exists {
			continue
		}

		close(doneChan)
		delete(pc.memberAddedChans, ch)
	}
}

func (pc *presenceChannel) BindMemberRemoved() chan string {
	pc.membersMutex.Lock()
	defer pc.membersMutex.Unlock()

	ch := make(chan string)
	pc.memberRemovedChans[ch] = make(chan struct{})

	return ch
}

func (pc *presenceChannel) UnbindMemberRemoved(chans ...chan string) {
	pc.membersMutex.Lock()
	defer pc.membersMutex.Unlock()

	// Remove all channels when no channels were specified
	if len(chans) == 0 {
		for _, doneChan := range pc.memberRemovedChans {
			close(doneChan)
		}
		pc.memberRemovedChans = map[chan string]chan struct{}{}
		return
	}

	// Remove given channels
	for _, ch := range chans {
		doneChan, exists := pc.memberRemovedChans[ch]
		if !exists {
			continue
		}

		close(doneChan)
		delete(pc.memberRemovedChans, ch)
	}
}

func (pc *presenceChannel) Members() map[string]Member {
	pc.membersMutex.RLock()
	defer pc.membersMutex.RUnlock()

	// Maps are passed by reference, so a copy must be made to avoid giving the
	// caller a reference to the internal map.
	members := make(map[string]Member, len(pc.members))
	for id, member := range pc.members {
		members[id] = member
	}

	return members
}

func (pc *presenceChannel) Member(id string) *Member {
	pc.membersMutex.RLock()
	defer pc.membersMutex.RUnlock()

	member, ok := pc.members[id]
	if ok {
		return &member
	}

	return nil
}

func (pc *presenceChannel) Me() (*Member, error) {
	if !pc.privateChannel.channel.IsSubscribed() {
		return nil, ErrNotSubscribed
	}

	pc.membersMutex.RLock()
	defer pc.membersMutex.RUnlock()

	var data presenceChannelData
	err := UnmarshalDataString(pc.channelData.ChannelData, &data)
	if err != nil {
		return nil, fmt.Errorf("invalid channel data: %w", err)
	}

	member, ok := pc.members[data.UserID]
	if !ok {
		return nil, ErrMissingMe
	}

	return &member, nil
}

func (pc *presenceChannel) MemberCount() int {
	pc.membersMutex.RLock()
	defer pc.membersMutex.RUnlock()

	return len(pc.members)
}
