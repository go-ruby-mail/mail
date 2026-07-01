// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mail

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// decodeEncodedWords decodes any RFC 2047 encoded-words (`=?charset?B?...?=` /
// `=?charset?Q?...?=`) embedded in s, matching the `mail` gem's unstructured
// field decode. Text outside encoded-words is passed through verbatim; adjacent
// encoded-words separated only by linear whitespace have that whitespace
// dropped (per RFC 2047 §6.2). Only UTF-8 / US-ASCII / ISO-8859-1 charsets are
// interpreted; an unknown charset is left byte-for-byte so no information is lost.
func decodeEncodedWords(s string) string {
	var b strings.Builder
	i := 0
	prevWasWord := false // last emitted token was an encoded-word
	for i < len(s) {
		start := strings.Index(s[i:], "=?")
		if start < 0 {
			b.WriteString(s[i:])
			break
		}
		start += i
		// The run of characters before the encoded-word. If it is only
		// whitespace and it sits between two encoded-words, RFC 2047 drops it.
		gap := s[i:start]
		word, next, ok := parseEncodedWord(s, start)
		if !ok {
			// Not a valid encoded-word; emit the "=?" literally and advance.
			b.WriteString(s[i : start+2])
			i = start + 2
			prevWasWord = false
			continue
		}
		if prevWasWord && strings.TrimSpace(gap) == "" {
			// Fold: whitespace between two encoded-words is removed.
		} else {
			b.WriteString(gap)
		}
		b.WriteString(word)
		i = next
		prevWasWord = true
	}
	return b.String()
}

// parseEncodedWord parses one encoded-word beginning at s[start] (which must be
// "=?"). It returns the decoded text, the index just past the closing "?=", and
// whether a well-formed encoded-word was found.
func parseEncodedWord(s string, start int) (string, int, bool) {
	rest := s[start+2:]
	// charset
	q1 := strings.IndexByte(rest, '?')
	if q1 < 0 {
		return "", 0, false
	}
	charset := rest[:q1]
	rest = rest[q1+1:]
	// encoding letter
	if len(rest) < 2 || rest[1] != '?' {
		return "", 0, false
	}
	enc := rest[0]
	rest = rest[2:]
	// encoded text up to "?="
	end := strings.Index(rest, "?=")
	if end < 0 {
		return "", 0, false
	}
	encoded := rest[:end]
	next := start + 2 + q1 + 1 + 2 + end + 2

	var raw []byte
	switch enc {
	case 'B', 'b':
		dec, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err != nil {
			// Retry lenient (drop stray whitespace) then give up.
			dec, err = base64.StdEncoding.DecodeString(stripWS(encoded))
			if err != nil {
				return "", 0, false
			}
		}
		raw = dec
	case 'Q', 'q':
		dec, err := decodeQEncoding(encoded)
		if err != nil {
			return "", 0, false
		}
		raw = dec
	default:
		return "", 0, false
	}
	return charsetToUTF8(charset, raw), next, true
}

// decodeQEncoding decodes the RFC 2047 "Q" encoding: "=XX" hex escapes and "_"
// meaning a space. Unlike RFC 2045 quoted-printable it has no soft line breaks.
func decodeQEncoding(s string) ([]byte, error) {
	var out []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '_':
			out = append(out, ' ')
		case '=':
			if i+2 >= len(s) {
				return nil, fmt.Errorf("mail: truncated Q escape %q", s[i:])
			}
			hi, ok1 := fromHex(s[i+1])
			lo, ok2 := fromHex(s[i+2])
			if !ok1 || !ok2 {
				return nil, fmt.Errorf("mail: bad Q escape %q", s[i:i+3])
			}
			out = append(out, hi<<4|lo)
			i += 2
		default:
			out = append(out, c)
		}
	}
	return out, nil
}

// stripWS removes all ASCII whitespace from s.
func stripWS(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\r', '\n':
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// needsEncodedWord reports whether s contains bytes outside the printable
// US-ASCII range, meaning it must be RFC 2047 encoded to appear in a header.
func needsEncodedWord(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return true
		}
	}
	return false
}

// encodeWordQ encodes s as a single UTF-8 "Q" encoded-word, matching the `mail`
// gem's default header emission (`=?UTF-8?Q?...?=`). Bytes that are safe in a Q
// word pass through; space becomes "_", everything else becomes "=XX".
func encodeWordQ(s string) string {
	var b strings.Builder
	b.WriteString("=?UTF-8?Q?")
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ':
			b.WriteByte('_')
		case qSafe(c):
			b.WriteByte(c)
		default:
			b.WriteByte('=')
			b.WriteByte(toHex(c >> 4))
			b.WriteByte(toHex(c & 0x0f))
		}
	}
	b.WriteString("?=")
	return b.String()
}

// qSafe reports whether c may appear literally in a Q encoded-word (it excludes
// the special "=", "?", "_", space and any non-printable / 8-bit byte).
func qSafe(c byte) bool {
	if c <= 0x20 || c >= 0x7f {
		return false
	}
	switch c {
	case '=', '?', '_':
		return false
	}
	return true
}
