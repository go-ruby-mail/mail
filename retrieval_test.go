// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mail

import (
	"errors"
	"strings"
	"testing"
)

// msgN builds a tiny message whose Subject encodes n, for ordering assertions.
func msgN(n string) string {
	return "From: s@x.com\r\nSubject: msg-" + n + "\r\n\r\nbody " + n + "\r\n"
}

func subjectsOf(msgs []*Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Subject()
	}
	return out
}

// --- POP3 ---------------------------------------------------------------------

func popServer(fail map[string]string) *fakePOP3 {
	return &fakePOP3{
		messages: []string{msgN("1"), msgN("2"), msgN("3")},
		fail:     fail,
	}
}

func TestPOP3FindFirstAsc(t *testing.T) {
	srv := popServer(nil)
	p := &POP3{Address: "127.0.0.1", UserName: "u", Password: "p", Dial: tcpDialer(t, srv.serve)}
	msgs, err := p.Find(FindOptions{What: "first", Order: "asc", Count: 2})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got := strings.Join(subjectsOf(msgs), ","); got != "msg-1,msg-2" {
		t.Errorf("first asc = %s", got)
	}
}

func TestPOP3FindFirstDesc(t *testing.T) {
	srv := popServer(nil)
	p := &POP3{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	msgs, err := p.Find(FindOptions{What: "first", Order: "desc", Count: 2})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got := strings.Join(subjectsOf(msgs), ","); got != "msg-2,msg-1" {
		t.Errorf("first desc = %s", got)
	}
}

func TestPOP3FindLastAsc(t *testing.T) {
	srv := popServer(nil)
	p := &POP3{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	msgs, err := p.Find(FindOptions{What: "last", Order: "asc", Count: 2})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	// last two by arrival (2,3), ascending order -> 2,3.
	if got := strings.Join(subjectsOf(msgs), ","); got != "msg-2,msg-3" {
		t.Errorf("last asc = %s", got)
	}
}

func TestPOP3FindLastDesc(t *testing.T) {
	srv := popServer(nil)
	p := &POP3{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	msgs, err := p.Find(FindOptions{What: "last", Order: "desc", Count: 2})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got := strings.Join(subjectsOf(msgs), ","); got != "msg-3,msg-2" {
		t.Errorf("last desc = %s", got)
	}
}

func TestPOP3AllFirstLast(t *testing.T) {
	srv := popServer(nil)
	p := &POP3{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	all, err := p.All()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all = %d", len(all))
	}
	first, err := p.First()
	if err != nil || first.Subject() != "msg-1" {
		t.Errorf("first = %v %v", first, err)
	}
	last, err := p.Last()
	if err != nil || last.Subject() != "msg-3" {
		t.Errorf("last = %v %v", last, err)
	}
}

func TestPOP3DeleteAfterFind(t *testing.T) {
	srv := popServer(nil)
	p := &POP3{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	if _, err := p.Find(FindOptions{All: true, DeleteAfterFind: true}); err != nil {
		t.Fatalf("find: %v", err)
	}
	if got := srv.deletedNums(); len(got) != 3 {
		t.Errorf("deleted = %v", got)
	}
}

func TestPOP3EmptyMailbox(t *testing.T) {
	srv := &fakePOP3{}
	p := &POP3{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	first, err := p.First()
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if first != nil {
		t.Errorf("expected nil for empty mailbox, got %v", first)
	}
}

func TestPOP3ImplicitTLS(t *testing.T) {
	scfg, ccfg := testTLS(t)
	srv := &fakePOP3{messages: []string{msgN("1")}, implicit: true, tlsCfg: scfg}
	p := &POP3{Address: "127.0.0.1", EnableSSL: true, TLSConfig: ccfg, Dial: tcpDialer(t, srv.serve)}
	msgs, err := p.All()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("msgs = %d", len(msgs))
	}
}

func TestPOP3Defaults(t *testing.T) {
	p := &POP3{}
	if p.host() != "localhost" || p.port() != 110 {
		t.Errorf("defaults host=%q port=%d", p.host(), p.port())
	}
}

func TestPOP3ErrorPaths(t *testing.T) {
	cases := []struct {
		name string
		fail map[string]string
		dial Dialer
	}{
		{"dial", nil, errDialer(errors.New("no route"))},
		{"greeting", map[string]string{"GREET": "-ERR closed"}, nil},
		{"user", map[string]string{"USER": "-ERR no user"}, nil},
		{"pass", map[string]string{"PASS": "-ERR bad pass"}, nil},
		{"list", map[string]string{"LIST": "-ERR cannot list"}, nil},
		{"retr", map[string]string{"RETR": "-ERR cannot retr"}, nil},
		{"dele", map[string]string{"DELE": "-ERR cannot dele"}, nil},
		{"quit", map[string]string{"QUIT": "-ERR cannot quit"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &POP3{Address: "127.0.0.1"}
			if c.dial != nil {
				p.Dial = c.dial
			} else {
				p.Dial = tcpDialer(t, popServer(c.fail).serve)
			}
			_, err := p.Find(FindOptions{All: true, DeleteAfterFind: true})
			if err == nil {
				t.Fatalf("%s: expected error", c.name)
			}
		})
	}
}

func TestPOP3DefaultDialErrorToClosedPort(t *testing.T) {
	p := &POP3{Address: "127.0.0.1", Port: 1}
	if _, err := p.All(); err == nil {
		t.Fatal("expected connection error")
	}
}

func TestPOP3ReadErrors(t *testing.T) {
	for _, hangup := range []string{"greet", "list", "retr"} {
		t.Run(hangup, func(t *testing.T) {
			srv := popServer(nil)
			srv.hangup = hangup
			p := &POP3{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
			if _, err := p.All(); err == nil {
				t.Fatalf("hangup %s: expected read error", hangup)
			}
		})
	}
}

func TestPOP3WriteError(t *testing.T) {
	// Greeting reads fine; the first command write fails.
	p := &POP3{Address: "127.0.0.1", Dial: writeFailDialer(t, popServer(nil).serve, 0)}
	if _, err := p.All(); err == nil {
		t.Fatal("expected write error")
	}
}

func TestPOP3ListBlankLineSkipped(t *testing.T) {
	srv := popServer(nil)
	srv.listBlank = true
	p := &POP3{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	msgs, err := p.All()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("blank LIST line broke parse: got %d", len(msgs))
	}
}

func TestPOP3DotUnstuffing(t *testing.T) {
	// A body line starting with "." is dot-stuffed by the server and must be
	// unstuffed by the client.
	body := "From: s@x.com\r\nSubject: dots\r\n\r\n.leading dot line\r\nnormal\r\n"
	srv := &fakePOP3{messages: []string{body}}
	p := &POP3{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	msgs, err := p.All()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if got := msgs[0].Body().DecodedString(); !strings.Contains(got, ".leading dot line") {
		t.Errorf("dot line not unstuffed: %q", got)
	}
}

// --- IMAP ---------------------------------------------------------------------

func imapServer(fail map[string]string) *fakeIMAP {
	return &fakeIMAP{
		uidBodies: map[int]string{101: msgN("1"), 102: msgN("2"), 103: msgN("3")},
		fail:      fail,
	}
}

func TestIMAPFindFirstAsc(t *testing.T) {
	srv := imapServer(nil)
	p := &IMAP{Address: "127.0.0.1", UserName: "u", Password: "p", Dial: tcpDialer(t, srv.serve)}
	msgs, err := p.Find(FindOptions{What: "first", Order: "asc", Count: 2})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got := strings.Join(subjectsOf(msgs), ","); got != "msg-1,msg-2" {
		t.Errorf("first asc = %s", got)
	}
}

func TestIMAPFindFirstDesc(t *testing.T) {
	srv := imapServer(nil)
	p := &IMAP{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	msgs, err := p.Find(FindOptions{What: "first", Order: "desc", Count: 2})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got := strings.Join(subjectsOf(msgs), ","); got != "msg-2,msg-1" {
		t.Errorf("first desc = %s", got)
	}
}

func TestIMAPFindLastAsc(t *testing.T) {
	srv := imapServer(nil)
	p := &IMAP{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	msgs, err := p.Find(FindOptions{What: "last", Order: "asc", Count: 2})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got := strings.Join(subjectsOf(msgs), ","); got != "msg-2,msg-3" {
		t.Errorf("last asc = %s", got)
	}
}

func TestIMAPFindLastDesc(t *testing.T) {
	srv := imapServer(nil)
	p := &IMAP{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	msgs, err := p.Find(FindOptions{What: "last", Order: "desc", Count: 2})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got := strings.Join(subjectsOf(msgs), ","); got != "msg-3,msg-2" {
		t.Errorf("last desc = %s", got)
	}
}

func TestIMAPAllFirstLastReadOnly(t *testing.T) {
	srv := imapServer(nil)
	p := &IMAP{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	all, err := p.All()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all = %d", len(all))
	}
	first, err := p.First()
	if err != nil || first.Subject() != "msg-1" {
		t.Errorf("first = %v %v", first, err)
	}
	last, err := p.Last()
	if err != nil || last.Subject() != "msg-3" {
		t.Errorf("last = %v %v", last, err)
	}
	// EXAMINE path (read-only).
	ro, err := p.Find(FindOptions{ReadOnly: true, All: true})
	if err != nil || len(ro) != 3 {
		t.Errorf("read-only find = %d %v", len(ro), err)
	}
}

func TestIMAPDeleteAfterFind(t *testing.T) {
	srv := imapServer(nil)
	p := &IMAP{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	if _, err := p.Find(FindOptions{All: true, DeleteAfterFind: true}); err != nil {
		t.Fatalf("find: %v", err)
	}
	if got := srv.storedUIDs(); len(got) != 3 {
		t.Errorf("stored deleted = %v", got)
	}
	if !srv.expunged {
		t.Error("expected EXPUNGE")
	}
}

func TestIMAPEmptySearch(t *testing.T) {
	srv := &fakeIMAP{uidBodies: map[int]string{}, searchUIDs: []int{}}
	p := &IMAP{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	first, err := p.First()
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if first != nil {
		t.Errorf("expected nil for empty search, got %v", first)
	}
}

func TestIMAPImplicitTLS(t *testing.T) {
	scfg, ccfg := testTLS(t)
	srv := &fakeIMAP{uidBodies: map[int]string{101: msgN("1")}, implicit: true, tlsCfg: scfg}
	p := &IMAP{Address: "127.0.0.1", EnableSSL: true, TLSConfig: ccfg, Dial: tcpDialer(t, srv.serve)}
	msgs, err := p.All()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("msgs = %d", len(msgs))
	}
}

func TestIMAPStartTLS(t *testing.T) {
	scfg, ccfg := testTLS(t)
	srv := &fakeIMAP{uidBodies: map[int]string{101: msgN("1")}, starttls: true, tlsCfg: scfg}
	p := &IMAP{Address: "127.0.0.1", EnableStartTLS: true, TLSConfig: ccfg, Dial: tcpDialer(t, srv.serve)}
	msgs, err := p.All()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("msgs = %d", len(msgs))
	}
}

func TestIMAPDefaults(t *testing.T) {
	p := &IMAP{}
	if p.host() != "localhost" || p.port() != 143 {
		t.Errorf("defaults host=%q port=%d", p.host(), p.port())
	}
}

func TestIMAPErrorPaths(t *testing.T) {
	cases := []struct {
		name string
		fail map[string]string
		dial Dialer
	}{
		{"dial", nil, errDialer(errors.New("no route"))},
		{"greeting", map[string]string{"GREET": "* BYE closed"}, nil},
		{"login", map[string]string{"LOGIN": "NO auth failed"}, nil},
		{"select", map[string]string{"SELECT": "NO no mailbox"}, nil},
		{"search", map[string]string{"SEARCH": "BAD bad search"}, nil},
		{"fetch", map[string]string{"FETCH": "NO cannot fetch"}, nil},
		{"store", map[string]string{"STORE": "NO cannot store"}, nil},
		{"expunge", map[string]string{"EXPUNGE": "NO cannot expunge"}, nil},
		{"logout", map[string]string{"LOGOUT": "BAD cannot logout"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &IMAP{Address: "127.0.0.1"}
			if c.dial != nil {
				p.Dial = c.dial
			} else {
				p.Dial = tcpDialer(t, imapServer(c.fail).serve)
			}
			_, err := p.Find(FindOptions{All: true, DeleteAfterFind: true})
			if err == nil {
				t.Fatalf("%s: expected error", c.name)
			}
		})
	}
}

func TestIMAPStartTLSCommandRejected(t *testing.T) {
	// Server does not support STARTTLS -> NO -> command error.
	srv := &fakeIMAP{uidBodies: map[int]string{101: msgN("1")}, starttls: false}
	p := &IMAP{Address: "127.0.0.1", EnableStartTLS: true, Dial: tcpDialer(t, srv.serve)}
	if _, err := p.All(); err == nil {
		t.Fatal("expected STARTTLS rejection")
	}
}

func TestIMAPFetchNoSuchMessage(t *testing.T) {
	// SEARCH returns a UID with no stored body -> FETCH returns NO.
	srv := &fakeIMAP{uidBodies: map[int]string{}, searchUIDs: []int{999}}
	p := &IMAP{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
	if _, err := p.All(); err == nil {
		t.Fatal("expected fetch error")
	}
}

func TestIMAPDefaultDialErrorToClosedPort(t *testing.T) {
	p := &IMAP{Address: "127.0.0.1", Port: 1}
	if _, err := p.All(); err == nil {
		t.Fatal("expected connection error")
	}
}

func TestIMAPReadErrors(t *testing.T) {
	for _, hangup := range []string{"greet", "select", "fetch-nolit", "fetch-short"} {
		t.Run(hangup, func(t *testing.T) {
			srv := imapServer(nil)
			srv.hangup = hangup
			p := &IMAP{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)}
			if _, err := p.All(); err == nil {
				t.Fatalf("hangup %s: expected read error", hangup)
			}
		})
	}
}

func TestIMAPWriteError(t *testing.T) {
	// Greeting reads fine; the LOGIN command write fails (covers command's send).
	p := &IMAP{Address: "127.0.0.1", Dial: writeFailDialer(t, imapServer(nil).serve, 0)}
	if _, err := p.All(); err == nil {
		t.Fatal("expected write error")
	}
}

func TestIMAPFetchWriteError(t *testing.T) {
	// LOGIN, SELECT and SEARCH writes succeed; the UID FETCH write fails,
	// covering uidFetchRFC822's own send-error branch.
	p := &IMAP{Address: "127.0.0.1", Dial: writeFailDialer(t, imapServer(nil).serve, 3)}
	if _, err := p.All(); err == nil {
		t.Fatal("expected fetch write error")
	}
}

// --- literalLength / quoteIMAP ------------------------------------------------

func TestLiteralLength(t *testing.T) {
	if n, ok := literalLength("* 1 FETCH (RFC822 {42}"); !ok || n != 42 {
		t.Errorf("valid literal = %d %v", n, ok)
	}
	if _, ok := literalLength("no brace here"); ok {
		t.Error("missing brace should not parse")
	}
	if _, ok := literalLength("trailing }"); ok {
		t.Error("no open brace should not parse")
	}
	if _, ok := literalLength("{notanumber}"); ok {
		t.Error("non-numeric literal should not parse")
	}
}

func TestQuoteIMAP(t *testing.T) {
	if got := quoteIMAP(`a"b\c`); got != `"a\"b\\c"` {
		t.Errorf("quoteIMAP = %q", got)
	}
}

// --- package Find/First/Last/All + retriever config ---------------------------

func TestPackageRetrievers(t *testing.T) {
	resetDefaults(t)
	srv := popServer(nil)
	Defaults(func(c *Config) {
		c.SetRetrieverMethod(&POP3{Address: "127.0.0.1", Dial: tcpDialer(t, srv.serve)})
	})
	all, err := All()
	if err != nil || len(all) != 3 {
		t.Fatalf("All = %d %v", len(all), err)
	}
	first, err := First()
	if err != nil || first.Subject() != "msg-1" {
		t.Errorf("First = %v %v", first, err)
	}
	last, err := Last()
	if err != nil || last.Subject() != "msg-3" {
		t.Errorf("Last = %v %v", last, err)
	}
	msgs, err := Find(FindOptions{What: "first", Count: 1})
	if err != nil || len(msgs) != 1 {
		t.Errorf("Find = %d %v", len(msgs), err)
	}
}

func TestPackageRetrieversNoConfig(t *testing.T) {
	resetDefaults(t)
	if _, err := Find(FindOptions{}); err == nil {
		t.Error("Find without retriever should error")
	}
	if _, err := First(); err == nil {
		t.Error("First without retriever should error")
	}
	if _, err := Last(); err == nil {
		t.Error("Last without retriever should error")
	}
	if _, err := All(); err == nil {
		t.Error("All without retriever should error")
	}
}
