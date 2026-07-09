package sandbox

import (
	"encoding/json"
	"testing"
)

func TestSandboxToStringAllowsShortUser(t *testing.T) {
	sb := &Sandbox{User: "u1"}

	got := sb.ToString()

	var payload struct {
		User string `json:"user"`
	}
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("failed to unmarshal sandbox string: %v", err)
	}
	if payload.User != "u1" {
		t.Fatalf("expected short user to be preserved, got %q", payload.User)
	}
}

func TestSandboxToStringTruncatesLongUser(t *testing.T) {
	sb := &Sandbox{User: "1234567890123456789012345"}

	got := sb.ToString()

	var payload struct {
		User string `json:"user"`
	}
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("failed to unmarshal sandbox string: %v", err)
	}
	if payload.User != "12345678901234567890" {
		t.Fatalf("expected long user to be truncated to 20 characters, got %q", payload.User)
	}
}
