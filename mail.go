package main

import (
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

type mailer interface {
	Send(to, subject, body string) error
}

// consoleMailer logs emails instead of sending them, for development.
type consoleMailer struct {
	logger *slog.Logger
}

func (m consoleMailer) Send(to, subject, body string) error {
	m.logger.Info("email not sent (no SMTP_HOST configured)", "to", to, "subject", subject, "body", body)
	return nil
}

// smtpMailer sends plain-text email through a transactional relay. net/smtp upgrades
// to TLS with STARTTLS when the server offers it, which PlainAuth requires.
type smtpMailer struct {
	addr     string // host:port
	username string
	password string
	from     *mail.Address
}

func (m smtpMailer) Send(to, subject, body string) error {
	host, _, err := net.SplitHostPort(m.addr)
	if err != nil {
		return err
	}
	var auth smtp.Auth
	if m.username != "" {
		auth = smtp.PlainAuth("", m.username, m.password, host)
	}

	var msg strings.Builder
	fmt.Fprintf(&msg, "From: %s\r\n", m.from.String())
	fmt.Fprintf(&msg, "To: %s\r\n", to)
	fmt.Fprintf(&msg, "Subject: %s\r\n", subject)
	fmt.Fprintf(&msg, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
	msg.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))

	return smtp.SendMail(m.addr, auth, m.from.Address, []string{to}, []byte(msg.String()))
}

func newMailer(cfg config, logger *slog.Logger) (mailer, error) {
	if cfg.SMTPHost == "" {
		return consoleMailer{logger: logger}, nil
	}
	from, err := mail.ParseAddress(cfg.MailFrom)
	if err != nil {
		return nil, fmt.Errorf("MAIL_FROM: %w", err)
	}
	return smtpMailer{
		addr:     net.JoinHostPort(cfg.SMTPHost, cfg.SMTPPort),
		username: cfg.SMTPUser,
		password: cfg.SMTPPass,
		from:     from,
	}, nil
}
