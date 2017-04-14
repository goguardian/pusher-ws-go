package pusher

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestEventErrorError(t *testing.T) {
	err := EventError{
		Message: "foo",
		Code:    123,
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "code 123") {
		t.Errorf("Expected error message to contain 'code 123', got %s", errMsg)
	}
	if !strings.Contains(errMsg, `message "foo"`) {
		t.Errorf(`Expected error message to contain 'message "foo"', got %s`, errMsg)
	}
}

func TestExtractEventError(t *testing.T) {
	wantMessage := "foo"
	wantCode := 123
	event := Event{
		Data: json.RawMessage(fmt.Sprintf(`{"message":"%s","code":%d}`, wantMessage, wantCode)),
	}
	err := extractEventError(event)
	evtErr := err.(EventError)
	if evtErr.Message != wantMessage {
		t.Errorf("Expected error message to be %s, got %s", wantMessage, evtErr.Message)
	}
	if evtErr.Code != wantCode {
		t.Errorf("Expected error code to be %d, got %d", wantCode, evtErr.Code)
	}
}
