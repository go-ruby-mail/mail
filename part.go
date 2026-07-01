// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mail

import "strings"

// Part is one MIME part of a multipart message, mirroring Mail::Part. It shares
// the [Message] shape (a part is itself a message-like entity with its own
// header and body, and may nest further parts).
type Part = Message

// parsePart parses a raw MIME part (its own header + body, possibly nested
// multipart) into a Part.
func parsePart(raw string) *Part {
	// A part may begin with a stray leading newline from the boundary split.
	raw = strings.TrimPrefix(raw, "\n")
	return parseMessage(raw)
}

// ContentType returns the part's Content-Type header value with its parameters
// preserved (e.g. `text/plain; charset=UTF-8`), or "" when absent — mirroring
// Mail::Part#content_type.
func (m *Message) ContentType() string {
	return m.header.value("Content-Type")
}

// MimeType returns just the media type of the Content-Type (e.g. "text/plain"),
// lower-cased, mirroring Mail#mime_type.
func (m *Message) MimeType() string {
	main, _ := parseParams(m.header.value("Content-Type"))
	return strings.ToLower(main)
}

// ContentTypeParameters returns the parsed parameters of the Content-Type header
// (keys lower-cased), mirroring Mail#content_type_parameters.
func (m *Message) ContentTypeParameters() map[string]string {
	_, params := parseParams(m.header.value("Content-Type"))
	return params
}

// Charset returns the charset parameter of the Content-Type, or "".
func (m *Message) Charset() string {
	return m.ContentTypeParameters()["charset"]
}

// ContentDisposition returns the Content-Disposition media value (e.g.
// "attachment" / "inline"), or "".
func (m *Message) ContentDisposition() string {
	main, _ := parseParams(m.header.value("Content-Disposition"))
	return strings.ToLower(main)
}

// Filename returns the attachment filename from the Content-Disposition
// (filename=) or the Content-Type (name=) parameter, mirroring how Mail derives
// an attachment's filename; "" when the part is not a named attachment.
func (m *Message) Filename() string {
	_, disp := parseParams(m.header.value("Content-Disposition"))
	if fn := disp["filename"]; fn != "" {
		return decodeEncodedWords(fn)
	}
	_, ct := parseParams(m.header.value("Content-Type"))
	if n := ct["name"]; n != "" {
		return decodeEncodedWords(n)
	}
	return ""
}

// ContentID returns the Content-ID header value with any surrounding angle
// brackets removed, mirroring Mail#content_id.
func (m *Message) ContentID() string {
	return stripAngles(m.header.value("Content-ID"))
}

// IsAttachment reports whether the part is an attachment: either an explicit
// `Content-Disposition: attachment`, or any part that carries a filename.
func (m *Message) IsAttachment() bool {
	if m.ContentDisposition() == "attachment" {
		return true
	}
	return m.Filename() != ""
}

// Decoded returns the part's body decoded per its Content-Transfer-Encoding,
// mirroring Mail::Part#decoded.
func (m *Message) Decoded() []byte { return m.body.Decoded() }
