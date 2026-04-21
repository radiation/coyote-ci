package api

import (
	"encoding/json"
	"testing"
)

func TestUpdateSourceCredentialRequestUsernamePatch_Omitted(t *testing.T) {
	var req UpdateSourceCredentialRequest
	if err := json.Unmarshal([]byte(`{"name":"cred"}`), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if req.Username.Set {
		t.Fatal("expected username patch to be unset when omitted")
	}
	if req.Username.Value != nil {
		t.Fatal("expected username value to be nil when omitted")
	}
}

func TestUpdateSourceCredentialRequestUsernamePatch_Null(t *testing.T) {
	var req UpdateSourceCredentialRequest
	if err := json.Unmarshal([]byte(`{"username":null}`), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !req.Username.Set {
		t.Fatal("expected username patch to be set for explicit null")
	}
	if req.Username.Value != nil {
		t.Fatal("expected username value to be nil for explicit null")
	}
}

func TestUpdateSourceCredentialRequestUsernamePatch_String(t *testing.T) {
	var req UpdateSourceCredentialRequest
	if err := json.Unmarshal([]byte(`{"username":"ci-bot"}`), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !req.Username.Set {
		t.Fatal("expected username patch to be set for explicit string")
	}
	if req.Username.Value == nil {
		t.Fatal("expected username value for explicit string")
	}
	if *req.Username.Value != "ci-bot" {
		t.Fatalf("expected username value ci-bot, got %q", *req.Username.Value)
	}
}
