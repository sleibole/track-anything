package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewMailerBoundsSMTP(t *testing.T) {
	m, err := newMailer(config{
		SMTPHost: "smtp.example.com",
		SMTPPort: "587",
		MailFrom: "Track Anything <hello@trackanything.io>",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	sm, ok := m.(smtpMailer)
	if !ok {
		t.Fatalf("mailer type %T", m)
	}
	if sm.timeout != smtpSendTimeout || sm.retryAfter != smtpRetryAfter {
		t.Fatalf("timeout %s retry %s", sm.timeout, sm.retryAfter)
	}
}

func TestSMTPSend(t *testing.T) {
	var mu sync.Mutex
	var got string
	addr, attempts := startSMTP(t, func(_ int, conn net.Conn) {
		speakSMTP(conn, "", func(body string) {
			mu.Lock()
			got = body
			mu.Unlock()
		})
	})

	m := testSMTPMailer(t, addr, time.Second, time.Millisecond)
	if err := m.Send("person@example.com", "Your Track Anything login link", "use this link"); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts %d", attempts.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(got, "Subject: Your Track Anything login link") || !strings.Contains(got, "use this link") {
		t.Fatalf("message %q", got)
	}
}

func TestSMTPSendRetriesATransientFailure(t *testing.T) {
	var mu sync.Mutex
	var got string
	addr, attempts := startSMTP(t, func(attempt int, conn net.Conn) {
		fail := ""
		if attempt == 1 {
			fail = "450 mailbox busy"
		}
		speakSMTP(conn, fail, func(body string) {
			mu.Lock()
			got = body
			mu.Unlock()
		})
	})

	m := testSMTPMailer(t, addr, time.Second, 20*time.Millisecond)
	if err := m.Send("person@example.com", "Your Track Anything login link", "use this link"); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts %d", attempts.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(got, "use this link") {
		t.Fatalf("message %q", got)
	}
}

func TestSMTPSendFailsWhenTheRetryIsExhausted(t *testing.T) {
	addr, attempts := startSMTP(t, func(_ int, conn net.Conn) {
		speakSMTP(conn, "450 mailbox busy", nil)
	})

	m := testSMTPMailer(t, addr, time.Second, 20*time.Millisecond)
	err := m.Send("person@example.com", "subject", "body")
	var reply *textproto.Error
	if !errors.As(err, &reply) || reply.Code != 450 {
		t.Fatalf("got %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts %d", attempts.Load())
	}
}

func TestSMTPSendDoesNotRetryAPermanentFailure(t *testing.T) {
	addr, attempts := startSMTP(t, func(_ int, conn net.Conn) {
		speakSMTP(conn, "550 no such user", nil)
	})

	m := testSMTPMailer(t, addr, time.Second, time.Second)
	start := time.Now()
	err := m.Send("person@example.com", "subject", "body")
	var reply *textproto.Error
	if !errors.As(err, &reply) || reply.Code != 550 {
		t.Fatalf("got %v", err)
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts %d", attempts.Load())
	}
	if elapsed := time.Since(start); elapsed >= 500*time.Millisecond {
		t.Fatalf("permanent failure waited %s", elapsed)
	}
}

func TestProdMailerRefusesCleartext(t *testing.T) {
	addr, attempts := startSMTP(t, func(_ int, conn net.Conn) {
		speakSMTP(conn, "", nil)
	})
	m := testSMTPMailer(t, addr, time.Second, time.Second)
	m.requireTLS = true
	err := m.Send("person@example.com", "subject", "body")
	if !errors.Is(err, errSMTPCleartext) {
		t.Fatalf("got %v", err)
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts %d", attempts.Load())
	}
}

func TestProdMailerRequiresTLS(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	from := "Track Anything <hello@trackanything.io>"
	m, err := newMailer(config{Env: "prod", SMTPHost: "smtp.example.com", SMTPPort: "587", MailFrom: from}, logger)
	if err != nil {
		t.Fatal(err)
	}
	sm := m.(smtpMailer)
	if !sm.requireTLS || sm.implicitTLS {
		t.Fatalf("587: requireTLS %v implicit %v", sm.requireTLS, sm.implicitTLS)
	}
	m, err = newMailer(config{Env: "prod", SMTPHost: "smtp.example.com", SMTPPort: "465", MailFrom: from}, logger)
	if err != nil {
		t.Fatal(err)
	}
	sm = m.(smtpMailer)
	if !sm.requireTLS || !sm.implicitTLS {
		t.Fatalf("465: requireTLS %v implicit %v", sm.requireTLS, sm.implicitTLS)
	}
	m, err = newMailer(config{Env: "dev", SMTPHost: "127.0.0.1", SMTPPort: "1025", MailFrom: from}, logger)
	if err != nil {
		t.Fatal(err)
	}
	if m.(smtpMailer).requireTLS {
		t.Fatal("development mail requires TLS")
	}
}

func TestSMTPSendTimesOut(t *testing.T) {
	addr, attempts := startSMTP(t, func(_ int, conn net.Conn) {
		defer conn.Close()
		io.Copy(io.Discard, conn)
	})

	const timeout = 200 * time.Millisecond
	const retryAfter = 30 * time.Millisecond
	m := testSMTPMailer(t, addr, timeout, retryAfter)

	start := time.Now()
	errc := make(chan error, 1)
	go func() { errc <- m.Send("person@example.com", "subject", "body") }()

	select {
	case err := <-errc:
		elapsed := time.Since(start)
		var ne net.Error
		if !errors.As(err, &ne) || !ne.Timeout() {
			t.Fatalf("got %v, want a timeout", err)
		}
		if attempts.Load() != 2 {
			t.Fatalf("attempts %d", attempts.Load())
		}
		// Far above two short attempts, and far below an unbounded dial.
		if elapsed > 2*time.Second {
			t.Fatalf("send took %s", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("send blocked past the timeout")
	}
}

func testSMTPMailer(t *testing.T, addr string, timeout, retryAfter time.Duration) smtpMailer {
	t.Helper()
	from, err := mail.ParseAddress("Track Anything <hello@example.com>")
	if err != nil {
		t.Fatal(err)
	}
	return smtpMailer{
		addr:       addr,
		from:       from,
		timeout:    timeout,
		retryAfter: retryAfter,
	}
}

// startSMTP accepts connections and runs handle on each. attempt starts at 1.
// The returned counter is how many connections were accepted.
func startSMTP(t *testing.T, handle func(attempt int, conn net.Conn)) (string, *atomic.Int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	var (
		mu    sync.Mutex
		open  []net.Conn
		count atomic.Int32
	)
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range open {
			c.Close()
		}
	})

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			open = append(open, conn)
			mu.Unlock()
			n := int(count.Add(1))
			go handle(n, conn)
		}
	}()
	return ln.Addr().String(), &count
}

// speakSMTP plays a minimal SMTP server. fail is a full reply line, such as
// "450 mailbox busy", returned to MAIL FROM. An empty fail accepts the message.
// got, when set, is called with the DATA payload before that payload is accepted,
// so the caller can read it after Send returns.
func speakSMTP(conn net.Conn, fail string, got func(string)) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	bw := bufio.NewWriter(conn)
	write := func(line string) error {
		if _, err := fmt.Fprintf(bw, "%s\r\n", line); err != nil {
			return err
		}
		return bw.Flush()
	}
	if err := write("220 localhost ESMTP"); err != nil {
		return
	}

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		cmd, _, _ := strings.Cut(strings.TrimRight(line, "\r\n"), " ")
		switch strings.ToUpper(cmd) {
		case "EHLO", "HELO", "RCPT", "RSET", "NOOP":
			if err := write("250 localhost"); err != nil {
				return
			}
		case "MAIL":
			if fail != "" {
				write(fail)
				return
			}
			if err := write("250 OK"); err != nil {
				return
			}
		case "DATA":
			if err := write("354 Start mail input"); err != nil {
				return
			}
			var data strings.Builder
			for {
				l, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				data.WriteString(l)
			}
			if got != nil {
				got(data.String())
			}
			if err := write("250 OK"); err != nil {
				return
			}
		case "QUIT":
			write("221 Bye")
			return
		default:
			if err := write("500 unknown"); err != nil {
				return
			}
		}
	}
}
