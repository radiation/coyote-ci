package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

var ErrInvalidMessage = errors.New("email message is invalid")

const DefaultSMTPTimeout = 10 * time.Second

type Sender interface {
	SendText(ctx context.Context, message Message) error
}

type Message struct {
	To      string
	Subject string
	Body    string
}

type Config struct {
	Enabled     bool
	Host        string
	Port        string
	Username    string
	Password    string
	FromAddress string
}

type sendMailFunc func(ctx context.Context, addr string, auth smtp.Auth, from string, to []string, msg []byte) error

type smtpDialContextFunc func(ctx context.Context, network, addr string) (net.Conn, error)

type smtpClient interface {
	Extension(ext string) (bool, string)
	StartTLS(config *tls.Config) error
	Auth(auth smtp.Auth) error
	Mail(from string) error
	Rcpt(to string) error
	Data() (io.WriteCloser, error)
	Quit() error
	Close() error
}

type smtpClientFactory func(conn net.Conn, host string) (smtpClient, error)

type noopSender struct{}

type smtpSender struct {
	addr         string
	auth         smtp.Auth
	envelopeFrom string
	headerFrom   string
	timeout      time.Duration
	sendMail     sendMailFunc
}

func NewSender(cfg Config) (Sender, error) {
	if !cfg.Enabled {
		return noopSender{}, nil
	}

	return newSMTPSender(cfg, smtpSendMailWithTimeout(DefaultSMTPTimeout))
}

func newSMTPSender(cfg Config, sendMail sendMailFunc) (Sender, error) {
	return newSMTPSenderWithTimeout(cfg, DefaultSMTPTimeout, sendMail)
}

func newSMTPSenderWithTimeout(cfg Config, timeout time.Duration, sendMail sendMailFunc) (Sender, error) {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return nil, errors.New("smtp host is required")
	}

	port := strings.TrimSpace(cfg.Port)
	if port == "" {
		return nil, errors.New("smtp port is required")
	}

	fromAddress := strings.TrimSpace(cfg.FromAddress)
	if fromAddress == "" {
		return nil, errors.New("smtp from address is required")
	}

	parsedFrom, parseErr := mail.ParseAddress(fromAddress)
	if parseErr != nil {
		return nil, fmt.Errorf("invalid smtp from address: %w", parseErr)
	}

	username := strings.TrimSpace(cfg.Username)
	password := strings.TrimSpace(cfg.Password)
	if (username == "") != (password == "") {
		return nil, errors.New("smtp username and password must both be set or both be empty")
	}

	var auth smtp.Auth
	if username != "" {
		auth = smtp.PlainAuth("", username, password, host)
	}

	return &smtpSender{
		addr:         host + ":" + port,
		auth:         auth,
		envelopeFrom: parsedFrom.Address,
		headerFrom:   parsedFrom.String(),
		timeout:      timeout,
		sendMail:     sendMail,
	}, nil
}

func (noopSender) SendText(ctx context.Context, message Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_ = message
	return nil
}

func (s *smtpSender) SendText(ctx context.Context, message Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	parsedTo, subject, body, validateErr := validateMessage(message)
	if validateErr != nil {
		return validateErr
	}

	rawMessage := buildPlainTextMessage(s.headerFrom, parsedTo.String(), subject, body)
	sendCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	if sendErr := s.sendMail(sendCtx, s.addr, s.auth, s.envelopeFrom, []string{parsedTo.Address}, rawMessage); sendErr != nil {
		return fmt.Errorf("send email: %w", sendErr)
	}

	return nil
}

func validateMessage(message Message) (*mail.Address, string, string, error) {
	toAddress := strings.TrimSpace(message.To)
	if toAddress == "" {
		return nil, "", "", fmt.Errorf("%w: to address is required", ErrInvalidMessage)
	}

	parsedTo, parseErr := mail.ParseAddress(toAddress)
	if parseErr != nil {
		return nil, "", "", fmt.Errorf("%w: invalid email to address: %v", ErrInvalidMessage, parseErr)
	}

	subject := strings.TrimSpace(message.Subject)
	if subject == "" {
		return nil, "", "", fmt.Errorf("%w: subject is required", ErrInvalidMessage)
	}

	body := strings.TrimSpace(message.Body)
	if body == "" {
		return nil, "", "", fmt.Errorf("%w: body is required", ErrInvalidMessage)
	}

	return parsedTo, sanitizeHeaderValue(subject), normalizeBody(body), nil
}

func buildPlainTextMessage(from, to, subject, body string) []byte {
	return []byte(
		"From: " + from + "\r\n" +
			"To: " + to + "\r\n" +
			"Subject: " + subject + "\r\n" +
			"MIME-Version: 1.0\r\n" +
			"Content-Type: text/plain; charset=UTF-8\r\n" +
			"\r\n" + body,
	)
}

func sanitizeHeaderValue(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}

func normalizeBody(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	return strings.ReplaceAll(body, "\n", "\r\n")
}

func smtpSendMailWithTimeout(timeout time.Duration) sendMailFunc {
	return smtpSendMail(timeout, (&net.Dialer{}).DialContext, func(conn net.Conn, host string) (smtpClient, error) {
		return smtp.NewClient(conn, host)
	}, time.Now)
}

func smtpSendMail(timeout time.Duration, dial smtpDialContextFunc, newClient smtpClientFactory, now func() time.Time) sendMailFunc {
	return func(ctx context.Context, addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		host, _, splitErr := net.SplitHostPort(addr)
		if splitErr != nil {
			return splitErr
		}

		operationCtx, cancel, deadline := smtpOperationContext(ctx, timeout, now)
		defer cancel()

		conn, dialErr := dial(operationCtx, "tcp", addr)
		if dialErr != nil {
			return dialErr
		}
		defer func() {
			_ = conn.Close()
		}()
		if !deadline.IsZero() {
			if setErr := conn.SetDeadline(deadline); setErr != nil {
				return setErr
			}
		}

		client, clientErr := newClient(conn, host)
		if clientErr != nil {
			return clientErr
		}
		defer func() {
			_ = client.Close()
		}()

		if ok, _ := client.Extension("STARTTLS"); ok {
			if startTLSErr := client.StartTLS(&tls.Config{ServerName: host}); startTLSErr != nil {
				return startTLSErr
			}
		}
		if auth != nil {
			if ok, _ := client.Extension("AUTH"); !ok {
				return errors.New("smtp server does not support AUTH")
			}
			if authErr := client.Auth(auth); authErr != nil {
				return authErr
			}
		}
		if mailErr := client.Mail(from); mailErr != nil {
			return mailErr
		}
		for _, recipient := range to {
			if rcptErr := client.Rcpt(recipient); rcptErr != nil {
				return rcptErr
			}
		}
		writer, dataErr := client.Data()
		if dataErr != nil {
			return dataErr
		}
		if _, writeErr := writer.Write(msg); writeErr != nil {
			_ = writer.Close()
			return writeErr
		}
		if closeErr := writer.Close(); closeErr != nil {
			return closeErr
		}
		return client.Quit()
	}
}

func smtpOperationContext(ctx context.Context, timeout time.Duration, now func() time.Time) (context.Context, context.CancelFunc, time.Time) {
	if timeout <= 0 {
		if deadline, ok := ctx.Deadline(); ok {
			return ctx, func() {}, deadline
		}
		return ctx, func() {}, time.Time{}
	}

	deadline := now().Add(timeout)
	if parentDeadline, ok := ctx.Deadline(); ok && !parentDeadline.After(deadline) {
		return ctx, func() {}, parentDeadline
	}

	operationCtx, cancel := context.WithDeadline(ctx, deadline)
	return operationCtx, cancel, deadline
}
