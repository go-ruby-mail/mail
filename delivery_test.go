// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mail

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// sampleMessage builds a small message with From/To/Cc/Bcc for delivery tests.
func sampleMessage() *Message {
	return New("").
		SetFrom("Alice <alice@example.com>").
		SetTo("bob@example.com").
		SetCc("carol@example.com").
		SetBcc("dave@example.com").
		SetSubject("Greetings").
		SetBody("Hello there\r\n")
}

// --- Envelope -----------------------------------------------------------------

func TestEnvelopeFromToAndBccStripped(t *testing.T) {
	env, err := buildEnvelope(sampleMessage())
	if err != nil {
		t.Fatalf("buildEnvelope: %v", err)
	}
	if env.from != "alice@example.com" {
		t.Errorf("from = %q", env.from)
	}
	want := []string{"bob@example.com", "carol@example.com", "dave@example.com"}
	if strings.Join(env.to, ",") != strings.Join(want, ",") {
		t.Errorf("to = %v, want %v", env.to, want)
	}
	if strings.Contains(env.message, "Bcc:") {
		t.Errorf("delivery message must not contain Bcc:\n%s", env.message)
	}
	if strings.Contains(env.message, "dave@example.com") {
		t.Errorf("bcc recipient leaked into message body:\n%s", env.message)
	}
}

func TestEnvelopeSenderPrecedence(t *testing.T) {
	// Return-Path wins over Sender and From.
	m := New("").SetFrom("from@x.com").SetHeader("Sender", "sender@x.com").
		SetHeader("Return-Path", "<rp@x.com>").SetTo("t@x.com").SetBody("b")
	if got := m.smtpEnvelopeFrom(); got != "rp@x.com" {
		t.Errorf("return-path precedence: %q", got)
	}
	// Sender wins over From when no Return-Path.
	m2 := New("").SetFrom("from@x.com").SetHeader("Sender", "sender@x.com").SetTo("t@x.com").SetBody("b")
	if got := m2.smtpEnvelopeFrom(); got != "sender@x.com" {
		t.Errorf("sender precedence: %q", got)
	}
	// From when nothing else.
	m3 := New("").SetFrom("from@x.com").SetTo("t@x.com").SetBody("b")
	if got := m3.smtpEnvelopeFrom(); got != "from@x.com" {
		t.Errorf("from fallback: %q", got)
	}
	// Nothing at all.
	m4 := New("").SetTo("t@x.com").SetBody("b")
	if got := m4.smtpEnvelopeFrom(); got != "" {
		t.Errorf("empty from: %q", got)
	}
}

func TestFirstAddrEmptyTokens(t *testing.T) {
	if got := firstAddr(""); got != "" {
		t.Errorf("empty input: %q", got)
	}
	if got := firstAddr(";"); got != "" { // group with no members => zero addrs
		t.Errorf("no-member group: %q", got)
	}
}

func TestEnvelopeValidationErrors(t *testing.T) {
	if _, err := newEnvelope("", []string{"a@x.com"}, "msg"); err == nil {
		t.Error("blank from should error")
	}
	if _, err := newEnvelope(strings.Repeat("a", maxAddrBytes+1)+"@x.com", []string{"a@x.com"}, "m"); err == nil {
		t.Error("over-long from should error")
	}
	if _, err := newEnvelope("a@x.com", nil, "m"); err == nil {
		t.Error("empty to should error")
	}
	if _, err := newEnvelope("a@x.com", []string{"   "}, "m"); err == nil {
		t.Error("blank to should error")
	}
	if _, err := newEnvelope("a@x.com", []string{strings.Repeat("b", maxAddrBytes+1)}, "m"); err == nil {
		t.Error("over-long to should error")
	}
	if _, err := newEnvelope("a@x.com", []string{"a@x\r\ninject"}, "m"); err == nil {
		t.Error("CRLF in to should error")
	}
	if _, err := newEnvelope("a@x.com", []string{"a@x.com"}, "   "); err == nil {
		t.Error("blank message should error")
	}
	if _, err := newEnvelope("from@x\r\n.com", []string{"a@x.com"}, "m"); err == nil {
		t.Error("CRLF in from should error")
	}
}

// --- SMTP happy paths ---------------------------------------------------------

func TestSMTPDeliverPlainAuth(t *testing.T) {
	srv := &fakeSMTP{ext: []string{"AUTH PLAIN LOGIN CRAM-MD5"}}
	s := &SMTP{
		Address: "127.0.0.1", Domain: "client.local",
		UserName: "user", Password: "pass", Authentication: "plain",
		Dial: tcpDialer(t, srv.serve),
	}
	if err := s.Deliver(sampleMessage()); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	from, rcpt, body := srv.recorded()
	if from != "alice@example.com" {
		t.Errorf("from = %q", from)
	}
	if strings.Join(rcpt, ",") != "bob@example.com,carol@example.com,dave@example.com" {
		t.Errorf("rcpt = %v", rcpt)
	}
	if !strings.Contains(body, "Subject: Greetings") {
		t.Errorf("body missing subject:\n%s", body)
	}
	if strings.Contains(body, "Bcc:") {
		t.Errorf("bcc leaked:\n%s", body)
	}
}

func TestSMTPDeliverLoginAuth(t *testing.T) {
	srv := &fakeSMTP{ext: []string{"AUTH LOGIN"}}
	s := &SMTP{Address: "127.0.0.1", UserName: "u", Password: "p",
		Authentication: "login", Dial: tcpDialer(t, srv.serve)}
	if err := s.Deliver(sampleMessage()); err != nil {
		t.Fatalf("deliver: %v", err)
	}
}

func TestSMTPDeliverCramMD5Auth(t *testing.T) {
	srv := &fakeSMTP{ext: []string{"AUTH CRAM-MD5"}}
	s := &SMTP{Address: "127.0.0.1", UserName: "u", Password: "p",
		Authentication: "cram_md5", Dial: tcpDialer(t, srv.serve)}
	if err := s.Deliver(sampleMessage()); err != nil {
		t.Fatalf("deliver: %v", err)
	}
}

func TestSMTPDeliverNoAuthNoDomain(t *testing.T) {
	// No Domain (skip explicit Hello) and no auth.
	srv := &fakeSMTP{}
	s := &SMTP{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	if err := s.Deliver(sampleMessage()); err != nil {
		t.Fatalf("deliver: %v", err)
	}
}

func TestSMTPDeliverImplicitTLS(t *testing.T) {
	scfg, ccfg := testTLS(t)
	srv := &fakeSMTP{implicit: true, tlsCfg: scfg}
	s := &SMTP{Address: "127.0.0.1", SSL: true, TLSConfig: ccfg, Dial: tcpDialer(t, srv.serve)}
	if err := s.Deliver(sampleMessage()); err != nil {
		t.Fatalf("deliver: %v", err)
	}
}

func TestSMTPDeliverStartTLSAuto(t *testing.T) {
	scfg, _ := testTLS(t)
	srv := &fakeSMTP{ext: []string{"STARTTLS", "AUTH PLAIN"}, tlsCfg: scfg}
	// OpenSSLVerifyMode "none" -> InsecureSkipVerify accepts the self-signed cert.
	s := &SMTP{Address: "127.0.0.1", EnableStartTLSAuto: true, OpenSSLVerifyMode: "none",
		UserName: "u", Password: "p", Authentication: "plain",
		Dial: tcpDialer(t, srv.serve)}
	if err := s.Deliver(sampleMessage()); err != nil {
		t.Fatalf("deliver: %v", err)
	}
}

func TestSMTPDeliverStartTLSAutoNotAdvertised(t *testing.T) {
	// EnableStartTLSAuto set but server does not advertise STARTTLS -> skipped.
	srv := &fakeSMTP{}
	s := &SMTP{Address: "127.0.0.1", EnableStartTLSAuto: true, Dial: tcpDialer(t, srv.serve)}
	if err := s.Deliver(sampleMessage()); err != nil {
		t.Fatalf("deliver: %v", err)
	}
}

// --- SMTP error paths ---------------------------------------------------------

func TestSMTPDialError(t *testing.T) {
	s := &SMTP{Address: "127.0.0.1", Dial: errDialer(errors.New("no route"))}
	if err := s.Deliver(sampleMessage()); err == nil {
		t.Fatal("expected dial error")
	}
}

func TestSMTPDefaultDialErrorToClosedPort(t *testing.T) {
	// Nil Dial exercises the net.Dial default; port 1 refuses instantly.
	s := &SMTP{Address: "127.0.0.1", Port: 1}
	if err := s.Deliver(sampleMessage()); err == nil {
		t.Fatal("expected connection error to closed port")
	}
}

func TestSMTPEnvelopeErrorPropagates(t *testing.T) {
	// No From -> envelope build fails before any dial.
	m := New("").SetTo("t@x.com").SetBody("b")
	s := &SMTP{Address: "127.0.0.1", Dial: tcpDialer(t, (&fakeSMTP{}).serve)}
	if err := s.Deliver(m); err == nil {
		t.Fatal("expected envelope error")
	}
}

func TestSMTPGreetingError(t *testing.T) {
	srv := &fakeSMTP{fail: map[string]string{"GREET": "554 go away"}}
	s := &SMTP{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	if err := s.Deliver(sampleMessage()); err == nil {
		t.Fatal("expected greeting error")
	}
}

func TestSMTPHelloError(t *testing.T) {
	srv := &fakeSMTP{fail: map[string]string{"EHLO": "502 bad"}}
	s := &SMTP{Address: "127.0.0.1", Domain: "client.local", Dial: tcpDialer(t, srv.serve)}
	if err := s.Deliver(sampleMessage()); err == nil {
		t.Fatal("expected hello error")
	}
}

func TestSMTPStartTLSError(t *testing.T) {
	scfg, _ := testTLS(t)
	// Server advertises STARTTLS; client verifies the cert (OpenSSLVerifyMode "")
	// against system roots -> self-signed rejected -> StartTLS fails.
	srv := &fakeSMTP{ext: []string{"STARTTLS"}, tlsCfg: scfg}
	s := &SMTP{Address: "127.0.0.1", EnableStartTLSAuto: true, Dial: tcpDialer(t, srv.serve)}
	if err := s.Deliver(sampleMessage()); err == nil {
		t.Fatal("expected STARTTLS verification error")
	}
}

func TestSMTPStartTLSCommandRejected(t *testing.T) {
	scfg, ccfg := testTLS(t)
	srv := &fakeSMTP{ext: []string{"STARTTLS"}, tlsCfg: scfg,
		fail: map[string]string{"STARTTLS": "454 TLS temporarily unavailable"}}
	s := &SMTP{Address: "127.0.0.1", EnableStartTLSAuto: true, TLSConfig: ccfg, Dial: tcpDialer(t, srv.serve)}
	if err := s.Deliver(sampleMessage()); err == nil {
		t.Fatal("expected STARTTLS command rejection")
	}
}

func TestSMTPUnknownAuthMechanism(t *testing.T) {
	srv := &fakeSMTP{}
	s := &SMTP{Address: "127.0.0.1", Authentication: "bogus", Dial: tcpDialer(t, srv.serve)}
	if err := s.Deliver(sampleMessage()); err == nil {
		t.Fatal("expected unknown-auth error")
	}
}

func TestSMTPAuthRejected(t *testing.T) {
	srv := &fakeSMTP{ext: []string{"AUTH PLAIN"}, fail: map[string]string{"AUTH": "535 5.7.8 bad creds"}}
	s := &SMTP{Address: "127.0.0.1", UserName: "u", Password: "p",
		Authentication: "plain", Dial: tcpDialer(t, srv.serve)}
	if err := s.Deliver(sampleMessage()); err == nil {
		t.Fatal("expected auth rejection")
	}
}

func TestSMTPMailError(t *testing.T) {
	srv := &fakeSMTP{fail: map[string]string{"MAIL": "550 sender rejected"}}
	s := &SMTP{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	if err := s.Deliver(sampleMessage()); err == nil {
		t.Fatal("expected MAIL error")
	}
}

func TestSMTPRcptError(t *testing.T) {
	srv := &fakeSMTP{fail: map[string]string{"RCPT": "550 recipient rejected"}}
	s := &SMTP{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	if err := s.Deliver(sampleMessage()); err == nil {
		t.Fatal("expected RCPT error")
	}
}

func TestSMTPDataError(t *testing.T) {
	srv := &fakeSMTP{fail: map[string]string{"DATA": "554 no data"}}
	s := &SMTP{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	if err := s.Deliver(sampleMessage()); err == nil {
		t.Fatal("expected DATA error")
	}
}

func TestSMTPWriteError(t *testing.T) {
	// Server accepts DATA then drops the connection; a large body overflows the
	// client's write buffer and flushes into the closed pipe -> write error.
	srv := &fakeSMTP{closeAfter354: true}
	s := &SMTP{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	big := New("").SetFrom("a@x.com").SetTo("b@x.com").SetBody(strings.Repeat("x", 200000))
	if err := s.Deliver(big); err == nil {
		t.Fatal("expected write error")
	}
}

func TestSMTPDataCloseError(t *testing.T) {
	srv := &fakeSMTP{dotReply: "550 message rejected"}
	s := &SMTP{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	if err := s.Deliver(sampleMessage()); err == nil {
		t.Fatal("expected data-close error")
	}
}

func TestSMTPQuitError(t *testing.T) {
	srv := &fakeSMTP{quitFail: true}
	s := &SMTP{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	if err := s.Deliver(sampleMessage()); err == nil {
		t.Fatal("expected QUIT error")
	}
}

func TestSMTPDefaultsAddressPort(t *testing.T) {
	s := &SMTP{}
	if s.host() != "localhost" || s.port() != 25 {
		t.Errorf("defaults host=%q port=%d", s.host(), s.port())
	}
}

// --- loginAuth ----------------------------------------------------------------

func TestLoginAuthNext(t *testing.T) {
	a := &loginAuth{username: "bob", password: "secret"}
	mech, ir, err := a.Start(nil)
	if mech != "LOGIN" || ir != nil || err != nil {
		t.Fatalf("start: %q %v %v", mech, ir, err)
	}
	if got, _ := a.Next([]byte("Username:"), true); string(got) != "bob" {
		t.Errorf("username = %q", got)
	}
	if got, _ := a.Next([]byte("Password:"), true); string(got) != "secret" {
		t.Errorf("password = %q", got)
	}
	if got, err := a.Next(nil, false); got != nil || err != nil {
		t.Errorf("done: %q %v", got, err)
	}
	if _, err := a.Next([]byte("Weird?"), true); err == nil {
		t.Error("unexpected challenge should error")
	}
}

// --- Sendmail -----------------------------------------------------------------

func TestSendmailDeliverArgs(t *testing.T) {
	var gotPath string
	var gotArgs []string
	var gotStdin []byte
	s := &Sendmail{
		Location: "/opt/sendmail",
		Run: func(path string, args []string, stdin []byte) error {
			gotPath, gotArgs, gotStdin = path, args, stdin
			return nil
		},
	}
	if err := s.Deliver(sampleMessage()); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if gotPath != "/opt/sendmail" {
		t.Errorf("path = %q", gotPath)
	}
	want := []string{"-i", "-f", "alice@example.com", "--", "bob@example.com", "carol@example.com", "dave@example.com"}
	if strings.Join(gotArgs, " ") != strings.Join(want, " ") {
		t.Errorf("args = %v, want %v", gotArgs, want)
	}
	if strings.Contains(string(gotStdin), "\r\n") {
		t.Errorf("stdin must be LF-normalised:\n%q", gotStdin)
	}
	if !strings.Contains(string(gotStdin), "Subject: Greetings") {
		t.Errorf("stdin missing subject")
	}
}

func TestSendmailDefaultsAndNoFrom(t *testing.T) {
	// Defaults: location /usr/sbin/sendmail, args [-i]. No From -> envelope error.
	s := &Sendmail{Run: func(string, []string, []byte) error { return nil }}
	if s.location() != "/usr/sbin/sendmail" {
		t.Errorf("location default = %q", s.location())
	}
	if strings.Join(s.args(), " ") != "-i" {
		t.Errorf("args default = %v", s.args())
	}
	m := New("").SetTo("t@x.com").SetBody("b")
	if err := s.Deliver(m); err == nil {
		t.Fatal("expected envelope error")
	}
}

func TestSendmailExplicitArguments(t *testing.T) {
	var gotArgs []string
	s := &Sendmail{Arguments: []string{"-oi", "-t"}, Run: func(_ string, args []string, _ []byte) error {
		gotArgs = args
		return nil
	}}
	if err := s.Deliver(sampleMessage()); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if strings.Join(s.args(), " ") != "-oi -t" {
		t.Errorf("args() = %v", s.args())
	}
	if gotArgs[0] != "-oi" || gotArgs[1] != "-t" {
		t.Errorf("delivered args = %v", gotArgs)
	}
}

func TestSendmailRunError(t *testing.T) {
	s := &Sendmail{Run: func(string, []string, []byte) error { return errors.New("boom") }}
	if err := s.Deliver(sampleMessage()); err == nil {
		t.Fatal("expected run error")
	}
}

func TestSendmailNilRunUsesDefaultRunner(t *testing.T) {
	// Leaving Run nil selects the package sendmailRunner; stub it so the branch is
	// covered deterministically on every platform.
	orig := sendmailRunner
	defer func() { sendmailRunner = orig }()
	called := false
	sendmailRunner = func(string, []string, []byte) error { called = true; return nil }
	s := &Sendmail{Location: "sendmail-x"}
	if err := s.Deliver(sampleMessage()); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if !called {
		t.Fatal("default runner not invoked")
	}
}

func TestExecRun(t *testing.T) {
	okCmd, okArgs, failArgs := probeCommands()
	if err := execRun(okCmd, okArgs, []byte("ignored")); err != nil {
		// Environments where sub-exec is unavailable (e.g. qemu-user) don't gate
		// coverage, so skip rather than fail there.
		t.Skipf("cannot exec %s in this environment: %v", okCmd, err)
	}
	if err := execRun(okCmd, failArgs, nil); err == nil {
		t.Fatalf("expected non-zero exit from %s %v", okCmd, failArgs)
	}
}

// probeCommands returns an OS-appropriate command that ignores its args plus
// argument sets that exit 0 and non-zero.
func probeCommands() (cmd string, okArgs, failArgs []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", "exit", "0"}, []string{"/c", "exit", "1"}
	}
	return "sh", []string{"-c", "exit 0"}, []string{"-c", "exit 1"}
}

// --- TestDelivery / FileDelivery / LoggerDelivery -----------------------------

func TestTestDelivery(t *testing.T) {
	td := &TestDelivery{}
	if err := td.Deliver(sampleMessage()); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if err := td.Deliver(sampleMessage()); err != nil {
		t.Fatalf("deliver 2: %v", err)
	}
	if got := td.Deliveries(); len(got) != 2 {
		t.Errorf("deliveries = %d", len(got))
	}
	td.Clear()
	if got := td.Deliveries(); len(got) != 0 {
		t.Errorf("after clear = %d", len(got))
	}
	// Envelope error propagates.
	if err := td.Deliver(New("").SetTo("t@x.com").SetBody("b")); err == nil {
		t.Error("expected envelope error")
	}
}

func TestFileDelivery(t *testing.T) {
	dir := t.TempDir()
	f := &FileDelivery{Location: dir, Extension: ".eml"}
	// Deliver twice to the same recipients: files are appended.
	if err := f.Deliver(sampleMessage()); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if err := f.Deliver(sampleMessage()); err != nil {
		t.Fatalf("deliver 2: %v", err)
	}
	for _, to := range []string{"bob@example.com", "carol@example.com", "dave@example.com"} {
		data, err := os.ReadFile(filepath.Join(dir, to+".eml"))
		if err != nil {
			t.Fatalf("read %s: %v", to, err)
		}
		if n := strings.Count(string(data), "Subject: Greetings"); n != 2 {
			t.Errorf("%s appended %d times, want 2", to, n)
		}
	}
}

func TestFileDeliveryUniqueRecipients(t *testing.T) {
	// A recipient appearing in both To and Cc is written once (the seen-skip).
	m := New("").SetFrom("a@x.com").SetTo("bob@example.com").
		SetCc("bob@example.com").SetBody("hi")
	dir := t.TempDir()
	if err := (&FileDelivery{Location: dir}).Deliver(m); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 file for duplicated recipient, got %d", len(entries))
	}
}

func TestFileDeliveryDefaultsAndEnvelopeError(t *testing.T) {
	f := &FileDelivery{}
	if f.location() != "./mails" {
		t.Errorf("default location = %q", f.location())
	}
	if err := f.Deliver(New("").SetTo("t@x.com").SetBody("b")); err == nil {
		t.Error("expected envelope error")
	}
}

func TestFileDeliveryMkdirError(t *testing.T) {
	orig := mkdirAll
	defer func() { mkdirAll = orig }()
	mkdirAll = func(string, os.FileMode) error { return errors.New("mkdir denied") }
	f := &FileDelivery{Location: "/whatever"}
	if err := f.Deliver(sampleMessage()); err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestFileDeliveryOpenError(t *testing.T) {
	orig := fileOpener
	defer func() { fileOpener = orig }()
	fileOpener = func(string) (io.WriteCloser, error) { return nil, errors.New("open denied") }
	f := &FileDelivery{Location: t.TempDir()}
	if err := f.Deliver(sampleMessage()); err == nil {
		t.Fatal("expected open error")
	}
}

func TestFileDeliveryWriteAndCloseErrors(t *testing.T) {
	orig := fileOpener
	defer func() { fileOpener = orig }()

	// Write error.
	fileOpener = func(string) (io.WriteCloser, error) { return errWriteCloser{writeErr: errors.New("disk full")}, nil }
	if err := (&FileDelivery{Location: t.TempDir()}).Deliver(sampleMessage()); err == nil {
		t.Fatal("expected write error")
	}
	// Close error (write succeeds).
	fileOpener = func(string) (io.WriteCloser, error) { return errWriteCloser{closeErr: errors.New("close failed")}, nil }
	if err := (&FileDelivery{Location: t.TempDir()}).Deliver(sampleMessage()); err == nil {
		t.Fatal("expected close error")
	}
}

type errWriteCloser struct {
	writeErr error
	closeErr error
}

func (e errWriteCloser) Write(p []byte) (int, error) {
	if e.writeErr != nil {
		return 0, e.writeErr
	}
	return len(p), nil
}
func (e errWriteCloser) Close() error { return e.closeErr }

func TestLoggerDelivery(t *testing.T) {
	var lg captureLogger
	l := &LoggerDelivery{Logger: &lg}
	if err := l.Deliver(sampleMessage()); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if !strings.Contains(lg.last, "Subject: Greetings") {
		t.Errorf("logged = %q", lg.last)
	}
	// Envelope error.
	if err := l.Deliver(New("").SetTo("t@x.com").SetBody("b")); err == nil {
		t.Error("expected envelope error")
	}
}

func TestLoggerDeliveryDefaultLogger(t *testing.T) {
	// Nil Logger falls back to the package default (writes to stdout).
	l := &LoggerDelivery{}
	if err := l.Deliver(sampleMessage()); err != nil {
		t.Fatalf("deliver: %v", err)
	}
}

type captureLogger struct{ last string }

func (c *captureLogger) Print(v ...any) { c.last = fmt.Sprint(v...) }

// --- Global config (Defaults / Message.Deliver / DeliverWith) -----------------

func TestMessageDeliverUsesDefaults(t *testing.T) {
	resetDefaults(t)
	td := &TestDelivery{}
	Defaults(func(c *Config) { c.SetDeliveryMethod(td) })
	if err := sampleMessage().Deliver(); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if len(td.Deliveries()) != 1 {
		t.Errorf("deliveries = %d", len(td.Deliveries()))
	}
}

func TestMessageDeliverNoDefault(t *testing.T) {
	resetDefaults(t)
	if err := sampleMessage().Deliver(); err == nil {
		t.Fatal("expected no-delivery-method error")
	}
}

func TestMessageDeliverWith(t *testing.T) {
	td := &TestDelivery{}
	if err := sampleMessage().DeliverWith(td); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if len(td.Deliveries()) != 1 {
		t.Errorf("deliveries = %d", len(td.Deliveries()))
	}
}

// resetDefaults clears the process-wide delivery/retriever config and restores
// it after the test.
func resetDefaults(t *testing.T) {
	t.Helper()
	cfgMu.Lock()
	pd, pr := globalDelivery, globalRetriever
	globalDelivery, globalRetriever = nil, nil
	cfgMu.Unlock()
	t.Cleanup(func() {
		cfgMu.Lock()
		globalDelivery, globalRetriever = pd, pr
		cfgMu.Unlock()
	})
}
