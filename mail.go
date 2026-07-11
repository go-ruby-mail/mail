// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Package mail is a pure-Go (CGO-free) reimplementation of Ruby's `mail` gem —
// the RFC 5322 / MIME message parser and generator. It builds and reads email
// messages: header fold/unfold, structured address fields, RFC 2047
// encoded-words, RFC 2045 MIME (multipart, boundaries, nested parts,
// attachments, and base64 / quoted-printable transfer encodings) — all without
// any Ruby runtime.
//
// # API shape
//
// [New] parses a raw message (or, given a builder function, constructs one);
// [Read] reads a message from a file. The resulting [Message] exposes the
// gem-faithful accessors From/To/Cc/Bcc/ReplyTo/Subject/Body/Date/MessageID and
// the MIME view Parts/Attachments/Multipart/ContentType, plus [Message.Encoded]
// / [Message.String] to serialise it back.
//
// # Delivery and retrieval
//
// Sending and fetching are implemented here too, mirroring the gem's delivery
// and retriever methods: configure them with [Defaults] and send with
// [Message.Deliver], or fetch with [Find] / [First] / [Last] / [All]. The
// delivery methods are [SMTP] (net/smtp, with STARTTLS/implicit-TLS and PLAIN/
// LOGIN/CRAM-MD5 auth), [Sendmail], [TestDelivery], [FileDelivery] and
// [LoggerDelivery]; the retrievers are [POP3] and [IMAP], both pure-Go clients.
// Every transport reaches its server through an injectable [Dialer] seam, so it
// stays CGO-free and fully testable against in-process servers.
package mail

import (
	"os"
	"strings"
)

// Message is a parsed or constructed email message, mirroring Ruby's Mail
// message object. Build one with [New]; read it back with the accessors, or
// serialise it with [Message.Encoded].
type Message struct {
	header *Header
	body   *Body

	// parts holds the child parts of a multipart message (empty otherwise).
	parts []*Part

	// epilogue/preamble hold the text outside the MIME boundaries, preserved so
	// round-tripping does not lose them.
	preamble string
	epilogue string
}

// New parses raw as an RFC 5322 message, mirroring Mail.new(raw). If raw is
// empty and builders are supplied, it constructs a message by running each
// builder against the fresh message instead (mirroring the `Mail.new { … }`
// block form).
func New(raw string, builders ...func(*Message)) *Message {
	if raw == "" {
		m := &Message{header: &Header{}, body: &Body{}}
		for _, b := range builders {
			b(m)
		}
		return m
	}
	m := parseMessage(raw)
	for _, b := range builders {
		b(m)
	}
	return m
}

// Read reads a message from the file at path, mirroring Mail.read(path).
func Read(path string) (*Message, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return New(string(data)), nil
}

// parseMessage splits raw into header and body, parses the header, and — when
// the Content-Type is multipart — recursively parses the child parts.
func parseMessage(raw string) *Message {
	head, body := splitHeaderBody(raw)
	m := &Message{header: parseHeader(head)}
	cte := firstToken(m.header.value("Content-Transfer-Encoding"))
	m.body = &Body{Raw: body, Encoding: cte}

	ct := m.header.value("Content-Type")
	main, params := parseParams(ct)
	if strings.HasPrefix(strings.ToLower(main), "multipart/") {
		if boundary := params["boundary"]; boundary != "" {
			m.preamble, m.parts, m.epilogue = parseMultipart(body, boundary)
		}
	}
	return m
}

// splitHeaderBody separates the header block from the body at the first blank
// line, tolerating both CRLF and LF line endings. The returned body retains its
// original line endings.
func splitHeaderBody(raw string) (head, body string) {
	if i := strings.Index(raw, "\r\n\r\n"); i >= 0 {
		return raw[:i], raw[i+4:]
	}
	if i := strings.Index(raw, "\n\n"); i >= 0 {
		return raw[:i], raw[i+2:]
	}
	return raw, ""
}

// firstToken returns the value up to the first ";" or whitespace, trimmed — used
// to read the bare token of Content-Transfer-Encoding.
func firstToken(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "; \t"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// parseMultipart splits a multipart body on its boundary delimiter into a
// preamble, the child parts, and an epilogue, per RFC 2046 §5.1.
func parseMultipart(body, boundary string) (preamble string, parts []*Part, epilogue string) {
	delim := "--" + boundary
	lines := strings.Split(normalizeNL(body), "\n")

	var cur []string
	started := false
	closed := false
	var preLines []string
	var epiLines []string

	// appendPart flushes the accumulated lines as a part. It is only ever called
	// once started is true (after the opening boundary), so no guard is needed.
	appendPart := func() {
		text := strings.Join(cur, "\n")
		parts = append(parts, parsePart(text))
		cur = nil
	}

	for _, ln := range lines {
		trimmed := strings.TrimRight(ln, " \t")
		switch {
		case !started && trimmed == delim:
			started = true
		case !started && trimmed == delim+"--":
			started = true
			closed = true
		case !started:
			preLines = append(preLines, ln)
		case closed:
			epiLines = append(epiLines, ln)
		case trimmed == delim:
			appendPart()
		case trimmed == delim+"--":
			appendPart()
			closed = true
		default:
			cur = append(cur, ln)
		}
	}
	if started && !closed {
		appendPart()
	}
	return strings.Join(preLines, "\n"), parts, strings.Join(epiLines, "\n")
}

// normalizeNL converts CRLF to LF so line splitting is uniform.
func normalizeNL(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }
