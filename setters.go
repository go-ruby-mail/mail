// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mail

import "time"

// The setters below back the `Mail.new { … }` builder block and direct field
// assignment (`mail.subject = …`). Each replaces any existing field of that
// name, matching the gem's scalar writer semantics.

// SetFrom sets the From field.
func (m *Message) SetFrom(v string) *Message { m.header.set("From", v); return m }

// SetTo sets the To field.
func (m *Message) SetTo(v string) *Message { m.header.set("To", v); return m }

// SetCc sets the Cc field.
func (m *Message) SetCc(v string) *Message { m.header.set("Cc", v); return m }

// SetBcc sets the Bcc field.
func (m *Message) SetBcc(v string) *Message { m.header.set("Bcc", v); return m }

// SetReplyTo sets the Reply-To field.
func (m *Message) SetReplyTo(v string) *Message { m.header.set("Reply-To", v); return m }

// SetSubject sets the Subject field. The value is stored raw; header emission
// RFC 2047 encodes it when it contains non-ASCII bytes.
func (m *Message) SetSubject(v string) *Message { m.header.set("Subject", v); return m }

// SetMessageID sets the Message-ID field, adding angle brackets if the caller
// passed a bare id.
func (m *Message) SetMessageID(v string) *Message {
	if v != "" && v[0] != '<' {
		v = "<" + v + ">"
	}
	m.header.set("Message-ID", v)
	return m
}

// SetInReplyTo sets the In-Reply-To field.
func (m *Message) SetInReplyTo(v string) *Message { m.header.set("In-Reply-To", v); return m }

// SetReferences sets the References field.
func (m *Message) SetReferences(v string) *Message { m.header.set("References", v); return m }

// SetDate sets the Date field to the RFC 5322 rendering of t.
func (m *Message) SetDate(t time.Time) *Message {
	m.header.set("Date", formatDate(t))
	return m
}

// SetDateString sets the Date field to a raw string (already formatted).
func (m *Message) SetDateString(v string) *Message { m.header.set("Date", v); return m }

// SetContentType sets the Content-Type field.
func (m *Message) SetContentType(v string) *Message { m.header.set("Content-Type", v); return m }

// SetContentTypeParams sets the Content-Type field from a media type plus
// parameters, emitting them in the given key order (each value quoted when
// required). Mirrors building a Content-Type with content_type_parameters.
func (m *Message) SetContentTypeParams(mediaType string, order []string, params map[string]string) *Message {
	m.header.set("Content-Type", formatParams(mediaType, order, params))
	return m
}

// SetContentTransferEncoding sets the Content-Transfer-Encoding field and keeps
// the body's decode encoding in sync.
func (m *Message) SetContentTransferEncoding(v string) *Message {
	m.header.set("Content-Transfer-Encoding", v)
	if m.body != nil {
		m.body.Encoding = v
	}
	return m
}

// SetBody sets the body content (decoded/plain form).
func (m *Message) SetBody(v string) *Message {
	if m.body == nil {
		m.body = &Body{}
	}
	m.body.Raw = v
	return m
}

// SetHeader sets an arbitrary header field by name.
func (m *Message) SetHeader(name, v string) *Message { m.header.set(name, v); return m }

// AddHeader appends a header field without removing existing same-named fields.
func (m *Message) AddHeader(name, v string) *Message { m.header.add(name, v); return m }

// AddPart appends a child part and marks the message multipart if it is not
// already (the caller is expected to set the multipart Content-Type/boundary, or
// rely on the default applied at emission).
func (m *Message) AddPart(p *Part) *Message {
	m.parts = append(m.parts, p)
	return m
}
