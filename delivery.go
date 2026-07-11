// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Delivery — sending a [Message] over a transport — mirroring the Ruby `mail`
// gem's delivery methods (Mail.defaults { delivery_method … } + message.deliver).
//
// Every transport is modelled behind an injectable seam: SMTP dials through a
// [Dialer] (defaulting to net.Dial), sendmail runs through a swappable runner,
// and the file/logger sinks go through overridable openers. This keeps the code
// pure-Go (CGO-free) yet fully drivable by in-process fake servers in tests — no
// real mail server is ever contacted.
package mail

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/smtp"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Dialer establishes a transport connection to address (host:port), mirroring
// the signature of net.Dial. It is the seam through which every socket-opening
// delivery/retrieval method reaches its server; tests inject a Dialer that
// returns one end of an in-process net.Pipe so a fake server can drive the
// exchange without any real network.
type Dialer func(network, address string) (net.Conn, error)

// DeliveryMethod is a transport that sends a [Message], mirroring the gem's
// delivery-method objects (each responds to deliver!). Implementations:
// [SMTP], [Sendmail], [TestDelivery], [FileDelivery] and [LoggerDelivery].
type DeliveryMethod interface {
	// Deliver sends m, returning any transport or validation error.
	Deliver(m *Message) error
}

// --- Process-wide configuration (Mail.defaults { … }) --------------------------

var (
	cfgMu           sync.Mutex
	globalDelivery  DeliveryMethod
	globalRetriever RetrieverMethod
)

// Config is the receiver of a [Defaults] block, mirroring the object yielded by
// Ruby's Mail.defaults. Set the process-wide delivery and retriever methods on
// it.
type Config struct{}

// Defaults configures the process-wide delivery/retriever methods, mirroring
// Mail.defaults do … end. The block receives a [Config] to set the methods on.
func Defaults(fn func(*Config)) { fn(&Config{}) }

// SetDeliveryMethod installs d as the process-wide delivery method used by
// [Message.Deliver], mirroring `delivery_method :smtp, …`.
func (c *Config) SetDeliveryMethod(d DeliveryMethod) {
	cfgMu.Lock()
	globalDelivery = d
	cfgMu.Unlock()
}

// SetRetrieverMethod installs r as the process-wide retriever used by [Find] /
// [First] / [Last] / [All], mirroring `retriever_method :pop3, …`.
func (c *Config) SetRetrieverMethod(r RetrieverMethod) {
	cfgMu.Lock()
	globalRetriever = r
	cfgMu.Unlock()
}

func currentDelivery() DeliveryMethod {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	return globalDelivery
}

// Deliver sends the message via the process-wide delivery method configured with
// [Defaults], mirroring Mail::Message#deliver. It errors if no delivery method
// has been configured.
func (m *Message) Deliver() error {
	d := currentDelivery()
	if d == nil {
		return fmt.Errorf("mail: no delivery method configured; call Defaults to set one")
	}
	return d.Deliver(m)
}

// DeliverWith sends the message via the given delivery method, mirroring
// assigning a per-message delivery_method then calling deliver.
func (m *Message) DeliverWith(d DeliveryMethod) error { return d.Deliver(m) }

// --- SMTP envelope (Mail::SmtpEnvelope) ---------------------------------------

// maxAddrBytes caps an envelope address length, matching the gem's
// MAX_ADDRESS_BYTESIZE guard against SMTP line-length overflow.
const maxAddrBytes = 2000

// smtpEnvelope is the (from, recipients, message) triple handed to a transport,
// mirroring Mail::SmtpEnvelope.
type smtpEnvelope struct {
	from    string
	to      []string
	message string
}

// buildEnvelope derives the SMTP envelope from m: the sender from
// Return-Path/Sender/From, the recipients from To+Cc+Bcc, and the wire message
// with the Bcc header stripped (as the gem does on encode).
func buildEnvelope(m *Message) (*smtpEnvelope, error) {
	return newEnvelope(m.smtpEnvelopeFrom(), m.smtpEnvelopeTo(), m.encodedForDelivery())
}

// newEnvelope validates the envelope fields the way Mail::SmtpEnvelope does: a
// non-blank sender and recipient list, each address within the byte cap and free
// of CR/LF, and a non-blank message.
func newEnvelope(from string, to []string, message string) (*smtpEnvelope, error) {
	if blank(from) {
		return nil, fmt.Errorf("mail: SMTP From address may not be blank: %q", from)
	}
	if err := validateAddr("From", from); err != nil {
		return nil, err
	}
	if allBlank(to) {
		return nil, fmt.Errorf("mail: SMTP To address may not be blank: %v", to)
	}
	for _, a := range to {
		if err := validateAddr("To", a); err != nil {
			return nil, err
		}
	}
	if blank(message) {
		return nil, fmt.Errorf("mail: SMTP message may not be blank")
	}
	return &smtpEnvelope{from: from, to: to, message: message}, nil
}

// validateAddr enforces the byte cap and the no-CR/LF rule on a single envelope
// address, mirroring Mail::SmtpEnvelope#validate_addr.
func validateAddr(name, addr string) error {
	if len(addr) > maxAddrBytes {
		return fmt.Errorf("mail: SMTP %s address may not exceed %d bytes", name, maxAddrBytes)
	}
	if strings.ContainsAny(addr, "\r\n") {
		return fmt.Errorf("mail: SMTP %s address may not contain CR or LF line breaks", name)
	}
	return nil
}

// blank reports whether s is empty or all whitespace, mirroring
// Mail::Utilities.blank?.
func blank(s string) bool { return strings.TrimSpace(s) == "" }

// allBlank reports whether the list is empty or every entry is blank.
func allBlank(ss []string) bool {
	for _, s := range ss {
		if !blank(s) {
			return false
		}
	}
	return true
}

// smtpEnvelopeFrom returns the envelope sender, preferring Return-Path, then
// Sender, then the first From address — mirroring Mail#smtp_envelope_from.
func (m *Message) smtpEnvelopeFrom() string {
	if rp := firstAddr(m.header.value("Return-Path")); rp != "" {
		return rp
	}
	if s := firstAddr(m.header.value("Sender")); s != "" {
		return s
	}
	if f := m.From(); len(f) > 0 {
		return f[0]
	}
	return ""
}

// firstAddr parses an address field and returns the first addr-spec, or "".
func firstAddr(v string) string {
	if v == "" {
		return ""
	}
	al := NewAddressList(v)
	if len(al.addrs) == 0 {
		return ""
	}
	return al.addrs[0].Address()
}

// smtpEnvelopeTo returns the envelope recipients: the To, Cc and Bcc addr-specs
// concatenated, mirroring Mail#destinations.
func (m *Message) smtpEnvelopeTo() []string {
	out := make([]string, 0)
	out = append(out, m.To()...)
	out = append(out, m.Cc()...)
	out = append(out, m.Bcc()...)
	return out
}

// encodedForDelivery serialises the message for transmission exactly like
// [Message.Encoded] but omits the Bcc header, matching the gem — "Bcc field does
// not get output into an email".
func (m *Message) encodedForDelivery() string {
	var b strings.Builder
	for _, f := range m.header.fields {
		if strings.EqualFold(f.Name, "Bcc") {
			continue
		}
		b.WriteString(foldField(f.Name, encodeFieldValue(f.Name, f.Value)))
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")
	b.WriteString(m.bodyEncoded())
	return b.String()
}

// --- SMTP delivery (Mail::SMTP) -----------------------------------------------

// SMTP delivers a message over SMTP, mirroring the gem's `delivery_method :smtp`
// with its address/port/user_name/password/authentication/enable_starttls_auto/
// openssl_verify_mode settings. Sending is done with the standard library's
// net/smtp over a connection obtained from [SMTP.Dial] (net.Dial by default), so
// tests can substitute an in-process server.
type SMTP struct {
	// Address is the SMTP server host (default "localhost").
	Address string
	// Port is the SMTP server port (default 25).
	Port int
	// Domain is the argument to EHLO/HELO; when empty the client's default is
	// used and no explicit HELO is sent.
	Domain string
	// UserName / Password are the AUTH credentials.
	UserName string
	Password string
	// Authentication selects the AUTH mechanism: "plain", "login" or "cram_md5"
	// (empty means no authentication).
	Authentication string
	// EnableStartTLSAuto upgrades a plaintext connection to TLS via STARTTLS when
	// the server advertises it, mirroring :enable_starttls_auto.
	EnableStartTLSAuto bool
	// OpenSSLVerifyMode of "none" disables certificate verification (maps to
	// InsecureSkipVerify), mirroring :openssl_verify_mode => 'none'.
	OpenSSLVerifyMode string
	// SSL / TLS request an implicit-TLS (SMTPS) connection, mirroring :ssl/:tls.
	SSL bool
	TLS bool
	// Dial is the connection seam; nil means net.Dial.
	Dial Dialer
	// TLSConfig, when set, overrides the config built from OpenSSLVerifyMode.
	TLSConfig *tls.Config
}

// Deliver sends m over SMTP.
func (s *SMTP) Deliver(m *Message) error {
	env, err := buildEnvelope(m)
	if err != nil {
		return err
	}
	return s.deliverEnvelope(env)
}

func (s *SMTP) host() string {
	if s.Address == "" {
		return "localhost"
	}
	return s.Address
}

func (s *SMTP) port() int {
	if s.Port == 0 {
		return 25
	}
	return s.Port
}

func (s *SMTP) implicitTLS() bool { return s.SSL || s.TLS }

func (s *SMTP) clientTLS(host string) *tls.Config {
	if s.TLSConfig != nil {
		return s.TLSConfig
	}
	cfg := &tls.Config{ServerName: host}
	if strings.EqualFold(s.OpenSSLVerifyMode, "none") {
		cfg.InsecureSkipVerify = true
	}
	return cfg
}

func (s *SMTP) auth(host string) (smtp.Auth, error) {
	switch strings.ToLower(s.Authentication) {
	case "plain":
		return smtp.PlainAuth("", s.UserName, s.Password, host), nil
	case "login":
		return &loginAuth{username: s.UserName, password: s.Password}, nil
	case "cram_md5", "cram-md5":
		return smtp.CRAMMD5Auth(s.UserName, s.Password), nil
	default:
		return nil, fmt.Errorf("mail: unknown SMTP authentication %q", s.Authentication)
	}
}

func (s *SMTP) deliverEnvelope(env *smtpEnvelope) error {
	dial := s.Dial
	if dial == nil {
		dial = net.Dial
	}
	host := s.host()
	conn, err := dial("tcp", net.JoinHostPort(host, strconv.Itoa(s.port())))
	if err != nil {
		return err
	}
	if s.implicitTLS() {
		conn = tls.Client(conn, s.clientTLS(host))
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()

	if s.Domain != "" {
		if err := c.Hello(s.Domain); err != nil {
			return err
		}
	}
	if !s.implicitTLS() && s.EnableStartTLSAuto {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(s.clientTLS(host)); err != nil {
				return err
			}
		}
	}
	if s.Authentication != "" {
		auth, err := s.auth(host)
		if err != nil {
			return err
		}
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(env.from); err != nil {
		return err
	}
	for _, to := range env.to {
		if err := c.Rcpt(to); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(env.message)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// loginAuth implements the AUTH LOGIN mechanism (username then password, each
// base64-encoded), which the standard library omits but the gem supports.
type loginAuth struct {
	username string
	password string
}

// Start begins the LOGIN exchange with no initial response.
func (a *loginAuth) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "LOGIN", nil, nil
}

// Next answers the server's Username:/Password: challenges.
func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(strings.TrimSuffix(string(fromServer), ":"))) {
	case "username":
		return []byte(a.username), nil
	case "password":
		return []byte(a.password), nil
	default:
		return nil, fmt.Errorf("mail: unexpected LOGIN challenge %q", fromServer)
	}
}

// --- Sendmail delivery (Mail::Sendmail) ---------------------------------------

// sendmailRunner is the seam for spawning the sendmail binary; tests replace it.
var sendmailRunner = execRun

// Sendmail delivers a message by piping it to a local sendmail binary, mirroring
// `delivery_method :sendmail`. The command is built as
// `location arguments… -f <from> -- <recipients…>` and the LF-normalised message
// is written to its stdin.
type Sendmail struct {
	// Location is the sendmail binary path (default "/usr/sbin/sendmail").
	Location string
	// Arguments are the fixed arguments (default ["-i"]).
	Arguments []string
	// Run is the execution seam; nil runs the real binary.
	Run func(path string, args []string, stdin []byte) error
}

func (s *Sendmail) location() string {
	if s.Location == "" {
		return "/usr/sbin/sendmail"
	}
	return s.Location
}

func (s *Sendmail) args() []string {
	if s.Arguments == nil {
		return []string{"-i"}
	}
	return s.Arguments
}

// Deliver pipes m to the sendmail binary.
func (s *Sendmail) Deliver(m *Message) error {
	env, err := buildEnvelope(m)
	if err != nil {
		return err
	}
	args := append([]string{}, s.args()...)
	if env.from != "" {
		args = append(args, "-f", env.from)
	}
	args = append(args, "--")
	args = append(args, env.to...)

	run := s.Run
	if run == nil {
		run = sendmailRunner
	}
	return run(s.location(), args, []byte(toLF(env.message)))
}

// execRun runs path with args, feeding stdin, and returns an error on non-zero
// exit — the default [Sendmail] runner.
func execRun(path string, args []string, stdin []byte) error {
	cmd := exec.Command(path, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mail: sendmail delivery failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// toLF normalises CRLF line endings to LF, matching
// Mail::Utilities.binary_unsafe_to_lf used before piping to sendmail.
func toLF(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// --- Test delivery (Mail::TestMailer) -----------------------------------------

// TestDelivery is a no-op delivery method that records every delivered message,
// mirroring Mail::TestMailer for use in tests. It still builds (and thus
// validates) the SMTP envelope, so envelope errors surface as they would over a
// real transport.
type TestDelivery struct {
	mu         sync.Mutex
	deliveries []*Message
}

// Deliver records m after validating its envelope.
func (t *TestDelivery) Deliver(m *Message) error {
	if _, err := buildEnvelope(m); err != nil {
		return err
	}
	t.mu.Lock()
	t.deliveries = append(t.deliveries, m)
	t.mu.Unlock()
	return nil
}

// Deliveries returns a snapshot of the recorded messages, mirroring
// Mail::TestMailer.deliveries.
func (t *TestDelivery) Deliveries() []*Message {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]*Message(nil), t.deliveries...)
}

// Clear empties the recorded deliveries, mirroring TestMailer.deliveries.clear.
func (t *TestDelivery) Clear() {
	t.mu.Lock()
	t.deliveries = nil
	t.mu.Unlock()
}

// --- File delivery (Mail::FileDelivery) ---------------------------------------

// mkdirAll and fileOpener are the filesystem seams for [FileDelivery]; tests
// override them to drive the error branches deterministically.
var (
	mkdirAll   = os.MkdirAll
	fileOpener = func(path string) (io.WriteCloser, error) {
		return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	}
)

// FileDelivery writes the message to one file per unique recipient under
// Location, appending if the file exists, mirroring Mail::FileDelivery.
type FileDelivery struct {
	// Location is the output directory (default "./mails").
	Location string
	// Extension is appended to each recipient-derived filename.
	Extension string
}

func (f *FileDelivery) location() string {
	if f.Location == "" {
		return "./mails"
	}
	return f.Location
}

// Deliver writes m into Location, one file per unique recipient.
func (f *FileDelivery) Deliver(m *Message) error {
	env, err := buildEnvelope(m)
	if err != nil {
		return err
	}
	if err := mkdirAll(f.location(), 0o755); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, to := range env.to {
		if seen[to] {
			continue
		}
		seen[to] = true
		path := filepath.Join(f.location(), filepath.Base(to+f.Extension))
		if err := f.writeOne(path, env.message); err != nil {
			return err
		}
	}
	return nil
}

func (f *FileDelivery) writeOne(path, message string) error {
	fp, err := fileOpener(path)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(fp, message+"\r\n\r\n"); err != nil {
		fp.Close()
		return err
	}
	return fp.Close()
}

// --- Logger delivery (Mail::LoggerDelivery) -----------------------------------

// Logger is the sink for [LoggerDelivery]; *log.Logger satisfies it.
type Logger interface {
	Print(v ...any)
}

// defaultLogger is used when a [LoggerDelivery] has no Logger set.
var defaultLogger Logger = log.New(os.Stdout, "", log.LstdFlags)

// LoggerDelivery "delivers" a message by logging its wire form, mirroring
// Mail::LoggerDelivery. Useful for development.
type LoggerDelivery struct {
	// Logger is the destination; nil logs to stdout.
	Logger Logger
}

// Deliver logs m's envelope message.
func (l *LoggerDelivery) Deliver(m *Message) error {
	env, err := buildEnvelope(m)
	if err != nil {
		return err
	}
	lg := l.Logger
	if lg == nil {
		lg = defaultLogger
	}
	lg.Print(env.message)
	return nil
}
