package pusher

import (
	"encoding/json"
	"fmt"
)

type Event struct {
	Event   string          `json:"event"`
	Data    json.RawMessage `json:"data"`
	Channel string          `json:"channel,omitempty"`
}

type EventError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func (e EventError) Error() string {
	return fmt.Sprintf("Pusher error: code %d, message %q", e.Code, e.Message)
}

func extractEventError(event Event) error {
	var eventErr EventError
	err := json.Unmarshal([]byte(event.Data), &eventErr)
	if err != nil {
		return err
	}
	return eventErr
}
