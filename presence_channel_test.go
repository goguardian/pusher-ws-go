package pusher

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestPresenceHandleEvent(t *testing.T) {
	t.Run("subscribeSuccess", func(t *testing.T) {
		ch := newPresenceChannel(&channel{})

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

		ch.handleEvent(pusherInternalSubSucceeded, json.RawMessage(data))

		if ch.subscribed != true {
			t.Fatal("Expected channel subscribed to be true, got false")
		}

		members := ch.Members()
		expectMembers := map[string]Member{
			"1": {"1", json.RawMessage(`{ "name": "name-1" }`)},
			"2": {"2", json.RawMessage(`{ "name": "name-2" }`)},
		}
		if !reflect.DeepEqual(members, expectMembers) {
			t.Errorf("Expected %+v, got %+v", expectMembers, members)
		}
	})

	t.Run("memberAdded", func(t *testing.T) {
		ch := newPresenceChannel(&channel{})
		ch.members = map[string]Member{"1": {"1", json.RawMessage(`{ "name": "name-1" }`)}}

		data, err := json.Marshal(`
			{
				"user_id": "2",
				"user_info": { "name": "name-2" }
			}
		`)
		if err != nil {
			t.Fatal("error marshaling data: ", err)
		}

		ch.handleEvent(pusherInternalMemberAdded, json.RawMessage(data))

		members := ch.Members()
		expectMembers := map[string]Member{
			"1": {"1", json.RawMessage(`{ "name": "name-1" }`)},
			"2": {"2", json.RawMessage(`{ "name": "name-2" }`)},
		}
		if !reflect.DeepEqual(members, expectMembers) {
			t.Errorf("Expected %+v, got %+v", expectMembers, members)
		}
	})

	t.Run("memberRemoved", func(t *testing.T) {
		ch := newPresenceChannel(&channel{})
		ch.members = map[string]Member{
			"1": {"1", json.RawMessage(`{ "name": "name-1" }`)},
			"2": {"2", json.RawMessage(`{ "name": "name-2" }`)},
		}

		data, err := json.Marshal(`{"user_id": "2"}`)
		if err != nil {
			t.Fatal("error marshaling data: ", err)
		}

		ch.handleEvent(pusherInternalMemberRemoved, json.RawMessage(data))

		members := ch.Members()
		expectMembers := map[string]Member{
			"1": {"1", json.RawMessage(`{ "name": "name-1" }`)},
		}
		if !reflect.DeepEqual(members, expectMembers) {
			t.Errorf("Expected %+v, got %+v", expectMembers, members)
		}
	})

	t.Run("boundEvent", func(t *testing.T) {
		ch := newPresenceChannel(&channel{})

		expectedData := json.RawMessage(`{"hello":"world"}`)
		event := "foo"

		dataChan := make(chan json.RawMessage)
		ch.boundEvents = map[string]boundDataChans{
			event: {dataChan: make(chan struct{})},
		}

		ch.handleEvent(event, expectedData)

		if data := <-dataChan; !reflect.DeepEqual(data, expectedData) {
			t.Errorf("Expected %+v, got %+v", expectedData, data)
		}
	})
}

func TestPresenceMembers(t *testing.T) {
	t.Run("Member()", func(t *testing.T) {
		ch := newPresenceChannel(&channel{})
		ch.members = map[string]Member{
			"1": {"1", json.RawMessage(`{ "name": "name-1" }`)},
			"2": {"2", json.RawMessage(`{ "name": "name-2" }`)},
		}

		member1 := ch.Member("1")
		expectedMember1 := &Member{"1", json.RawMessage(`{ "name": "name-1" }`)}
		if member1 == nil || !reflect.DeepEqual(member1, expectedMember1) {
			t.Errorf("Expected %+v, got %+v", expectedMember1, member1)
		}

		member2 := ch.Member("2")
		expectedMember2 := &Member{"2", json.RawMessage(`{ "name": "name-2" }`)}
		if member2 == nil || !reflect.DeepEqual(member2, expectedMember2) {
			t.Errorf("Expected %+v, got %+v", expectedMember2, member2)
		}

		member3 := ch.Member("3")
		if member3 != nil {
			t.Errorf("Expected nil, got %+v", member3)
		}
	})

	t.Run("Me()", func(t *testing.T) {
		ch := newPresenceChannel(&channel{})
		ch.members = map[string]Member{
			"1": {"1", json.RawMessage(`{ "name": "name-1" }`)},
			"2": {"2", json.RawMessage(`{ "name": "name-2" }`)},
		}
		ch.subscribed = true
		ch.channelData = channelData{
			ChannelData: json.RawMessage(`"{\"user_id\":\"1\"}"`),
		}

		me, err := ch.Me()
		if err != nil {
			t.Fatal("Expected no error, got ", err)
		}

		expectedMe := &Member{"1", json.RawMessage(`{ "name": "name-1" }`)}
		if !reflect.DeepEqual(me, expectedMe) {
			t.Errorf("Expected %+v, got %+v", expectedMe, me)
		}
	})

	t.Run("MemberCount()", func(t *testing.T) {
		ch := newPresenceChannel(&channel{})
		ch.members = map[string]Member{
			"1": {"1", json.RawMessage(`{ "name": "name-1" }`)},
			"2": {"2", json.RawMessage(`{ "name": "name-2" }`)},
		}

		count := ch.MemberCount()
		if count != 2 {
			t.Errorf("Expected %d, got %d", 2, count)
		}
	})

	t.Run("BindMemberAdded()", func(t *testing.T) {
		ch := newPresenceChannel(&channel{})
		memberAddedChan := ch.BindMemberAdded()

		data, err := json.Marshal(`
			{
				"user_id": "1",
				"user_info": { "name": "name-1" }
			}
		`)
		if err != nil {
			t.Fatal("error marshaling data: ", err)
		}

		ch.handleEvent(pusherInternalMemberAdded, json.RawMessage(data))

		select {
		case member := <-memberAddedChan:
			expectedMember := Member{"1", json.RawMessage(`{ "name": "name-1" }`)}
			if !reflect.DeepEqual(member, expectedMember) {
				t.Errorf("Expected first member to equal %+v, got %+v", expectedMember, member)
			}
		case <-time.After(time.Second):
			t.Error("Not enough member added events")
		}

		data, err = json.Marshal(`
			{
				"user_id": "2",
				"user_info": { "name": "name-2" }
			}
		`)
		if err != nil {
			t.Fatal("error marshaling data: ", err)
		}

		ch.handleEvent(pusherInternalMemberAdded, json.RawMessage(data))

		select {
		case member := <-memberAddedChan:
			expectedMember := Member{"2", json.RawMessage(`{ "name": "name-2" }`)}
			if !reflect.DeepEqual(member, expectedMember) {
				t.Errorf("Expected second member to equal %+v, got %+v", expectedMember, member)
			}
		case <-time.After(time.Second):
			t.Error("Not enough member added events")
		}
	})

	t.Run("UnbindMemberAdded()", func(t *testing.T) {
		chan1 := make(chan Member)
		chan2 := make(chan Member)
		chan3 := make(chan Member)

		ch := newPresenceChannel(&channel{})
		ch.memberAddedChans = map[chan Member]chan struct{}{
			chan1: make(chan struct{}),
			chan2: make(chan struct{}),
			chan3: make(chan struct{}),
		}

		ch.UnbindMemberAdded(chan1, chan2)

		if _, exists := ch.memberAddedChans[chan1]; exists {
			t.Errorf("Expected chan1 to be removed, but it still exists")
		}

		if _, exists := ch.memberAddedChans[chan2]; exists {
			t.Errorf("Expected chan2 to be removed, but it still exists")
		}

		if _, exists := ch.memberAddedChans[chan3]; !exists {
			t.Errorf("Expected chan3 to still exist, but it was removed")
		}

		ch.UnbindMemberAdded()

		if _, exists := ch.memberAddedChans[chan3]; exists {
			t.Errorf("Expected chan3 to be removed, but it still exists")
		}
	})

	t.Run("BindMemberRemoved()", func(t *testing.T) {
		ch := newPresenceChannel(&channel{})
		memberRemovedChan := ch.BindMemberRemoved()

		data := `"{\"user_id\": \"1\"}"`
		ch.handleEvent(pusherInternalMemberRemoved, json.RawMessage(data))

		select {
		case id := <-memberRemovedChan:
			expectedID := "1"
			if !reflect.DeepEqual(id, expectedID) {
				t.Errorf("Expected first id to equal %+v, got %+v", expectedID, id)
			}
		case <-time.After(time.Second):
			t.Error("Not enough id removed events")
		}

		data = `"{\"user_id\": \"2\"}"`
		ch.handleEvent(pusherInternalMemberRemoved, json.RawMessage(data))

		select {
		case id := <-memberRemovedChan:
			expectedID := "2"
			if !reflect.DeepEqual(id, expectedID) {
				t.Errorf("Expected second id to equal %+v, got %+v", expectedID, id)
			}
		case <-time.After(time.Second):
			t.Error("Not enough id removed events")
		}
	})

	t.Run("UnbindMemberRemoved()", func(t *testing.T) {
		chan1 := make(chan string)
		chan2 := make(chan string)
		chan3 := make(chan string)

		ch := newPresenceChannel(&channel{})
		ch.memberRemovedChans = map[chan string]chan struct{}{
			chan1: make(chan struct{}),
			chan2: make(chan struct{}),
			chan3: make(chan struct{}),
		}

		ch.UnbindMemberRemoved(chan1, chan2)

		if _, exists := ch.memberRemovedChans[chan1]; exists {
			t.Errorf("Expected chan1 to be removed, but it still exists")
		}

		if _, exists := ch.memberRemovedChans[chan2]; exists {
			t.Errorf("Expected chan2 to be removed, but it still exists")
		}

		if _, exists := ch.memberRemovedChans[chan3]; !exists {
			t.Errorf("Expected chan3 to still exist, but it was removed")
		}

		ch.UnbindMemberRemoved()

		if _, exists := ch.memberRemovedChans[chan3]; exists {
			t.Errorf("Expected chan3 to be removed, but it still exists")
		}
	})
}
