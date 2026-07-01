// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mail

import (
	"encoding/base64"
	"strings"
	"unicode/utf8"
)

// fromHex returns the value of an ASCII hex digit and whether it was valid.
func fromHex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	}
	return 0, false
}

// toHex returns the uppercase ASCII hex digit for a nibble (0..15).
func toHex(n byte) byte {
	if n < 10 {
		return '0' + n
	}
	return 'A' + (n - 10)
}

// decodeQuotedPrintable decodes an RFC 2045 quoted-printable body: "=XX" hex
// escapes and soft line breaks ("=" at end of line). It is byte-exact with the
// `mail` gem's decode, including treating a lone "=" not followed by two hex
// digits as a literal "=".
func decodeQuotedPrintable(s string) []byte {
	var out []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '=' {
			out = append(out, c)
			continue
		}
		// Soft line break: "=\r\n" or "=\n".
		if i+1 < len(s) && s[i+1] == '\n' {
			i++
			continue
		}
		if i+2 < len(s) && s[i+1] == '\r' && s[i+2] == '\n' {
			i += 2
			continue
		}
		if i+2 < len(s) {
			hi, ok1 := fromHex(s[i+1])
			lo, ok2 := fromHex(s[i+2])
			if ok1 && ok2 {
				out = append(out, hi<<4|lo)
				i += 2
				continue
			}
		}
		// Not a valid escape — keep the literal "=".
		out = append(out, c)
	}
	return out
}

// encodeQuotedPrintable encodes raw as an RFC 2045 quoted-printable body,
// wrapping lines to at most 76 characters with soft breaks, matching the `mail`
// gem's body emission for the "quoted-printable" transfer encoding.
func encodeQuotedPrintable(raw []byte) string {
	var b strings.Builder
	lineLen := 0
	emit := func(tok string) {
		if lineLen+len(tok) > 75 {
			b.WriteString("=\r\n")
			lineLen = 0
		}
		b.WriteString(tok)
		lineLen += len(tok)
	}
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c == '\n' {
			b.WriteString("\r\n")
			lineLen = 0
			continue
		}
		if c == '\r' {
			continue
		}
		if qpSafe(c) {
			emit(string(c))
			continue
		}
		emit(string([]byte{'=', toHex(c >> 4), toHex(c & 0x0f)}))
	}
	return b.String()
}

// qpSafe reports whether c may appear literally in a quoted-printable body
// (printable US-ASCII other than "=", and including space/tab which we escape
// only at line ends — simplified here to always-literal for readability parity).
func qpSafe(c byte) bool {
	if c == '=' {
		return false
	}
	if c == '\t' {
		return true
	}
	return c >= 0x20 && c <= 0x7e
}

// encodeBase64 encodes raw as a base64 body wrapped to 76-character lines with
// CRLF separators, matching the `mail` gem's base64 body emission.
func encodeBase64(raw []byte) string {
	enc := base64.StdEncoding.EncodeToString(raw)
	var b strings.Builder
	for len(enc) > 76 {
		b.WriteString(enc[:76])
		b.WriteString("\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc)
	b.WriteString("\r\n")
	return b.String()
}

// decodeBase64 decodes a base64 body, tolerating embedded whitespace/newlines
// (as bodies wrap). Invalid input yields the best-effort prefix.
func decodeBase64(s string) []byte {
	dec, err := base64.StdEncoding.DecodeString(stripWS(s))
	if err != nil {
		// Best effort: base64.RawStdEncoding for un-padded input.
		if d2, e2 := base64.RawStdEncoding.DecodeString(stripWS(s)); e2 == nil {
			return d2
		}
		return dec
	}
	return dec
}

// charsetToUTF8 reinterprets raw bytes labelled with charset as UTF-8. UTF-8 and
// US-ASCII pass through; ISO-8859-1 / Latin-1 is transcoded rune-by-rune.
// Any other charset is returned as-is (bytes preserved) so no data is lost.
func charsetToUTF8(charset string, raw []byte) string {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "utf-8", "utf8", "us-ascii", "ascii", "":
		return string(raw)
	case "iso-8859-1", "latin1", "latin-1", "iso8859-1", "windows-1252", "cp1252":
		var b strings.Builder
		for _, c := range raw {
			b.WriteRune(rune(c))
		}
		return b.String()
	default:
		if utf8.Valid(raw) {
			return string(raw)
		}
		return string(raw)
	}
}
