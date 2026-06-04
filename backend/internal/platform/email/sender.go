package email

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/smtp"
	"strings"
)

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

type sendMailFunc func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error

type noopSender struct{}

type smtpSender struct {
	addr         string
	auth         smtp.Auth
	envelopeFrom string
	headerFrom   string
	sendMail     sendMailFunc
}

func NewSender(cfg Config) (Sender, error) {
	if !cfg.Enabled {
		return noopSender{}, nil
	}

	return newSMTPSender(cfg, smtp.SendMail)
}

func newSMTPSender(cfg Config, sendMail sendMailFunc) (Sender, error) {
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
	if sendErr := s.sendMail(s.addr, s.auth, s.envelopeFrom, []string{parsedTo.Address}, rawMessage); sendErr != nil {
		return fmt.Errorf("send email: %w", sendErr)
	}

	return nil
}

func validateMessage(message Message) (*mail.Address, string, string, error) {
	toAddress := strings.TrimSpace(message.To)
	if toAddress == "" {
		return nil, "", "", errors.New("email to address is required")
	}

	parsedTo, parseErr := mail.ParseAddress(toAddress)
	if parseErr != nil {
		return nil, "", "", fmt.Errorf("invalid email to address: %w", parseErr)
	}

	subject := strings.TrimSpace(message.Subject)
	if subject == "" {
		return nil, "", "", errors.New("email subject is required")
	}

	body := strings.TrimSpace(message.Body)
	if body == "" {
		return nil, "", "", errors.New("email body is required")
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
