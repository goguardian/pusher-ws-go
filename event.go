package pusher

import (
	"encoding/json"
	"fmt"
)

// Event represents an event sent to or received from a Pusher connection.
type Event struct {
	Event   string          `json:"event"`
	Data    json.RawMessage `json:"data"`
	Channel string          `json:"channel,omitempty"`
}

// EventError represents an error event received from Pusher.
type EventError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func (e EventError) Error() string {
	return fmt.Sprintf("Pusher error: code %d, message %q", e.Code, e.Message)
}

func extractEventError(event Event) error {
	var eventErr EventError
	err := json.Unmarshal(event.Data, &eventErr)
	if err != nil {
		return err
	}
	return eventErr
}
