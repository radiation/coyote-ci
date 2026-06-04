package email

import (
	"context"
	"errors"
	"net/smtp"
	"strings"
	"testing"
)

func TestNewSender_DisabledReturnsNoop(t *testing.T) {
	sender, err := NewSender(Config{Enabled: false})
	if err != nil {
		t.Fatalf("expected disabled sender without error, got %v", err)
	}

	if sendErr := sender.SendText(context.Background(), Message{}); sendErr != nil {
		t.Fatalf("expected disabled sender to no-op, got %v", sendErr)
	}
}

func TestNewSender_ValidatesConfig(t *testing.T) {
	tests := []struct {
		name  string
		cfg   Config
		match string
	}{
		{
			name:  "requires host",
			cfg:   Config{Enabled: true, Port: "1025", FromAddress: "coyote-ci@localhost"},
			match: "smtp host is required",
		},
		{
			name:  "requires port",
			cfg:   Config{Enabled: true, Host: "mailpit", FromAddress: "coyote-ci@localhost"},
			match: "smtp port is required",
		},
		{
			name:  "requires from address",
			cfg:   Config{Enabled: true, Host: "mailpit", Port: "1025"},
			match: "smtp from address is required",
		},
		{
			name:  "rejects invalid from address",
			cfg:   Config{Enabled: true, Host: "mailpit", Port: "1025", FromAddress: "not-an-email"},
			match: "invalid smtp from address",
		},
		{
			name:  "rejects partial auth",
			cfg:   Config{Enabled: true, Host: "mailpit", Port: "1025", FromAddress: "coyote-ci@localhost", Username: "mailer"},
			match: "smtp username and password must both be set or both be empty",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewSender(tc.cfg)
			if err == nil || !strings.Contains(err.Error(), tc.match) {
				t.Fatalf("expected error containing %q, got %v", tc.match, err)
			}
		})
	}
}

func TestSMTPSender_SendText(t *testing.T) {
	t.Parallel()

	type sendCall struct {
		addr string
		from string
		to   []string
		msg  string
	}

	var got sendCall
	sender, err := newSMTPSender(Config{
		Enabled:     true,
		Host:        "mailpit",
		Port:        "1025",
		FromAddress: "Coyote CI <coyote-ci@localhost>",
	}, func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		_ = auth
		got = sendCall{addr: addr, from: from, to: to, msg: string(msg)}
		return nil
	})
	if err != nil {
		t.Fatalf("expected sender, got %v", err)
	}

	sendErr := sender.SendText(context.Background(), Message{
		To:      "Dev User <dev@example.com>",
		Subject: "Build queued\nInjected",
		Body:    "line 1\nline 2",
	})
	if sendErr != nil {
		t.Fatalf("expected send to succeed, got %v", sendErr)
	}

	if got.addr != "mailpit:1025" {
		t.Fatalf("expected smtp addr mailpit:1025, got %q", got.addr)
	}
	if got.from != "coyote-ci@localhost" {
		t.Fatalf("expected envelope from coyote-ci@localhost, got %q", got.from)
	}
	if len(got.to) != 1 || got.to[0] != "dev@example.com" {
		t.Fatalf("expected recipient dev@example.com, got %#v", got.to)
	}
	if !strings.Contains(got.msg, "From: \"Coyote CI\" <coyote-ci@localhost>\r\n") {
		t.Fatalf("expected From header, got %q", got.msg)
	}
	if !strings.Contains(got.msg, "To: \"Dev User\" <dev@example.com>\r\n") {
		t.Fatalf("expected To header, got %q", got.msg)
	}
	if !strings.Contains(got.msg, "Subject: Build queued Injected\r\n") {
		t.Fatalf("expected sanitized Subject header, got %q", got.msg)
	}
	if !strings.Contains(got.msg, "\r\n\r\nline 1\r\nline 2") {
		t.Fatalf("expected normalized body, got %q", got.msg)
	}
}

func TestSMTPSender_SendTextValidatesMessageAndContext(t *testing.T) {
	t.Parallel()

	called := false
	sender, err := newSMTPSender(Config{
		Enabled:     true,
		Host:        "mailpit",
		Port:        "1025",
		FromAddress: "coyote-ci@localhost",
	}, func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		called = true
		_ = addr
		_ = auth
		_ = from
		_ = to
		_ = msg
		return nil
	})
	if err != nil {
		t.Fatalf("expected sender, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sendErr := sender.SendText(ctx, Message{To: "dev@example.com", Subject: "Build queued", Body: "test"}); !errors.Is(sendErr, context.Canceled) {
		t.Fatalf("expected canceled context error, got %v", sendErr)
	}
	if called {
		t.Fatalf("expected canceled context to skip send")
	}

	if sendErr := sender.SendText(context.Background(), Message{Subject: "Build queued", Body: "test"}); sendErr == nil || !strings.Contains(sendErr.Error(), "email to address is required") {
		t.Fatalf("expected missing recipient error, got %v", sendErr)
	}
	if sendErr := sender.SendText(context.Background(), Message{To: "dev@example.com", Body: "test"}); sendErr == nil || !strings.Contains(sendErr.Error(), "email subject is required") {
		t.Fatalf("expected missing subject error, got %v", sendErr)
	}
	if sendErr := sender.SendText(context.Background(), Message{To: "dev@example.com", Subject: "Build queued"}); sendErr == nil || !strings.Contains(sendErr.Error(), "email body is required") {
		t.Fatalf("expected missing body error, got %v", sendErr)
	}
	if sendErr := sender.SendText(context.Background(), Message{To: "not-an-email", Subject: "Build queued", Body: "test"}); sendErr == nil || !strings.Contains(sendErr.Error(), "invalid email to address") {
		t.Fatalf("expected invalid recipient error, got %v", sendErr)
	}
}

func TestSMTPSender_SendTextWrapsSendError(t *testing.T) {
	t.Parallel()

	sender, err := newSMTPSender(Config{
		Enabled:     true,
		Host:        "mailpit",
		Port:        "1025",
		FromAddress: "coyote-ci@localhost",
	}, func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		_ = addr
		_ = auth
		_ = from
		_ = to
		_ = msg
		return errors.New("boom")
	})
	if err != nil {
		t.Fatalf("expected sender, got %v", err)
	}

	sendErr := sender.SendText(context.Background(), Message{To: "dev@example.com", Subject: "Build queued", Body: "test"})
	if sendErr == nil || !strings.Contains(sendErr.Error(), "send email: boom") {
		t.Fatalf("expected wrapped send error, got %v", sendErr)
	}
}
