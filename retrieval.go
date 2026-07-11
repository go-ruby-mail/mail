// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Retrieval — fetching messages from a mailbox — mirroring the Ruby `mail` gem's
// retriever methods (Mail.defaults { retriever_method … } + Mail.find/first/
// last/all). Two transports are implemented in pure Go: POP3 and IMAP, each
// dialing through the same injectable [Dialer] seam so an in-process fake server
// can drive the exchange in tests.
package mail

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
)

// FindOptions selects which messages a [RetrieverMethod] returns, mirroring the
// options hash of Mail.find. The zero value is completed with the gem's defaults
// (what=first, order=asc, count=10, mailbox=INBOX, keys=[ALL]).
type FindOptions struct {
	// What is "first" or "last" (default "first").
	What string
	// Order is "asc" or "desc" (default "asc").
	Order string
	// Count caps how many messages are returned (default 10); ignored when All.
	Count int
	// All returns every message, mirroring Mail.all / count => :all.
	All bool
	// DeleteAfterFind deletes each retrieved message from the server.
	DeleteAfterFind bool
	// Mailbox is the IMAP mailbox to open (default "INBOX"). Ignored by POP3.
	Mailbox string
	// Keys are the IMAP SEARCH criteria (default ["ALL"]). Ignored by POP3.
	Keys []string
	// ReadOnly opens the IMAP mailbox with EXAMINE instead of SELECT.
	ReadOnly bool
}

// withDefaults fills the zero fields of o with the gem's defaults.
func (o FindOptions) withDefaults() FindOptions {
	if o.What == "" {
		o.What = "first"
	}
	if o.Order == "" {
		o.Order = "asc"
	}
	if o.Count == 0 {
		o.Count = 10
	}
	if o.Mailbox == "" {
		o.Mailbox = "INBOX"
	}
	if len(o.Keys) == 0 {
		o.Keys = []string{"ALL"}
	}
	return o
}

// RetrieverMethod fetches messages from a mailbox, mirroring the gem's retriever
// objects. Implementations: [POP3] and [IMAP].
type RetrieverMethod interface {
	// Find returns the messages selected by opts.
	Find(opts FindOptions) ([]*Message, error)
}

func currentRetriever() (RetrieverMethod, error) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if globalRetriever == nil {
		return nil, fmt.Errorf("mail: no retriever method configured; call Defaults to set one")
	}
	return globalRetriever, nil
}

// Find returns messages from the process-wide retriever configured with
// [Defaults], mirroring Mail.find.
func Find(opts FindOptions) ([]*Message, error) {
	r, err := currentRetriever()
	if err != nil {
		return nil, err
	}
	return r.Find(opts)
}

// First returns the oldest message from the configured retriever, mirroring
// Mail.first (nil when the mailbox is empty).
func First() (*Message, error) {
	return firstOf(Find(FindOptions{What: "first", Count: 1}))
}

// Last returns the newest message from the configured retriever, mirroring
// Mail.last (nil when the mailbox is empty).
func Last() (*Message, error) {
	return firstOf(Find(FindOptions{What: "last", Count: 1}))
}

// All returns every message from the configured retriever, mirroring Mail.all.
func All() ([]*Message, error) {
	return Find(FindOptions{All: true})
}

// firstOf reduces a (messages, error) result to its first message.
func firstOf(msgs []*Message, err error) (*Message, error) {
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return nil, nil
	}
	return msgs[0], nil
}

// selectAndOrder applies the gem's what/order/count selection to an
// ascending-by-arrival list of item indices, returning the indices in the order
// the messages should be retrieved and returned.
func selectAndOrder(items []int, opts FindOptions) []int {
	out := append([]int(nil), items...)
	if opts.What == "last" {
		sort.Sort(sort.Reverse(sort.IntSlice(out)))
	}
	if !opts.All && opts.Count > 0 && opts.Count < len(out) {
		out = out[:opts.Count]
	}
	// Reverse unless the natural order already matches the request: the gem keeps
	// order for (last & desc) and (first & asc), and reverses otherwise.
	if !((opts.What == "last" && opts.Order == "desc") ||
		(opts.What == "first" && opts.Order == "asc")) {
		reverseInts(out)
	}
	return out
}

func reverseInts(s []int) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// dialConn opens a connection via the seam (net.Dial when dial is nil) and, when
// useTLS, wraps it in a TLS client — the shared connect step for POP3/IMAP.
func dialConn(dial Dialer, host string, port int, useTLS bool, cfg *tls.Config) (net.Conn, error) {
	if dial == nil {
		dial = net.Dial
	}
	conn, err := dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	if useTLS {
		conn = tls.Client(conn, cfg)
	}
	return conn, nil
}

// retrieverTLS builds the TLS config for a retriever: an explicit override, or a
// server-named config with verification disabled (matching the gem's
// enable_ssl, which uses VERIFY_NONE).
func retrieverTLS(override *tls.Config, host string) *tls.Config {
	if override != nil {
		return override
	}
	return &tls.Config{ServerName: host, InsecureSkipVerify: true}
}

// --- POP3 (Mail::POP3) --------------------------------------------------------

// POP3 retrieves messages from a POP3 mailbox, mirroring
// `retriever_method :pop3`. It speaks POP3 in pure Go over a connection from
// [POP3.Dial] (net.Dial by default).
type POP3 struct {
	// Address is the POP3 server host (default "localhost").
	Address string
	// Port is the POP3 server port (default 110).
	Port int
	// UserName / Password are the login credentials.
	UserName string
	Password string
	// EnableSSL requests an implicit-TLS (POP3S) connection.
	EnableSSL bool
	// Dial is the connection seam; nil means net.Dial.
	Dial Dialer
	// TLSConfig, when set, overrides the default (verify-none) TLS config.
	TLSConfig *tls.Config
}

func (p *POP3) host() string {
	if p.Address == "" {
		return "localhost"
	}
	return p.Address
}

func (p *POP3) port() int {
	if p.Port == 0 {
		return 110
	}
	return p.Port
}

// Find retrieves messages from the POP3 mailbox per opts.
func (p *POP3) Find(opts FindOptions) ([]*Message, error) {
	opts = opts.withDefaults()
	conn, err := dialConn(p.Dial, p.host(), p.port(), p.EnableSSL, retrieverTLS(p.TLSConfig, p.host()))
	if err != nil {
		return nil, err
	}
	c := &pop3Conn{r: bufio.NewReader(conn), conn: conn}
	defer c.conn.Close()

	if _, err := c.readStatus(); err != nil { // greeting
		return nil, err
	}
	if _, err := c.cmd("USER " + p.UserName); err != nil {
		return nil, err
	}
	if _, err := c.cmd("PASS " + p.Password); err != nil {
		return nil, err
	}
	nums, err := c.list()
	if err != nil {
		return nil, err
	}
	order := selectAndOrder(nums, opts)

	var msgs []*Message
	for _, n := range order {
		body, err := c.retr(n)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, New(body))
		if opts.DeleteAfterFind {
			if _, err := c.cmd("DELE " + strconv.Itoa(n)); err != nil {
				return nil, err
			}
		}
	}
	if _, err := c.cmd("QUIT"); err != nil {
		return nil, err
	}
	return msgs, nil
}

// First returns the oldest POP3 message.
func (p *POP3) First() (*Message, error) {
	return firstOf(p.Find(FindOptions{What: "first", Count: 1}))
}

// Last returns the newest POP3 message.
func (p *POP3) Last() (*Message, error) {
	return firstOf(p.Find(FindOptions{What: "last", Count: 1}))
}

// All returns every POP3 message.
func (p *POP3) All() ([]*Message, error) { return p.Find(FindOptions{All: true}) }

// pop3Conn is a minimal POP3 protocol connection.
type pop3Conn struct {
	r    *bufio.Reader
	conn net.Conn
}

// readStatus reads a single-line status response, returning its text (after
// "+OK ") and an error for a "-ERR" reply.
func (c *pop3Conn) readStatus() (string, error) {
	line, err := c.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if strings.HasPrefix(line, "+OK") {
		return strings.TrimSpace(strings.TrimPrefix(line, "+OK")), nil
	}
	return "", fmt.Errorf("mail: POP3 error: %s", line)
}

// cmd sends a command line and reads its single-line status.
func (c *pop3Conn) cmd(line string) (string, error) {
	if _, err := c.conn.Write([]byte(line + "\r\n")); err != nil {
		return "", err
	}
	return c.readStatus()
}

// list issues LIST and returns the message numbers in ascending order.
func (c *pop3Conn) list() ([]int, error) {
	if _, err := c.cmd("LIST"); err != nil {
		return nil, err
	}
	lines, err := c.readMultiline()
	if err != nil {
		return nil, err
	}
	var nums []int
	for _, ln := range lines {
		fields := strings.Fields(ln)
		if len(fields) == 0 {
			continue
		}
		if n, err := strconv.Atoi(fields[0]); err == nil {
			nums = append(nums, n)
		}
	}
	return nums, nil
}

// retr issues RETR n and returns the decoded message body.
func (c *pop3Conn) retr(n int) (string, error) {
	if _, err := c.cmd("RETR " + strconv.Itoa(n)); err != nil {
		return "", err
	}
	lines, err := c.readMultiline()
	if err != nil {
		return "", err
	}
	return strings.Join(lines, "\r\n"), nil
}

// readMultiline reads a dot-terminated multiline response, unstuffing leading
// dots, and returns the lines without the terminator.
func (c *pop3Conn) readMultiline() ([]string, error) {
	var lines []string
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "." {
			return lines, nil
		}
		if strings.HasPrefix(line, "..") {
			line = line[1:]
		}
		lines = append(lines, line)
	}
}

// --- IMAP (Mail::IMAP) --------------------------------------------------------

// IMAP retrieves messages from an IMAP mailbox, mirroring
// `retriever_method :imap` with mailbox select/search/fetch. It speaks IMAP in
// pure Go over a connection from [IMAP.Dial] (net.Dial by default).
type IMAP struct {
	// Address is the IMAP server host (default "localhost").
	Address string
	// Port is the IMAP server port (default 143).
	Port int
	// UserName / Password are the login credentials.
	UserName string
	Password string
	// EnableSSL requests an implicit-TLS (IMAPS) connection.
	EnableSSL bool
	// EnableStartTLS upgrades a plaintext connection via STARTTLS before login.
	EnableStartTLS bool
	// Dial is the connection seam; nil means net.Dial.
	Dial Dialer
	// TLSConfig, when set, overrides the default (verify-none) TLS config.
	TLSConfig *tls.Config
}

func (p *IMAP) host() string {
	if p.Address == "" {
		return "localhost"
	}
	return p.Address
}

func (p *IMAP) port() int {
	if p.Port == 0 {
		return 143
	}
	return p.Port
}

// Find retrieves messages from the IMAP mailbox per opts.
func (p *IMAP) Find(opts FindOptions) ([]*Message, error) {
	opts = opts.withDefaults()
	conn, err := dialConn(p.Dial, p.host(), p.port(), p.EnableSSL, retrieverTLS(p.TLSConfig, p.host()))
	if err != nil {
		return nil, err
	}
	c := &imapConn{r: bufio.NewReader(conn), conn: conn, host: p.host(), tls: retrieverTLS(p.TLSConfig, p.host())}
	defer c.conn.Close()

	if err := c.greeting(); err != nil {
		return nil, err
	}
	if p.EnableStartTLS {
		if err := c.startTLS(); err != nil {
			return nil, err
		}
	}
	if err := c.login(p.UserName, p.Password); err != nil {
		return nil, err
	}
	if err := c.selectMailbox(opts.Mailbox, opts.ReadOnly); err != nil {
		return nil, err
	}
	uids, err := c.uidSearch(opts.Keys)
	if err != nil {
		return nil, err
	}
	sort.Ints(uids)
	order := selectAndOrder(uids, opts)

	var msgs []*Message
	for _, uid := range order {
		body, err := c.uidFetchRFC822(uid)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, New(body))
		if opts.DeleteAfterFind {
			if err := c.uidStoreDeleted(uid); err != nil {
				return nil, err
			}
		}
	}
	if opts.DeleteAfterFind {
		if err := c.expunge(); err != nil {
			return nil, err
		}
	}
	if err := c.logout(); err != nil {
		return nil, err
	}
	return msgs, nil
}

// First returns the oldest IMAP message.
func (p *IMAP) First() (*Message, error) {
	return firstOf(p.Find(FindOptions{What: "first", Count: 1}))
}

// Last returns the newest IMAP message.
func (p *IMAP) Last() (*Message, error) {
	return firstOf(p.Find(FindOptions{What: "last", Count: 1}))
}

// All returns every IMAP message.
func (p *IMAP) All() ([]*Message, error) { return p.Find(FindOptions{All: true}) }

// imapConn is a minimal tagged-IMAP protocol connection.
type imapConn struct {
	r    *bufio.Reader
	conn net.Conn
	host string
	tls  *tls.Config
	seq  int
}

func (c *imapConn) nextTag() string {
	c.seq++
	return "a" + strconv.Itoa(c.seq)
}

// greeting reads the server's untagged greeting line.
func (c *imapConn) greeting() error {
	line, err := c.r.ReadString('\n')
	if err != nil {
		return err
	}
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "* OK") {
		return fmt.Errorf("mail: IMAP greeting: %s", line)
	}
	return nil
}

// send writes a tagged command line.
func (c *imapConn) send(line string) error {
	_, err := c.conn.Write([]byte(line + "\r\n"))
	return err
}

// command runs a tagged command and returns the untagged response lines,
// erroring on a NO/BAD completion.
func (c *imapConn) command(cmd string) ([]string, error) {
	tag := c.nextTag()
	if err := c.send(tag + " " + cmd); err != nil {
		return nil, err
	}
	var untagged []string
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, tag+" ") {
			return untagged, statusError(strings.TrimPrefix(line, tag+" "))
		}
		untagged = append(untagged, line)
	}
}

// statusError turns a tagged completion ("OK …"/"NO …"/"BAD …") into an error
// for a non-OK result.
func statusError(rest string) error {
	if strings.HasPrefix(rest, "OK") {
		return nil
	}
	return fmt.Errorf("mail: IMAP command failed: %s", rest)
}

func (c *imapConn) startTLS() error {
	if _, err := c.command("STARTTLS"); err != nil {
		return err
	}
	tc := tls.Client(c.conn, c.tls)
	c.conn = tc
	c.r = bufio.NewReader(tc)
	return nil
}

func (c *imapConn) login(user, pass string) error {
	_, err := c.command(fmt.Sprintf("LOGIN %s %s", quoteIMAP(user), quoteIMAP(pass)))
	return err
}

func (c *imapConn) selectMailbox(mailbox string, readOnly bool) error {
	verb := "SELECT"
	if readOnly {
		verb = "EXAMINE"
	}
	_, err := c.command(fmt.Sprintf("%s %s", verb, quoteIMAP(mailbox)))
	return err
}

// uidSearch issues UID SEARCH and parses the returned UID list.
func (c *imapConn) uidSearch(keys []string) ([]int, error) {
	untagged, err := c.command("UID SEARCH " + strings.Join(keys, " "))
	if err != nil {
		return nil, err
	}
	var uids []int
	for _, ln := range untagged {
		if !strings.HasPrefix(ln, "* SEARCH") {
			continue
		}
		for _, tok := range strings.Fields(strings.TrimPrefix(ln, "* SEARCH")) {
			if n, err := strconv.Atoi(tok); err == nil {
				uids = append(uids, n)
			}
		}
	}
	return uids, nil
}

// uidFetchRFC822 fetches a message body by UID, reading the RFC822 literal.
func (c *imapConn) uidFetchRFC822(uid int) (string, error) {
	tag := c.nextTag()
	if err := c.send(fmt.Sprintf("%s UID FETCH %d (RFC822)", tag, uid)); err != nil {
		return "", err
	}
	var body string
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return "", err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if n, ok := literalLength(trimmed); ok {
			buf := make([]byte, n)
			if _, err := io.ReadFull(c.r, buf); err != nil {
				return "", err
			}
			body = string(buf)
			continue
		}
		if strings.HasPrefix(trimmed, tag+" ") {
			return body, statusError(strings.TrimPrefix(trimmed, tag+" "))
		}
	}
}

func (c *imapConn) uidStoreDeleted(uid int) error {
	_, err := c.command(fmt.Sprintf("UID STORE %d +FLAGS (\\Deleted)", uid))
	return err
}

func (c *imapConn) expunge() error {
	_, err := c.command("EXPUNGE")
	return err
}

func (c *imapConn) logout() error {
	_, err := c.command("LOGOUT")
	return err
}

// literalLength extracts the byte count from a response line ending in "{N}".
func literalLength(line string) (int, bool) {
	if !strings.HasSuffix(line, "}") {
		return 0, false
	}
	open := strings.LastIndexByte(line, '{')
	if open < 0 {
		return 0, false
	}
	n, err := strconv.Atoi(line[open+1 : len(line)-1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// quoteIMAP wraps an IMAP astring in double quotes, escaping quotes and
// backslashes, so mailbox names / credentials with spaces are transmitted safely.
func quoteIMAP(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		if s[i] == '"' || s[i] == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	b.WriteByte('"')
	return b.String()
}
