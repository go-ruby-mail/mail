// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mail

import "strings"

// Encoded serialises the message back to its RFC 5322 / MIME wire form, mirroring
// Mail#encoded / Mail#to_s. Header fields are emitted in insertion order (so a
// parsed message round-trips its field order), unstructured values carrying
// non-ASCII bytes are RFC 2047 encoded, and long fields are folded at 78
// columns. A multipart message re-emits its parts between the boundary
// delimiters.
func (m *Message) Encoded() string {
	var b strings.Builder
	for _, f := range m.header.fields {
		b.WriteString(foldField(f.Name, encodeFieldValue(f.Name, f.Value)))
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")
	b.WriteString(m.bodyEncoded())
	return b.String()
}

// String is an alias for Encoded, mirroring Mail#to_s.
func (m *Message) String() string { return m.Encoded() }

// bodyEncoded returns the wire body: for a multipart message the parts wrapped
// in boundary delimiters, otherwise the (transfer-encoded) body bytes.
func (m *Message) bodyEncoded() string {
	if len(m.parts) > 0 {
		_, params := parseParams(m.header.value("Content-Type"))
		boundary := params["boundary"]
		if boundary == "" {
			boundary = "boundary"
		}
		var b strings.Builder
		if m.preamble != "" {
			b.WriteString(toCRLF(m.preamble))
			b.WriteString("\r\n")
		}
		for _, p := range m.parts {
			b.WriteString("--")
			b.WriteString(boundary)
			b.WriteString("\r\n")
			enc := p.Encoded()
			b.WriteString(enc)
			// RFC 2046 §5.1: the boundary delimiter is a CRLF followed by
			// "--boundary". Ensure the part ends with a CRLF so the next
			// delimiter starts on its own line.
			if !strings.HasSuffix(enc, "\r\n") {
				b.WriteString("\r\n")
			}
		}
		b.WriteString("--")
		b.WriteString(boundary)
		b.WriteString("--\r\n")
		if m.epilogue != "" {
			b.WriteString(toCRLF(m.epilogue))
		}
		return b.String()
	}
	return m.body.Raw
}

// encodeFieldValue RFC 2047 encodes an unstructured field value when it carries
// non-ASCII bytes (Subject and other free-text fields). Structured fields
// (addresses, message-ids, dates) are emitted verbatim, matching the gem which
// encodes only phrases/unstructured text.
func encodeFieldValue(name, v string) string {
	if !needsEncodedWord(v) {
		return v
	}
	if isStructuredField(name) {
		return v // structured fields are assumed already valid / ASCII addr-specs
	}
	return encodeWordQ(v)
}

// isStructuredField reports whether name is an address/msg-id/date field whose
// value must not be treated as free text.
func isStructuredField(name string) bool {
	switch strings.ToLower(name) {
	case "from", "to", "cc", "bcc", "reply-to", "sender",
		"message-id", "in-reply-to", "references", "date",
		"content-type", "content-transfer-encoding", "content-disposition",
		"mime-version", "content-id":
		return true
	}
	return false
}

// foldField renders "Name: value" and folds it to lines of at most 78 columns by
// breaking at whitespace, per RFC 5322 §2.2.3. Continuation lines start with a
// single leading space.
func foldField(name, value string) string {
	line := name + ": " + value
	if len(line) <= 78 && !strings.ContainsAny(line, "\r\n") {
		return line
	}
	var b strings.Builder
	prefix := name + ": "
	b.WriteString(prefix)
	col := len(prefix)
	words := strings.Fields(value)
	for i, w := range words {
		add := len(w)
		if i > 0 {
			add++ // the joining space
		}
		if col+add > 78 && col > len(prefix) {
			b.WriteString("\r\n ")
			col = 1
			b.WriteString(w)
			col += len(w)
			continue
		}
		if i > 0 {
			b.WriteByte(' ')
			col++
		}
		b.WriteString(w)
		col += len(w)
	}
	return b.String()
}

// toCRLF normalises LF-only text to CRLF for wire output.
func toCRLF(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}
