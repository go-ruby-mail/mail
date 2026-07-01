// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mail

import "strings"

// Body holds a message or part body, mirroring Mail::Body. Raw is the body
// exactly as it appeared on the wire (still transfer-encoded); Encoding names
// the Content-Transfer-Encoding used to decode it.
type Body struct {
	Raw      string
	Encoding string
}

// Decoded returns the body decoded according to its Content-Transfer-Encoding,
// matching Mail::Body#decoded. Unknown or identity encodings (7bit/8bit/binary)
// return the raw bytes unchanged.
func (b *Body) Decoded() []byte {
	switch strings.ToLower(strings.TrimSpace(b.Encoding)) {
	case "base64":
		return decodeBase64(b.Raw)
	case "quoted-printable":
		return decodeQuotedPrintable(b.Raw)
	default:
		return []byte(b.Raw)
	}
}

// DecodedString is Decoded as a string, for convenience.
func (b *Body) DecodedString() string { return string(b.Decoded()) }

// Encode returns b's decoded content re-encoded for the named
// Content-Transfer-Encoding (base64 / quoted-printable / identity), the form a
// host uses when serialising a body it built from decoded bytes. Mirrors
// Mail::Body#encoded.
func (b *Body) Encode(encoding string) string {
	return encodeBody(b.Decoded(), encoding)
}

// encodeBody returns the given decoded content encoded for the named
// Content-Transfer-Encoding, used when generating output.
func encodeBody(decoded []byte, encoding string) string {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		return encodeBase64(decoded)
	case "quoted-printable":
		return encodeQuotedPrintable(decoded)
	default:
		return string(decoded)
	}
}
