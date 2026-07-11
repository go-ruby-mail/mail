// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

// In-process fake SMTP / POP3 / IMAP servers driving the delivery and retrieval
// tests. Each server speaks just enough of its protocol to exercise the client,
// runs on the loopback-TCP listener behind [tcpDialer] (the injected Dial seam),
// and records what it received. Knobs let a test inject an error reply or
// truncate a response at any protocol step so the client's error branches are
// covered — all without an external network or a real mail server.

package mail

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// tcpDialer returns a [Dialer] backed by an in-process loopback TCP listener that
// runs serve for every accepted connection. Using a real (kernel-buffered)
// socket rather than net.Pipe lets the in-process fake servers complete TLS
// handshakes, while the injected Dialer seam is still what the client dials
// through — no external network or mail server is involved.
func tcpDialer(t *testing.T, serve func(net.Conn)) Dialer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serve(conn)
		}
	}()
	addr := ln.Addr().String()
	return func(network, address string) (net.Conn, error) {
		return net.Dial("tcp", addr)
	}
}

// errDialer returns a Dialer that always fails, for dial-error tests.
func errDialer(err error) Dialer {
	return func(network, address string) (net.Conn, error) { return nil, err }
}

// writeFailConn wraps a net.Conn so that, after `after` successful writes, every
// further Write fails — reads (e.g. the server greeting and earlier command
// replies) still work, so the client reaches a specific command's write and its
// write-error branch fires.
type writeFailConn struct {
	net.Conn
	after int
	count *int
}

func (c writeFailConn) Write(p []byte) (int, error) {
	if *c.count >= c.after {
		return 0, errFault("write fault")
	}
	*c.count++
	return c.Conn.Write(p)
}

type errFault string

func (e errFault) Error() string { return string(e) }

// writeFailDialer wraps tcpDialer so connections fail all writes after `after`
// successful ones.
func writeFailDialer(t *testing.T, serve func(net.Conn), after int) Dialer {
	base := tcpDialer(t, serve)
	return func(network, address string) (net.Conn, error) {
		conn, err := base(network, address)
		if err != nil {
			return nil, err
		}
		count := 0
		return writeFailConn{Conn: conn, after: after, count: &count}, nil
	}
}

// testTLS builds a self-signed cert and returns a server TLS config plus a
// verify-skipping client TLS config for the in-process TLS tests.
func testTLS(t *testing.T) (server, client *tls.Config) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}
	return &tls.Config{Certificates: []tls.Certificate{cert}},
		&tls.Config{InsecureSkipVerify: true}
}

func firstWord(s string) string {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i]
	}
	return s
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func extractAngle(s string) string {
	i := strings.IndexByte(s, '<')
	j := strings.IndexByte(s, '>')
	if i >= 0 && j > i {
		return s[i+1 : j]
	}
	return ""
}

// --- Fake SMTP server ---------------------------------------------------------

type fakeSMTP struct {
	greet         string            // greeting line (default "220 fake ESMTP")
	ext           []string          // EHLO extensions to advertise
	tlsCfg        *tls.Config       // for STARTTLS / implicit TLS
	implicit      bool              // wrap the connection in TLS immediately
	fail          map[string]string // verb -> error reply ("GREET" fails the greeting)
	dotReply      string            // reply to the end-of-data dot (default 250)
	quitFail      bool              // reply 500 to QUIT
	closeAfter354 bool              // close right after 354 (write-error test)

	mu   sync.Mutex
	from string
	rcpt []string
	body string
}

func (f *fakeSMTP) recorded() (string, []string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.from, append([]string(nil), f.rcpt...), f.body
}

func (f *fakeSMTP) serve(conn net.Conn) {
	defer conn.Close()
	if f.implicit {
		tc := tls.Server(conn, f.tlsCfg)
		if tc.Handshake() != nil {
			return
		}
		conn = tc
	}
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	reply := func(s string) { w.WriteString(s + "\r\n"); w.Flush() }

	if g := f.fail["GREET"]; g != "" {
		reply(g)
		return
	}
	reply(orDefault(f.greet, "220 fake ESMTP"))

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		switch strings.ToUpper(firstWord(line)) {
		case "EHLO", "HELO":
			if e := f.fail["EHLO"]; e != "" {
				reply(e)
				continue
			}
			lines := append([]string{"fake at your service"}, f.ext...)
			for i, l := range lines {
				if i == len(lines)-1 {
					reply("250 " + l)
				} else {
					reply("250-" + l)
				}
			}
		case "STARTTLS":
			if e := f.fail["STARTTLS"]; e != "" {
				reply(e)
				continue
			}
			reply("220 go ahead")
			tc := tls.Server(conn, f.tlsCfg)
			if tc.Handshake() != nil {
				return
			}
			conn = tc
			r = bufio.NewReader(conn)
			w = bufio.NewWriter(conn)
			reply = func(s string) { w.WriteString(s + "\r\n"); w.Flush() }
		case "AUTH":
			f.handleAuth(line, r, reply)
		case "MAIL":
			if e := f.fail["MAIL"]; e != "" {
				reply(e)
				continue
			}
			f.mu.Lock()
			f.from = extractAngle(line)
			f.mu.Unlock()
			reply("250 2.1.0 Ok")
		case "RCPT":
			if e := f.fail["RCPT"]; e != "" {
				reply(e)
				continue
			}
			f.mu.Lock()
			f.rcpt = append(f.rcpt, extractAngle(line))
			f.mu.Unlock()
			reply("250 2.1.5 Ok")
		case "DATA":
			if e := f.fail["DATA"]; e != "" {
				reply(e)
				continue
			}
			reply("354 End data with <CR><LF>.<CR><LF>")
			if f.closeAfter354 {
				return
			}
			f.mu.Lock()
			f.body = readDotData(r)
			f.mu.Unlock()
			reply(orDefault(f.dotReply, "250 2.0.0 Ok: queued"))
		case "QUIT":
			if f.quitFail {
				reply("500 nope")
				continue
			}
			reply("221 2.0.0 Bye")
			return
		default:
			reply("250 2.0.0 Ok")
		}
	}
}

func (f *fakeSMTP) handleAuth(line string, r *bufio.Reader, reply func(string)) {
	if e := f.fail["AUTH"]; e != "" {
		reply(e)
		return
	}
	parts := strings.Fields(line)
	mech := ""
	if len(parts) >= 2 {
		mech = strings.ToUpper(parts[1])
	}
	switch mech {
	case "PLAIN":
		if len(parts) >= 3 {
			reply("235 2.7.0 Authentication successful")
		} else {
			reply("334 ")
			r.ReadString('\n')
			reply("235 2.7.0 Authentication successful")
		}
	case "LOGIN":
		reply("334 " + base64.StdEncoding.EncodeToString([]byte("Username:")))
		r.ReadString('\n')
		reply("334 " + base64.StdEncoding.EncodeToString([]byte("Password:")))
		r.ReadString('\n')
		reply("235 2.7.0 Authentication successful")
	case "CRAM-MD5":
		reply("334 " + base64.StdEncoding.EncodeToString([]byte("<12345@fake>")))
		r.ReadString('\n')
		reply("235 2.7.0 Authentication successful")
	default:
		reply("504 5.7.4 Unrecognized authentication type")
	}
}

// readDotData reads a dot-terminated DATA payload, unstuffing leading dots.
func readDotData(r *bufio.Reader) string {
	var b strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return b.String()
		}
		t := strings.TrimRight(line, "\r\n")
		if t == "." {
			return b.String()
		}
		if strings.HasPrefix(t, "..") {
			t = t[1:]
		}
		b.WriteString(t)
		b.WriteString("\r\n")
	}
}

// --- Fake POP3 server ---------------------------------------------------------

type fakePOP3 struct {
	messages  []string          // message bodies, in mailbox order (1-based numbers)
	tlsCfg    *tls.Config       // for implicit TLS
	implicit  bool              // wrap the connection in TLS immediately
	fail      map[string]string // verb -> error reply ("GREET" fails the greeting)
	hangup    string            // truncation point: "greet" | "list" | "retr"
	listBlank bool              // emit a stray blank line in the LIST response

	mu      sync.Mutex
	deleted []int
}

func (f *fakePOP3) deletedNums() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.deleted...)
}

func (f *fakePOP3) serve(conn net.Conn) {
	defer conn.Close()
	if f.implicit {
		tc := tls.Server(conn, f.tlsCfg)
		if tc.Handshake() != nil {
			return
		}
		conn = tc
	}
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	reply := func(s string) { w.WriteString(s + "\r\n"); w.Flush() }
	replyMulti := func(status string, lines []string) {
		reply(status)
		for _, l := range lines {
			if strings.HasPrefix(l, ".") {
				l = "." + l
			}
			reply(l)
		}
		reply(".")
	}

	if g := f.fail["GREET"]; g != "" {
		reply(g)
		return
	}
	if f.hangup == "greet" {
		return
	}
	reply("+OK fake POP3 ready")

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb := strings.ToUpper(firstWord(line))
		if e := f.fail[verb]; e != "" {
			reply(e)
			continue
		}
		switch verb {
		case "USER", "PASS", "RSET", "NOOP":
			reply("+OK")
		case "LIST":
			if f.hangup == "list" { // status only, then drop -> multiline read error
				reply("+OK " + itoa(len(f.messages)) + " messages")
				return
			}
			var lines []string
			for i, m := range f.messages {
				lines = append(lines, itoa(i+1)+" "+itoa(len(m)))
			}
			if f.listBlank {
				lines = append(lines, "")
			}
			replyMulti("+OK "+itoa(len(f.messages))+" messages", lines)
		case "RETR":
			n := atoiField(line)
			if n < 1 || n > len(f.messages) {
				reply("-ERR no such message")
				continue
			}
			if f.hangup == "retr" { // status only, then drop -> multiline read error
				reply("+OK message follows")
				return
			}
			replyMulti("+OK message follows", strings.Split(f.messages[n-1], "\r\n"))
		case "DELE":
			n := atoiField(line)
			f.mu.Lock()
			f.deleted = append(f.deleted, n)
			f.mu.Unlock()
			reply("+OK marked deleted")
		case "QUIT":
			reply("+OK bye")
			return
		default:
			reply("-ERR unknown command")
		}
	}
}

// --- Fake IMAP server ---------------------------------------------------------

type fakeIMAP struct {
	// uidBodies maps UID -> message body. searchUIDs is the UID list returned by
	// SEARCH (defaults to the sorted keys of uidBodies).
	uidBodies  map[int]string
	searchUIDs []int
	tlsCfg     *tls.Config
	implicit   bool
	starttls   bool              // honour STARTTLS
	fail       map[string]string // command verb -> tagged error ("GREET" fails greeting)
	hangup     string            // truncation point: "greet"|"select"|"fetch-nolit"|"fetch-short"

	mu       sync.Mutex
	stored   []int // UIDs marked deleted
	expunged bool
}

func (f *fakeIMAP) storedUIDs() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.stored...)
}

func (f *fakeIMAP) serve(conn net.Conn) {
	defer conn.Close()
	if f.implicit {
		tc := tls.Server(conn, f.tlsCfg)
		if tc.Handshake() != nil {
			return
		}
		conn = tc
	}
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	send := func(s string) { w.WriteString(s + "\r\n"); w.Flush() }

	if g := f.fail["GREET"]; g != "" {
		send(g)
		return
	}
	if f.hangup == "greet" {
		return
	}
	send("* OK fake IMAP ready")

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		tag := fields[0]
		verb := strings.ToUpper(fields[1])
		if verb == "UID" && len(fields) >= 3 {
			verb = strings.ToUpper(fields[2])
		}
		if e := f.fail[verb]; e != "" {
			send(tag + " " + e)
			continue
		}
		switch verb {
		case "STARTTLS":
			if !f.starttls {
				send(tag + " NO STARTTLS not available")
				continue
			}
			send(tag + " OK begin TLS")
			tc := tls.Server(conn, f.tlsCfg)
			if tc.Handshake() != nil {
				return
			}
			conn = tc
			r = bufio.NewReader(conn)
			w = bufio.NewWriter(conn)
			send = func(s string) { w.WriteString(s + "\r\n"); w.Flush() }
		case "LOGIN":
			send(tag + " OK LOGIN completed")
		case "SELECT", "EXAMINE":
			send("* 3 EXISTS")
			if f.hangup == "select" { // untagged only, then drop -> response read error
				return
			}
			send(tag + " OK [READ-WRITE] SELECT completed")
		case "SEARCH":
			uids := f.searchUIDs
			if uids == nil {
				uids = sortedKeys(f.uidBodies)
			}
			send("* 3 RECENT") // an unrelated untagged line the client must skip
			parts := []string{"* SEARCH"}
			for _, u := range uids {
				parts = append(parts, itoa(u))
			}
			send(strings.Join(parts, " "))
			send(tag + " OK SEARCH completed")
		case "FETCH":
			uid := lastIntField(fields)
			if f.hangup == "fetch-nolit" { // untagged non-literal line, then drop
				send("* 1 FETCH (UID " + itoa(uid) + " FLAGS (\\Seen))")
				return
			}
			if f.hangup == "fetch-short" { // literal header promises more than we send
				send("* 1 FETCH (UID " + itoa(uid) + " RFC822 {1000}")
				w.WriteString("short")
				w.Flush()
				return
			}
			body, ok := f.uidBodies[uid]
			if !ok {
				send(tag + " NO no such message")
				continue
			}
			send("* 1 FETCH (UID " + itoa(uid) + " RFC822 {" + itoa(len(body)) + "}")
			w.WriteString(body)
			send(")")
			send(tag + " OK FETCH completed")
		case "STORE":
			uid := fetchStoreUID(fields)
			f.mu.Lock()
			f.stored = append(f.stored, uid)
			f.mu.Unlock()
			send("* 1 FETCH (FLAGS (\\Deleted))")
			send(tag + " OK STORE completed")
		case "EXPUNGE":
			f.mu.Lock()
			f.expunged = true
			f.mu.Unlock()
			send(tag + " OK EXPUNGE completed")
		case "LOGOUT":
			send("* BYE logging out")
			send(tag + " OK LOGOUT completed")
			return
		default:
			send(tag + " BAD unknown command")
		}
	}
}

// fetchStoreUID pulls the UID out of "UID STORE <uid> +FLAGS (...)".
func fetchStoreUID(fields []string) int {
	if len(fields) >= 4 {
		n, _ := strconv.Atoi(fields[3])
		return n
	}
	return 0
}

// lastIntField pulls the UID out of "UID FETCH <uid> (RFC822)".
func lastIntField(fields []string) int {
	if len(fields) >= 4 {
		n, _ := strconv.Atoi(fields[3])
		return n
	}
	return 0
}

func itoa(n int) string { return strconv.Itoa(n) }

func atoiField(line string) int {
	f := strings.Fields(line)
	if len(f) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(f[1])
	return n
}

func sortedKeys(m map[int]string) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}
