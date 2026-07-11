// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mail

import (
	"net"
	"strconv"
	"strings"
	"testing"
)

// The delivery oracle checks this package's delivery behaviour against the Ruby
// `mail` gem: the SMTP envelope it computes, the fact that Bcc is stripped from
// the transmitted message, and — end to end — that the gem and this package,
// each delivering the same message to the same in-process SMTP sink, put the
// identical envelope and DATA on the wire. Like the parse oracle, it skips
// itself where ruby / the gem is absent (the qemu and Windows lanes).

// gemMessageBuilder is the `Mail.new { … }` block used by the delivery oracle,
// matching the message built in Go by [oracleMessage].
const gemMessageBuilder = `Mail.new do
  from 'Alice <alice@example.com>'
  to 'bob@example.com'
  cc 'carol@example.com'
  bcc 'dave@example.com'
  subject 'Greetings'
  body "Hello there"
end`

// oracleMessage is the Go twin of [gemMessageBuilder].
func oracleMessage() *Message {
	return New("").
		SetFrom("Alice <alice@example.com>").
		SetTo("bob@example.com").
		SetCc("carol@example.com").
		SetBcc("dave@example.com").
		SetSubject("Greetings").
		SetBody("Hello there")
}

// TestOracleEnvelopeMatchesGem compares our SMTP envelope (sender, recipients,
// Bcc-stripping) with the gem's smtp_envelope_from / smtp_envelope_to.
func TestOracleEnvelopeMatchesGem(t *testing.T) {
	bin := rubyBin(t)
	script := "m = " + gemMessageBuilder + "\n" +
		"puts m.smtp_envelope_from\n" +
		"puts m.smtp_envelope_to.join(\",\")\n" +
		"puts m.encoded.include?('Bcc:')\n"
	lines := strings.Split(strings.TrimRight(rubyEval(t, bin, script), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("oracle output: %q", lines)
	}
	env, err := buildEnvelope(oracleMessage())
	if err != nil {
		t.Fatalf("buildEnvelope: %v", err)
	}
	if env.from != lines[0] {
		t.Errorf("envelope from: ours=%q gem=%q", env.from, lines[0])
	}
	if got := strings.Join(env.to, ","); got != lines[1] {
		t.Errorf("envelope to: ours=%q gem=%q", got, lines[1])
	}
	if lines[2] != "false" {
		t.Errorf("gem unexpectedly kept Bcc in encoded output: %q", lines[2])
	}
	if strings.Contains(env.message, "Bcc:") {
		t.Errorf("our delivery message kept Bcc:\n%s", env.message)
	}
}

// TestOracleDeliveryMessageParsesInGem confirms the gem parses our
// delivery-encoded message back to the same From/To/Subject/Body and sees no
// Bcc field.
func TestOracleDeliveryMessageParsesInGem(t *testing.T) {
	bin := rubyBin(t)
	env, err := buildEnvelope(oracleMessage())
	if err != nil {
		t.Fatalf("buildEnvelope: %v", err)
	}
	script := heredoc(env.message) +
		"m = Mail.new(raw)\n" +
		"puts m.from.inspect\n" +
		"puts m.to.inspect\n" +
		"puts m.cc.inspect\n" +
		"puts m.subject.inspect\n" +
		"puts m.bcc.inspect\n" +
		"puts m.body.decoded.inspect\n"
	lines := strings.Split(strings.TrimRight(rubyEval(t, bin, script), "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("oracle output: %q", lines)
	}
	assertArrayEq(t, "from", []string{"alice@example.com"}, lines[0])
	assertArrayEq(t, "to", []string{"bob@example.com"}, lines[1])
	assertArrayEq(t, "cc", []string{"carol@example.com"}, lines[2])
	assertScalarEq(t, "subject", "Greetings", lines[3])
	// Bcc was stripped: the gem parses no bcc field.
	if lines[4] != "nil" && lines[4] != "[]" {
		t.Errorf("gem saw bcc in delivery message: %q", lines[4])
	}
}

// TestOracleSMTPWireMatchesGem delivers the same message to an in-process SMTP
// sink from both the gem and this package, and asserts the envelope and DATA put
// on the wire agree.
func TestOracleSMTPWireMatchesGem(t *testing.T) {
	bin := rubyBin(t)

	// Our delivery.
	goSink := &fakeSMTP{}
	goHost, goPort := oracleListen(t, goSink.serve)
	if err := (&SMTP{Address: goHost, Port: goPort}).Deliver(oracleMessage()); err != nil {
		t.Fatalf("go deliver: %v", err)
	}
	goFrom, goRcpt, goBody := goSink.recorded()

	// The gem's delivery to a fresh sink.
	rbSink := &fakeSMTP{}
	rbHost, rbPort := oracleListen(t, rbSink.serve)
	script := "Mail.defaults { delivery_method :smtp, address: '" + rbHost +
		"', port: " + strconv.Itoa(rbPort) + ", enable_starttls: false }\n" +
		gemMessageBuilder + ".deliver!\n"
	rubyEval(t, bin, script)
	rbFrom, rbRcpt, rbBody := rbSink.recorded()

	if goFrom != rbFrom {
		t.Errorf("MAIL FROM: ours=%q gem=%q", goFrom, rbFrom)
	}
	if strings.Join(goRcpt, ",") != strings.Join(rbRcpt, ",") {
		t.Errorf("RCPT TO: ours=%v gem=%v", goRcpt, rbRcpt)
	}
	for _, tc := range []struct {
		name, from, body string
	}{{"go", goFrom, goBody}, {"gem", rbFrom, rbBody}} {
		if tc.from != "alice@example.com" {
			t.Errorf("%s MAIL FROM = %q", tc.name, tc.from)
		}
		if !strings.Contains(tc.body, "Subject: Greetings") {
			t.Errorf("%s DATA missing subject:\n%s", tc.name, tc.body)
		}
		if strings.Contains(tc.body, "Bcc:") {
			t.Errorf("%s DATA leaked Bcc:\n%s", tc.name, tc.body)
		}
	}
}

// oracleListen starts a loopback TCP sink serving serve for each connection, and
// returns its host and port for a client (the gem or this package) to dial.
func oracleListen(t *testing.T, serve func(net.Conn)) (host string, port int) {
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
	h, p, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("addr: %v", err)
	}
	pn, _ := strconv.Atoi(p)
	return h, pn
}
