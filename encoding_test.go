// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mail

import (
	"strings"
	"testing"
)

func TestEncodeQuotedPrintableRoundTrip(t *testing.T) {
	raw := []byte("Héllo wörld with = sign and\ttab")
	enc := encodeQuotedPrintable(raw)
	dec := decodeQuotedPrintable(enc)
	if string(dec) != "Héllo wörld with = sign and\ttab" {
		t.Errorf("QP round trip = %q", string(dec))
	}
}

func TestEncodeQuotedPrintableWraps(t *testing.T) {
	raw := []byte(strings.Repeat("a", 200))
	enc := encodeQuotedPrintable(raw)
	for _, ln := range strings.Split(enc, "\r\n") {
		if len(ln) > 76 {
			t.Errorf("QP line too long (%d)", len(ln))
		}
	}
	// Soft breaks decode away.
	if string(decodeQuotedPrintable(enc)) != strings.Repeat("a", 200) {
		t.Error("QP wrap round trip failed")
	}
}

func TestEncodeQuotedPrintableNewline(t *testing.T) {
	enc := encodeQuotedPrintable([]byte("line1\nline2"))
	if !strings.Contains(enc, "line1\r\nline2") {
		t.Errorf("QP newline = %q", enc)
	}
}

func TestEncodeQuotedPrintableStripsCR(t *testing.T) {
	enc := encodeQuotedPrintable([]byte("a\r\nb"))
	if !strings.Contains(enc, "a\r\nb") {
		t.Errorf("QP CRLF = %q", enc)
	}
}

func TestEncodeBase64Wraps(t *testing.T) {
	raw := make([]byte, 200)
	for i := range raw {
		raw[i] = byte(i)
	}
	enc := encodeBase64(raw)
	lines := strings.Split(strings.TrimRight(enc, "\r\n"), "\r\n")
	for _, ln := range lines {
		if len(ln) > 76 {
			t.Errorf("b64 line too long (%d)", len(ln))
		}
	}
	if string(decodeBase64(enc)) != string(raw) {
		t.Error("b64 round trip failed")
	}
}

func TestEncodeBase64Short(t *testing.T) {
	enc := encodeBase64([]byte("hi"))
	if strings.TrimRight(enc, "\r\n") != "aGk=" {
		t.Errorf("short b64 = %q", enc)
	}
}

func TestBodyEncodeMethod(t *testing.T) {
	b := &Body{Raw: "aGVsbG8=", Encoding: "base64"} // decodes to "hello"
	// Re-encode the decoded content as quoted-printable.
	if got := b.Encode("quoted-printable"); !strings.Contains(got, "hello") {
		t.Errorf("Encode QP = %q", got)
	}
	// Re-encode as base64.
	if got := b.Encode("base64"); strings.TrimRight(got, "\r\n") != "aGVsbG8=" {
		t.Errorf("Encode b64 = %q", got)
	}
	// Identity.
	if got := b.Encode("7bit"); got != "hello" {
		t.Errorf("Encode 7bit = %q", got)
	}
}

func TestFromHexLower(t *testing.T) {
	if v, ok := fromHex('a'); !ok || v != 10 {
		t.Errorf("fromHex('a') = %d %v", v, ok)
	}
	if _, ok := fromHex('g'); ok {
		t.Error("fromHex('g') should fail")
	}
}

func TestCharsetToUTF8UnknownInvalidUTF8(t *testing.T) {
	// Unknown charset with invalid UTF-8 bytes: preserved as-is.
	raw := []byte{0xff, 0xfe}
	got := charsetToUTF8("x-custom", raw)
	if got != string(raw) {
		t.Errorf("unknown invalid = %q", got)
	}
	// Unknown charset with valid UTF-8: passes through.
	if g := charsetToUTF8("x-custom", []byte("ok")); g != "ok" {
		t.Errorf("unknown valid = %q", g)
	}
}

func TestSetContentTypeParams(t *testing.T) {
	m := New("").SetContentTypeParams("text/plain", []string{"charset", "format"},
		map[string]string{"charset": "UTF-8", "format": "flowed", "unused": "x"})
	ct := m.Field("Content-Type")
	if ct != "text/plain; charset=UTF-8; format=flowed" {
		t.Errorf("content-type = %q", ct)
	}
}

func TestFormatParamsQuoting(t *testing.T) {
	got := formatParams("multipart/mixed", []string{"boundary"},
		map[string]string{"boundary": "a b"})
	if got != `multipart/mixed; boundary="a b"` {
		t.Errorf("quoted param = %q", got)
	}
	// Empty value is quoted.
	if g := formatParams("x", []string{"k"}, map[string]string{"k": ""}); g != `x; k=""` {
		t.Errorf("empty param = %q", g)
	}
	// Missing key in order is skipped.
	if g := formatParams("x", []string{"missing"}, map[string]string{}); g != "x" {
		t.Errorf("missing key = %q", g)
	}
}

func TestParseParamsEdgeCases(t *testing.T) {
	// Attribute with no value.
	main, params := parseParams("text/plain; flag")
	if main != "text/plain" || params["flag"] != "" {
		t.Errorf("no-value param: %q %v", main, params)
	}
	// Empty input.
	if m, p := parseParams(""); m != "" || len(p) != 0 {
		t.Errorf("empty params: %q %v", m, p)
	}
	// Quoted value with escaped quote.
	if _, p := parseParams(`x; name="a\"b"`); p["name"] != `a"b` {
		t.Errorf("escaped quote param = %q", p["name"])
	}
	// Blank token between semicolons is skipped.
	if _, p := parseParams("x; ; charset=utf-8"); p["charset"] != "utf-8" {
		t.Errorf("blank token param = %v", p)
	}
}

func TestFirstTokenWhitespace(t *testing.T) {
	m := New("Content-Transfer-Encoding: base64 (comment)\r\n\r\naGk=")
	if m.body.Encoding != "base64" {
		t.Errorf("first token = %q", m.body.Encoding)
	}
}

func TestPartContentTypeAccessor(t *testing.T) {
	p := New("Content-Type: text/html; charset=iso-8859-1\r\n\r\nx")
	if p.ContentType() != "text/html; charset=iso-8859-1" {
		t.Errorf("ContentType = %q", p.ContentType())
	}
}

func TestParseDateVariants(t *testing.T) {
	cases := []string{
		"Mon, 01 Jul 2026 10:00:00 +0000",
		"1 Jul 2026 10:00:00 +0000",
		"01 Jul 2026 10:00:00 +0000",
		"Mon, 1 Jul 2026 10:00:00 +0000",
	}
	for _, c := range cases {
		if _, ok := parseDate(c); !ok {
			t.Errorf("failed to parse %q", c)
		}
	}
	if _, ok := parseDate("not a date"); ok {
		t.Error("garbage date should fail")
	}
}

func TestQSafeSpecials(t *testing.T) {
	// "=", "?", "_" and space are not q-safe.
	for _, c := range []byte{'=', '?', '_', ' ', 0x00, 0x7f, 0x80} {
		if qSafe(c) {
			t.Errorf("qSafe(%q) should be false", c)
		}
	}
	if !qSafe('A') {
		t.Error("qSafe('A') should be true")
	}
}

func TestEncodeWordQFullCoverage(t *testing.T) {
	// Space -> "_", safe char literal, unsafe -> "=XX".
	got := encodeWordQ("A =")
	if got != "=?UTF-8?Q?A_=3D?=" {
		t.Errorf("encodeWordQ = %q", got)
	}
}

func TestNeedsEncodedWord(t *testing.T) {
	if needsEncodedWord("plain ascii") {
		t.Error("ascii should not need encoding")
	}
	if !needsEncodedWord("café") {
		t.Error("non-ascii should need encoding")
	}
	if !needsEncodedWord("has\tcontrol\x01") {
		t.Error("control chars need encoding")
	}
}

func TestIsStructuredField(t *testing.T) {
	if !isStructuredField("From") || !isStructuredField("message-id") {
		t.Error("known structured fields")
	}
	if isStructuredField("X-Custom") {
		t.Error("custom field is not structured")
	}
}

func TestEncodeFieldValueStructuredNonASCII(t *testing.T) {
	// A structured field with non-ASCII is emitted verbatim (not encoded).
	if got := encodeFieldValue("From", "é@x.com"); got != "é@x.com" {
		t.Errorf("structured non-ascii = %q", got)
	}
	// Unstructured non-ASCII is encoded.
	if got := encodeFieldValue("Subject", "café"); !strings.HasPrefix(got, "=?UTF-8?Q?") {
		t.Errorf("unstructured non-ascii = %q", got)
	}
	// ASCII passes through.
	if got := encodeFieldValue("Subject", "plain"); got != "plain" {
		t.Errorf("ascii = %q", got)
	}
}

func TestSplitLinesEmpty(t *testing.T) {
	if splitLines("") != nil {
		t.Error("empty splitLines should be nil")
	}
}
