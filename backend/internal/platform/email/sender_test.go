package email

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"
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
	}, func(ctx context.Context, addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		_ = ctx
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
	}, func(ctx context.Context, addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		_ = ctx
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

	if sendErr := sender.SendText(context.Background(), Message{Subject: "Build queued", Body: "test"}); sendErr == nil || !errors.Is(sendErr, ErrInvalidMessage) || !strings.Contains(sendErr.Error(), "to address is required") {
		t.Fatalf("expected missing recipient error, got %v", sendErr)
	}
	if sendErr := sender.SendText(context.Background(), Message{To: "dev@example.com", Body: "test"}); sendErr == nil || !errors.Is(sendErr, ErrInvalidMessage) || !strings.Contains(sendErr.Error(), "subject is required") {
		t.Fatalf("expected missing subject error, got %v", sendErr)
	}
	if sendErr := sender.SendText(context.Background(), Message{To: "dev@example.com", Subject: "Build queued"}); sendErr == nil || !errors.Is(sendErr, ErrInvalidMessage) || !strings.Contains(sendErr.Error(), "body is required") {
		t.Fatalf("expected missing body error, got %v", sendErr)
	}
	if sendErr := sender.SendText(context.Background(), Message{To: "not-an-email", Subject: "Build queued", Body: "test"}); sendErr == nil || !errors.Is(sendErr, ErrInvalidMessage) || !strings.Contains(sendErr.Error(), "invalid email to address") {
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
	}, func(ctx context.Context, addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		_ = ctx
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

func TestNewSender_UsesBoundedSMTPTimeout(t *testing.T) {
	t.Parallel()

	const timeout = 25 * time.Millisecond

	sender, err := newSMTPSenderWithTimeout(Config{
		Enabled:     true,
		Host:        "mailpit",
		Port:        "1025",
		FromAddress: "coyote-ci@localhost",
	}, timeout, smtpSendMailWithTimeout(timeout))
	if err != nil {
		t.Fatalf("expected sender, got %v", err)
	}

	smtpSender, ok := sender.(*smtpSender)
	if !ok {
		t.Fatalf("expected smtp sender, got %T", sender)
	}

	started := make(chan struct{}, 1)
	finished := make(chan struct{}, 1)
	smtpSender.sendMail = func(ctx context.Context, addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		_ = addr
		_ = auth
		_ = from
		_ = to
		_ = msg
		started <- struct{}{}
		<-ctx.Done()
		finished <- struct{}{}
		return ctx.Err()
	}

	start := time.Now()
	err = smtpSender.SendText(context.Background(), Message{To: "dev@example.com", Subject: "Build queued", Body: "test"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > timeout+250*time.Millisecond {
		t.Fatalf("expected send to stop near the smtp timeout, took %s", elapsed)
	}
	select {
	case <-started:
	default:
		t.Fatal("expected wrapped send function to be invoked")
	}
	select {
	case <-finished:
	default:
		t.Fatal("expected wrapped send function context to be canceled by timeout")
	}
}

func TestSMTPSendMail_UsesOneAbsoluteDeadlineForDialAndIO(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	conn := &recordingConn{}
	client := &fakeSMTPClient{writer: nopWriteCloser{Writer: io.Discard}}
	var dialDeadline time.Time

	sendMail := smtpSendMail(10*time.Second,
		func(ctx context.Context, network, addr string) (net.Conn, error) {
			_ = network
			_ = addr
			var ok bool
			dialDeadline, ok = ctx.Deadline()
			if !ok {
				t.Fatal("expected dial context deadline")
			}
			return conn, nil
		},
		func(gotConn net.Conn, host string) (smtpClient, error) {
			if gotConn != conn {
				t.Fatalf("expected smtp client to receive dialed connection, got %T", gotConn)
			}
			if host != "smtp.example.com" {
				t.Fatalf("expected smtp host smtp.example.com, got %q", host)
			}
			return client, nil
		},
		func() time.Time { return base },
	)

	err := sendMail(context.Background(), "smtp.example.com:25", nil, "from@example.com", []string{"to@example.com"}, []byte("hello"))
	if err != nil {
		t.Fatalf("expected send to succeed, got %v", err)
	}

	wantDeadline := base.Add(10 * time.Second)
	if !dialDeadline.Equal(wantDeadline) {
		t.Fatalf("expected dial deadline %s, got %s", wantDeadline, dialDeadline)
	}
	if conn.setDeadlineCalls != 1 {
		t.Fatalf("expected one SetDeadline call, got %d", conn.setDeadlineCalls)
	}
	if !conn.deadline.Equal(wantDeadline) {
		t.Fatalf("expected connection deadline %s, got %s", wantDeadline, conn.deadline)
	}
	if !client.quitCalled {
		t.Fatal("expected smtp client quit to be called")
	}
	if string(client.wrote) != "hello" {
		t.Fatalf("expected message payload to be written, got %q", string(client.wrote))
	}
	if !conn.closed {
		t.Fatal("expected dialed connection to be closed")
	}

	// The absolute deadline is computed before dialing and then reused for I/O,
	// so any time spent establishing the connection reduces the remaining budget.
	if !conn.deadline.Equal(dialDeadline) {
		t.Fatal("expected SMTP I/O deadline to match the original dial deadline")
	}
}

func TestSMTPSendMail_EarlierParentDeadlineWins(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	parentDeadline := base.Add(3 * time.Second)
	parentCtx, cancel := context.WithDeadline(context.Background(), parentDeadline)
	defer cancel()

	conn := &recordingConn{}
	var dialDeadline time.Time
	sendMail := smtpSendMail(10*time.Second,
		func(ctx context.Context, network, addr string) (net.Conn, error) {
			_ = network
			_ = addr
			var ok bool
			dialDeadline, ok = ctx.Deadline()
			if !ok {
				t.Fatal("expected parent deadline to be preserved")
			}
			return conn, nil
		},
		func(conn net.Conn, host string) (smtpClient, error) {
			_ = conn
			_ = host
			return &fakeSMTPClient{writer: nopWriteCloser{Writer: io.Discard}}, nil
		},
		func() time.Time { return base },
	)

	err := sendMail(parentCtx, "smtp.example.com:25", nil, "from@example.com", []string{"to@example.com"}, []byte("hello"))
	if err != nil {
		t.Fatalf("expected send to succeed, got %v", err)
	}
	if !dialDeadline.Equal(parentDeadline) {
		t.Fatalf("expected dial deadline %s, got %s", parentDeadline, dialDeadline)
	}
	if !conn.deadline.Equal(parentDeadline) {
		t.Fatalf("expected connection deadline %s, got %s", parentDeadline, conn.deadline)
	}
}

func TestSMTPSendMail_CanceledContextReturnsSafely(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dialCalled := false
	sendMail := smtpSendMail(10*time.Second,
		func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialCalled = true
			_ = network
			_ = addr
			return nil, ctx.Err()
		},
		func(conn net.Conn, host string) (smtpClient, error) {
			_ = conn
			_ = host
			return nil, errors.New("unexpected client creation")
		},
		time.Now,
	)

	err := sendMail(ctx, "smtp.example.com:25", nil, "from@example.com", []string{"to@example.com"}, []byte("hello"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context error, got %v", err)
	}
	if !dialCalled {
		t.Fatal("expected dial to observe the canceled context")
	}
}

type recordingConn struct {
	deadline         time.Time
	setDeadlineCalls int
	closed           bool
}

func (c *recordingConn) Read(b []byte) (int, error) {
	_ = b
	return 0, io.EOF
}

func (c *recordingConn) Write(b []byte) (int, error) {
	return len(b), nil
}

func (c *recordingConn) Close() error {
	c.closed = true
	return nil
}

func (c *recordingConn) LocalAddr() net.Addr {
	return &net.TCPAddr{}
}

func (c *recordingConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{}
}

func (c *recordingConn) SetDeadline(deadline time.Time) error {
	c.deadline = deadline
	c.setDeadlineCalls++
	return nil
}

func (c *recordingConn) SetReadDeadline(deadline time.Time) error {
	_ = deadline
	return nil
}

func (c *recordingConn) SetWriteDeadline(deadline time.Time) error {
	_ = deadline
	return nil
}

type fakeSMTPClient struct {
	writer     nopWriteCloser
	wrote      []byte
	quitCalled bool
}

func (c *fakeSMTPClient) Extension(ext string) (bool, string) {
	_ = ext
	return false, ""
}

func (c *fakeSMTPClient) StartTLS(config *tls.Config) error {
	_ = config
	return nil
}

func (c *fakeSMTPClient) Auth(auth smtp.Auth) error {
	_ = auth
	return nil
}

func (c *fakeSMTPClient) Mail(from string) error {
	_ = from
	return nil
}

func (c *fakeSMTPClient) Rcpt(to string) error {
	_ = to
	return nil
}

func (c *fakeSMTPClient) Data() (io.WriteCloser, error) {
	c.writer.write = func(p []byte) {
		c.wrote = append(c.wrote, p...)
	}
	return c.writer, nil
}

func (c *fakeSMTPClient) Quit() error {
	c.quitCalled = true
	return nil
}

func (c *fakeSMTPClient) Close() error {
	return nil
}

type nopWriteCloser struct {
	io.Writer
	write func([]byte)
}

func (w nopWriteCloser) Write(p []byte) (int, error) {
	if w.write != nil {
		w.write(p)
	}
	if w.Writer == nil {
		return len(p), nil
	}
	return w.Writer.Write(p)
}

func (nopWriteCloser) Close() error {
	return nil
}
