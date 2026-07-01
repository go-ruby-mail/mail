// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mail

import (
	"strings"
	"time"
)

// Header returns the message's header for direct field access, mirroring
// Mail#header.
func (m *Message) Header() *Header { return m.header }

// Body returns the message's [Body], mirroring Mail#body. For a multipart
// message this is the raw wrapper body; use [Message.Parts] for the children.
func (m *Message) Body() *Body { return m.body }

// Field returns the unfolded raw value of the named header field, or "".
func (m *Message) Field(name string) string { return m.header.value(name) }

// addressStrings parses the named address field and returns the addr-specs, in
// order — the shape Mail's from/to/cc/… accessors return (an array of address
// strings).
func (m *Message) addressStrings(name string) []string {
	v := m.header.value(name)
	if v == "" {
		return nil
	}
	al := NewAddressList(v)
	out := make([]string, 0, len(al.addrs))
	for _, a := range al.addrs {
		out = append(out, a.Address())
	}
	return out
}

// From returns the From addresses as addr-specs, mirroring Mail#from.
func (m *Message) From() []string { return m.addressStrings("From") }

// To returns the To addresses, mirroring Mail#to.
func (m *Message) To() []string { return m.addressStrings("To") }

// Cc returns the Cc addresses, mirroring Mail#cc.
func (m *Message) Cc() []string { return m.addressStrings("Cc") }

// Bcc returns the Bcc addresses, mirroring Mail#bcc.
func (m *Message) Bcc() []string { return m.addressStrings("Bcc") }

// ReplyTo returns the Reply-To addresses, mirroring Mail#reply_to.
func (m *Message) ReplyTo() []string { return m.addressStrings("Reply-To") }

// Subject returns the decoded Subject, mirroring Mail#subject.
func (m *Message) Subject() string { return decodeEncodedWords(m.header.value("Subject")) }

// MessageID returns the Message-ID with angle brackets stripped, mirroring
// Mail#message_id (which returns the msg-id without the angle brackets).
func (m *Message) MessageID() string { return stripAngles(m.header.value("Message-ID")) }

// InReplyTo returns the In-Reply-To msg-id without angle brackets. When multiple
// ids are present it returns the first (as the gem does for a scalar accessor).
func (m *Message) InReplyTo() string {
	ids := m.References()
	fromField := splitMsgIDs(m.header.value("In-Reply-To"))
	if len(fromField) > 0 {
		return fromField[0]
	}
	if len(ids) > 0 {
		return ids[len(ids)-1]
	}
	return ""
}

// References returns the References msg-ids (angle brackets stripped), in order,
// mirroring Mail#references.
func (m *Message) References() []string {
	return splitMsgIDs(m.header.value("References"))
}

// Date returns the parsed Date header. The bool reports whether a Date was
// present and parseable, mirroring the fact that Mail#date returns nil when
// absent.
func (m *Message) Date() (time.Time, bool) {
	v := m.header.value("Date")
	if v == "" {
		return time.Time{}, false
	}
	return parseDate(v)
}

// MIMEVersion returns the MIME-Version header value, or "".
func (m *Message) MIMEVersion() string { return m.header.value("MIME-Version") }

// Multipart reports whether the message is multipart (has a multipart/*
// Content-Type and at least one parsed part), mirroring Mail#multipart?.
func (m *Message) Multipart() bool {
	return strings.HasPrefix(m.MimeType(), "multipart/") && len(m.parts) > 0
}

// Parts returns the child MIME parts, mirroring Mail#parts (empty for a
// non-multipart message).
func (m *Message) Parts() []*Part { return m.parts }

// Attachments returns the parts that are attachments (see [Message.IsAttachment]),
// recursing into nested multipart parts, mirroring Mail#attachments.
func (m *Message) Attachments() []*Part {
	var out []*Part
	var walk func(parts []*Part)
	walk = func(parts []*Part) {
		for _, p := range parts {
			if p.IsAttachment() {
				out = append(out, p)
				continue
			}
			if len(p.parts) > 0 {
				walk(p.parts)
			}
		}
	}
	walk(m.parts)
	return out
}

// TextPart returns the first text/plain part of a multipart message (or the
// message itself when it is a plain text message), mirroring Mail#text_part.
func (m *Message) TextPart() *Part { return m.findPart("text/plain") }

// HTMLPart returns the first text/html part, mirroring Mail#html_part.
func (m *Message) HTMLPart() *Part { return m.findPart("text/html") }

// findPart returns the first part (recursively) whose mime type matches, or nil.
func (m *Message) findPart(mime string) *Part {
	if !m.Multipart() {
		if m.MimeType() == mime || (mime == "text/plain" && m.MimeType() == "") {
			return m
		}
		return nil
	}
	var walk func(parts []*Part) *Part
	walk = func(parts []*Part) *Part {
		for _, p := range parts {
			if p.MimeType() == mime {
				return p
			}
			if len(p.parts) > 0 {
				if found := walk(p.parts); found != nil {
					return found
				}
			}
		}
		return nil
	}
	return walk(m.parts)
}

// stripAngles removes a single pair of surrounding angle brackets from a msg-id.
func stripAngles(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "<")
	s = strings.TrimSuffix(s, ">")
	return s
}

// splitMsgIDs parses a whitespace/comma separated list of angle-bracketed
// msg-ids into their bare forms.
func splitMsgIDs(s string) []string {
	var out []string
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == ','
	})
	for _, f := range fields {
		id := stripAngles(f)
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}
