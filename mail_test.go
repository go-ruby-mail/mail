// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mail

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseBasicHeaders(t *testing.T) {
	raw := "From: John <john@example.com>\r\n" +
		"To: a@x.com, b@y.com\r\n" +
		"Cc: c@x.com\r\n" +
		"Bcc: d@x.com\r\n" +
		"Reply-To: r@x.com\r\n" +
		"Subject: Hello\r\n" +
		"Message-ID: <abc@example.com>\r\n" +
		"In-Reply-To: <prev@example.com>\r\n" +
		"References: <1@x> <2@x>\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Date: Mon, 01 Jul 2026 10:00:00 +0000\r\n" +
		"\r\n" +
		"Body text here\r\n"
	m := New(raw)

	if got := m.From(); !reflect.DeepEqual(got, []string{"john@example.com"}) {
		t.Errorf("From = %v", got)
	}
	if got := m.To(); !reflect.DeepEqual(got, []string{"a@x.com", "b@y.com"}) {
		t.Errorf("To = %v", got)
	}
	if got := m.Cc(); !reflect.DeepEqual(got, []string{"c@x.com"}) {
		t.Errorf("Cc = %v", got)
	}
	if got := m.Bcc(); !reflect.DeepEqual(got, []string{"d@x.com"}) {
		t.Errorf("Bcc = %v", got)
	}
	if got := m.ReplyTo(); !reflect.DeepEqual(got, []string{"r@x.com"}) {
		t.Errorf("ReplyTo = %v", got)
	}
	if m.Subject() != "Hello" {
		t.Errorf("Subject = %q", m.Subject())
	}
	if m.MessageID() != "abc@example.com" {
		t.Errorf("MessageID = %q", m.MessageID())
	}
	if m.InReplyTo() != "prev@example.com" {
		t.Errorf("InReplyTo = %q", m.InReplyTo())
	}
	if got := m.References(); !reflect.DeepEqual(got, []string{"1@x", "2@x"}) {
		t.Errorf("References = %v", got)
	}
	if m.MIMEVersion() != "1.0" {
		t.Errorf("MIMEVersion = %q", m.MIMEVersion())
	}
	if m.Body().DecodedString() != "Body text here\r\n" {
		t.Errorf("Body = %q", m.Body().DecodedString())
	}
	if d, ok := m.Date(); !ok || d.Year() != 2026 || d.Hour() != 10 {
		t.Errorf("Date = %v ok=%v", d, ok)
	}
}

func TestEmptyAndAbsentAccessors(t *testing.T) {
	m := New("Subject: x\r\n\r\nbody")
	if m.From() != nil {
		t.Errorf("From should be nil, got %v", m.From())
	}
	if m.To() != nil || m.Cc() != nil || m.Bcc() != nil || m.ReplyTo() != nil {
		t.Error("absent address fields should be nil")
	}
	if m.References() != nil {
		t.Errorf("References nil expected, got %v", m.References())
	}
	if m.InReplyTo() != "" {
		t.Errorf("InReplyTo empty expected, got %q", m.InReplyTo())
	}
	if _, ok := m.Date(); ok {
		t.Error("Date should be absent")
	}
	if m.MessageID() != "" || m.MIMEVersion() != "" {
		t.Error("absent scalar fields should be empty")
	}
	if m.Field("Subject") != "x" {
		t.Errorf("Field = %q", m.Field("Subject"))
	}
}

func TestInReplyToFallsBackToReferences(t *testing.T) {
	m := New("References: <1@x> <2@x>\r\n\r\n")
	if m.InReplyTo() != "2@x" {
		t.Errorf("InReplyTo fallback = %q, want 2@x", m.InReplyTo())
	}
}

func TestFoldedHeaderUnfolds(t *testing.T) {
	raw := "Subject: This is a very long subject line that\r\n" +
		" has been folded across multiple\r\n" +
		" lines\r\n" +
		"From: a@b.com\r\n\r\nbody"
	m := New(raw)
	want := "This is a very long subject line that has been folded across multiple lines"
	if m.Subject() != want {
		t.Errorf("unfolded subject = %q", m.Subject())
	}
}

func TestHeaderWithTabContinuation(t *testing.T) {
	raw := "Subject: A\r\n\tB\r\n\r\nbody"
	m := New(raw)
	if m.Subject() != "A B" {
		t.Errorf("tab continuation = %q", m.Subject())
	}
}

func TestLFOnlyMessage(t *testing.T) {
	raw := "Subject: LF only\nFrom: a@b.com\n\nbody line\n"
	m := New(raw)
	if m.Subject() != "LF only" {
		t.Errorf("subject = %q", m.Subject())
	}
	if m.Body().DecodedString() != "body line\n" {
		t.Errorf("body = %q", m.Body().DecodedString())
	}
}

func TestNoBlankLineIsAllHeader(t *testing.T) {
	m := New("Subject: only header")
	if m.Subject() != "only header" {
		t.Errorf("subject = %q", m.Subject())
	}
	if m.Body().DecodedString() != "" {
		t.Errorf("body should be empty, got %q", m.Body().DecodedString())
	}
}

func TestMalformedHeaderLineNoColon(t *testing.T) {
	m := New("This line has no colon\r\nSubject: ok\r\n\r\nbody")
	if m.Subject() != "ok" {
		t.Errorf("subject = %q", m.Subject())
	}
	// The nameless field is preserved.
	if len(m.Header().Fields()) != 2 {
		t.Errorf("expected 2 fields, got %d", len(m.Header().Fields()))
	}
}

func TestContinuationBeforeAnyField(t *testing.T) {
	// A leading continuation line with nothing to attach to must be ignored.
	m := New(" leading fold\r\nSubject: s\r\n\r\nb")
	if m.Subject() != "s" {
		t.Errorf("subject = %q", m.Subject())
	}
}

func TestEncodedWordBase64(t *testing.T) {
	m := New("Subject: =?utf-8?B?SGVsbG8gV29ybGQ=?=\r\n\r\nbody")
	if m.Subject() != "Hello World" {
		t.Errorf("B word = %q", m.Subject())
	}
}

func TestEncodedWordQ(t *testing.T) {
	m := New("Subject: =?utf-8?Q?H=C3=A9llo?=\r\n\r\nbody")
	if m.Subject() != "Héllo" {
		t.Errorf("Q word = %q", m.Subject())
	}
}

func TestEncodedWordUnderscoreSpace(t *testing.T) {
	m := New("Subject: =?UTF-8?Q?Hello_World?=\r\n\r\nbody")
	if m.Subject() != "Hello World" {
		t.Errorf("underscore = %q", m.Subject())
	}
}

func TestEncodedWordAdjacentFold(t *testing.T) {
	// Whitespace between two encoded-words is dropped.
	m := New("Subject: =?utf-8?B?SGVsbG8=?= =?utf-8?B?V29ybGQ=?=\r\n\r\nb")
	if m.Subject() != "HelloWorld" {
		t.Errorf("adjacent fold = %q", m.Subject())
	}
}

func TestEncodedWordWithSurroundingText(t *testing.T) {
	m := New("Subject: pre =?utf-8?B?SGk=?= post\r\n\r\nb")
	if m.Subject() != "pre Hi post" {
		t.Errorf("mixed = %q", m.Subject())
	}
}

func TestEncodedWordLatin1(t *testing.T) {
	// ISO-8859-1 é is 0xE9.
	got := decodeEncodedWords("=?iso-8859-1?Q?caf=E9?=")
	if got != "café" {
		t.Errorf("latin1 = %q", got)
	}
}

func TestEncodedWordUnknownCharset(t *testing.T) {
	// Unknown charset: bytes preserved (here ASCII so it is readable).
	got := decodeEncodedWords("=?x-weird?Q?abc?=")
	if got != "abc" {
		t.Errorf("unknown charset = %q", got)
	}
}

func TestEncodedWordInvalidPassThrough(t *testing.T) {
	// Not a real encoded word: no closing "?=".
	got := decodeEncodedWords("=?utf-8?B?nope")
	if got != "=?utf-8?B?nope" {
		t.Errorf("invalid passthrough = %q", got)
	}
	// Missing second "?".
	if g := decodeEncodedWords("=?utf-8?"); g != "=?utf-8?" {
		t.Errorf("truncated = %q", g)
	}
	// No "?" after charset.
	if g := decodeEncodedWords("=?utf-8"); g != "=?utf-8" {
		t.Errorf("no charset delim = %q", g)
	}
	// Unknown encoding letter.
	if g := decodeEncodedWords("=?utf-8?Z?xx?="); g != "=?utf-8?Z?xx?=" {
		t.Errorf("bad enc letter = %q", g)
	}
	// Bad base64.
	if g := decodeEncodedWords("=?utf-8?B?!!!?="); g != "=?utf-8?B?!!!?=" {
		t.Errorf("bad base64 = %q", g)
	}
	// Bad Q escape.
	if g := decodeEncodedWords("=?utf-8?Q?=ZZ?="); g != "=?utf-8?Q?=ZZ?=" {
		t.Errorf("bad Q escape = %q", g)
	}
	// Truncated Q escape.
	if g := decodeEncodedWords("=?utf-8?Q?=A?="); g != "=?utf-8?Q?=A?=" {
		t.Errorf("truncated Q = %q", g)
	}
	// enc letter present but no closing text delimiter.
	if g := decodeEncodedWords("=?u?B?"); g != "=?u?B?" {
		t.Errorf("no text delim = %q", g)
	}
}

func TestEncodedWordBase64LenientWhitespace(t *testing.T) {
	// Base64 with internal whitespace succeeds via the lenient retry.
	got := decodeEncodedWords("=?utf-8?B?SGVs bG8=?=")
	if got != "Hello" {
		t.Errorf("lenient b64 = %q", got)
	}
}

func TestNoEncodedWordPlainText(t *testing.T) {
	if g := decodeEncodedWords("plain text no words"); g != "plain text no words" {
		t.Errorf("plain = %q", g)
	}
}

func TestQuotedPrintableBody(t *testing.T) {
	m := New("Content-Transfer-Encoding: quoted-printable\r\n\r\nH=C3=A9llo=\r\n world")
	if string(m.Body().Decoded()) != "Héllo world" {
		t.Errorf("QP body = %q", string(m.Body().Decoded()))
	}
}

func TestQuotedPrintableLFSoftBreak(t *testing.T) {
	got := decodeQuotedPrintable("abc=\ndef")
	if string(got) != "abcdef" {
		t.Errorf("LF soft break = %q", got)
	}
}

func TestQuotedPrintableLiteralEquals(t *testing.T) {
	// A "=" not followed by valid hex is literal.
	got := decodeQuotedPrintable("a=b")
	if string(got) != "a=b" {
		t.Errorf("literal = %q", got)
	}
	// Trailing "=" with nothing after.
	if g := decodeQuotedPrintable("trailing="); string(g) != "trailing=" {
		t.Errorf("trailing = %q", g)
	}
}

func TestBase64Body(t *testing.T) {
	m := New("Content-Transfer-Encoding: base64\r\n\r\naGVsbG8gd29ybGQ=\r\n")
	if string(m.Body().Decoded()) != "hello world" {
		t.Errorf("b64 body = %q", string(m.Body().Decoded()))
	}
}

func TestBase64UnpaddedFallback(t *testing.T) {
	// Un-padded base64 falls back to RawStdEncoding.
	got := decodeBase64("aGVsbG8")
	if string(got) != "hello" {
		t.Errorf("unpadded = %q", got)
	}
	// Totally invalid: returns best-effort (empty).
	_ = decodeBase64("@@@@")
}

func TestSevenBitIdentity(t *testing.T) {
	m := New("Content-Transfer-Encoding: 7bit\r\n\r\nplain body")
	if string(m.Body().Decoded()) != "plain body" {
		t.Errorf("7bit = %q", string(m.Body().Decoded()))
	}
}

func TestMultipartMixed(t *testing.T) {
	raw := "From: a@b.com\r\n" +
		"Content-Type: multipart/mixed; boundary=\"BOUND\"\r\n\r\n" +
		"preamble text\r\n" +
		"--BOUND\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"Plain text part\r\n" +
		"--BOUND\r\n" +
		"Content-Type: text/html\r\n\r\n" +
		"<p>HTML part</p>\r\n" +
		"--BOUND\r\n" +
		"Content-Type: application/octet-stream\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"Content-Disposition: attachment; filename=\"file.bin\"\r\n\r\n" +
		"aGVsbG8gd29ybGQ=\r\n" +
		"--BOUND--\r\n" +
		"epilogue text\r\n"
	m := New(raw)
	if !m.Multipart() {
		t.Fatal("expected multipart")
	}
	if len(m.Parts()) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(m.Parts()))
	}
	if m.MimeType() != "multipart/mixed" {
		t.Errorf("mime = %q", m.MimeType())
	}
	if m.Parts()[0].MimeType() != "text/plain" {
		t.Errorf("part0 mime = %q", m.Parts()[0].MimeType())
	}
	if string(m.Parts()[0].Decoded()) != "Plain text part" {
		t.Errorf("part0 body = %q", string(m.Parts()[0].Decoded()))
	}
	atts := m.Attachments()
	if len(atts) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(atts))
	}
	if atts[0].Filename() != "file.bin" {
		t.Errorf("filename = %q", atts[0].Filename())
	}
	if string(atts[0].Decoded()) != "hello world" {
		t.Errorf("attachment = %q", string(atts[0].Decoded()))
	}
	if !strings.Contains(m.preamble, "preamble text") {
		t.Errorf("preamble = %q", m.preamble)
	}
	if !strings.Contains(m.epilogue, "epilogue text") {
		t.Errorf("epilogue = %q", m.epilogue)
	}
	if tp := m.TextPart(); tp == nil || tp.MimeType() != "text/plain" {
		t.Errorf("TextPart = %v", tp)
	}
	if hp := m.HTMLPart(); hp == nil || hp.MimeType() != "text/html" {
		t.Errorf("HTMLPart = %v", hp)
	}
}

func TestNestedMultipart(t *testing.T) {
	raw := "Content-Type: multipart/mixed; boundary=\"OUT\"\r\n\r\n" +
		"--OUT\r\n" +
		"Content-Type: multipart/alternative; boundary=\"IN\"\r\n\r\n" +
		"--IN\r\n" +
		"Content-Type: text/plain\r\n\r\ntext\r\n" +
		"--IN\r\n" +
		"Content-Type: text/html\r\n\r\n<p>html</p>\r\n" +
		"--IN--\r\n" +
		"--OUT\r\n" +
		"Content-Type: application/pdf\r\n" +
		"Content-Disposition: attachment; filename=\"doc.pdf\"\r\n\r\nPDFDATA\r\n" +
		"--OUT--\r\n"
	m := New(raw)
	if len(m.Parts()) != 2 {
		t.Fatalf("outer parts = %d", len(m.Parts()))
	}
	inner := m.Parts()[0]
	if len(inner.Parts()) != 2 {
		t.Fatalf("inner parts = %d", len(inner.Parts()))
	}
	// Attachment nested one level deep is found by the recursive walk.
	atts := m.Attachments()
	if len(atts) != 1 || atts[0].Filename() != "doc.pdf" {
		t.Errorf("nested attachments = %v", atts)
	}
	if hp := m.HTMLPart(); hp == nil {
		t.Error("nested HTMLPart not found")
	}
	if tp := m.TextPart(); tp == nil {
		t.Error("nested TextPart not found")
	}
}

func TestMultipartMissingCloseDelimiter(t *testing.T) {
	raw := "Content-Type: multipart/mixed; boundary=\"B\"\r\n\r\n" +
		"--B\r\nContent-Type: text/plain\r\n\r\nonly part\r\n"
	m := New(raw)
	if len(m.Parts()) != 1 {
		t.Fatalf("parts = %d", len(m.Parts()))
	}
}

func TestMultipartNoBoundaryParam(t *testing.T) {
	// multipart declared but no boundary: no parts, not multipart.
	m := New("Content-Type: multipart/mixed\r\n\r\nbody")
	if m.Multipart() {
		t.Error("should not be multipart without boundary")
	}
	if len(m.Parts()) != 0 {
		t.Errorf("parts = %d", len(m.Parts()))
	}
}

func TestClosingBoundaryFirst(t *testing.T) {
	// The very first delimiter is the closing one.
	raw := "Content-Type: multipart/mixed; boundary=\"B\"\r\n\r\n--B--\r\nepi\r\n"
	m := New(raw)
	if len(m.Parts()) != 0 {
		t.Errorf("parts = %d", len(m.Parts()))
	}
	if !strings.Contains(m.epilogue, "epi") {
		t.Errorf("epilogue = %q", m.epilogue)
	}
}

func TestFilenameFromContentTypeName(t *testing.T) {
	raw := "Content-Type: multipart/mixed; boundary=\"B\"\r\n\r\n" +
		"--B\r\nContent-Type: image/png; name=\"pic.png\"\r\n" +
		"Content-Disposition: inline\r\n\r\ndata\r\n--B--\r\n"
	m := New(raw)
	p := m.Parts()[0]
	if p.Filename() != "pic.png" {
		t.Errorf("name-derived filename = %q", p.Filename())
	}
	if p.ContentDisposition() != "inline" {
		t.Errorf("disposition = %q", p.ContentDisposition())
	}
	// Inline part carrying a filename is treated as an attachment.
	if !p.IsAttachment() {
		t.Error("named inline should be attachment")
	}
}

func TestContentIDAndCharset(t *testing.T) {
	raw := "Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-ID: <part1@x>\r\n\r\nbody"
	m := New(raw)
	if m.Charset() != "UTF-8" {
		t.Errorf("charset = %q", m.Charset())
	}
	if m.ContentID() != "part1@x" {
		t.Errorf("content-id = %q", m.ContentID())
	}
	if got := m.ContentTypeParameters()["charset"]; got != "UTF-8" {
		t.Errorf("param = %q", got)
	}
}

func TestNoFilenameNotAttachment(t *testing.T) {
	m := New("Content-Type: text/plain\r\n\r\nbody")
	if m.Filename() != "" {
		t.Errorf("filename = %q", m.Filename())
	}
	if m.IsAttachment() {
		t.Error("plain text is not an attachment")
	}
}

func TestBuilderBlock(t *testing.T) {
	m := New("", func(m *Message) {
		m.SetFrom("me@here.com").
			SetTo("you@there.com").
			SetSubject("Builder test").
			SetBody("Hello body")
	})
	if m.From()[0] != "me@here.com" || m.To()[0] != "you@there.com" {
		t.Errorf("builder addresses wrong: %v %v", m.From(), m.To())
	}
	if m.Subject() != "Builder test" {
		t.Errorf("subject = %q", m.Subject())
	}
	out := m.Encoded()
	if !strings.Contains(out, "From: me@here.com\r\n") {
		t.Errorf("encoded missing From:\n%s", out)
	}
	if !strings.HasSuffix(out, "\r\n\r\nHello body") {
		t.Errorf("encoded body wrong:\n%s", out)
	}
}

func TestAllSetters(t *testing.T) {
	when := time.Date(2026, 7, 1, 14, 4, 39, 0, time.FixedZone("", 2*3600))
	m := New("").
		SetFrom("f@x.com").
		SetTo("t@x.com").
		SetCc("c@x.com").
		SetBcc("b@x.com").
		SetReplyTo("r@x.com").
		SetSubject("Subj").
		SetMessageID("id@x").
		SetInReplyTo("<irt@x>").
		SetReferences("<1@x>").
		SetDate(when).
		SetContentType("text/plain").
		SetContentTransferEncoding("7bit").
		SetHeader("X-Custom", "custom").
		SetBody("body")

	if m.Cc()[0] != "c@x.com" || m.Bcc()[0] != "b@x.com" || m.ReplyTo()[0] != "r@x.com" {
		t.Error("cc/bcc/replyto setters")
	}
	if m.MessageID() != "id@x" {
		t.Errorf("message-id = %q (angle brackets should be added/stripped)", m.MessageID())
	}
	if !strings.Contains(m.Field("Message-ID"), "<id@x>") {
		t.Errorf("stored message-id = %q", m.Field("Message-ID"))
	}
	if m.InReplyTo() != "irt@x" {
		t.Errorf("in-reply-to = %q", m.InReplyTo())
	}
	if m.References()[0] != "1@x" {
		t.Errorf("references = %v", m.References())
	}
	d, ok := m.Date()
	if !ok || d.Hour() != 14 {
		t.Errorf("date = %v ok=%v", d, ok)
	}
	if m.Field("X-Custom") != "custom" {
		t.Errorf("custom = %q", m.Field("X-Custom"))
	}
	if m.body.Encoding != "7bit" {
		t.Errorf("body encoding = %q", m.body.Encoding)
	}
}

func TestSetMessageIDAlreadyBracketed(t *testing.T) {
	m := New("").SetMessageID("<already@x>")
	if m.Field("Message-ID") != "<already@x>" {
		t.Errorf("bracketed = %q", m.Field("Message-ID"))
	}
	// Empty stays empty (removed).
	m.SetMessageID("")
	if m.Field("Message-ID") != "" {
		t.Errorf("empty message-id = %q", m.Field("Message-ID"))
	}
}

func TestSetDateString(t *testing.T) {
	m := New("").SetDateString("Wed, 01 Jul 2026 14:04:39 +0200")
	d, ok := m.Date()
	if !ok || d.Day() != 1 {
		t.Errorf("date string = %v %v", d, ok)
	}
}

func TestSetBodyOnFreshMessage(t *testing.T) {
	m := &Message{header: &Header{}}
	m.SetBody("hi")
	if m.Body().Raw != "hi" {
		t.Errorf("body = %q", m.Body().Raw)
	}
}

func TestHeaderSetReplacesAndRemoves(t *testing.T) {
	m := New("Subject: old\r\nX: keep\r\n\r\nb")
	m.SetSubject("new")
	if m.Subject() != "new" {
		t.Errorf("replace = %q", m.Subject())
	}
	// Removing via set("") drops it.
	m.header.set("Subject", "")
	if m.Field("Subject") != "" {
		t.Errorf("remove = %q", m.Field("Subject"))
	}
	if m.Field("X") != "keep" {
		t.Errorf("other field lost = %q", m.Field("X"))
	}
}

func TestHeaderGetAllAndAdd(t *testing.T) {
	m := New("Received: a\r\nReceived: b\r\n\r\nbody")
	if all := m.Header().GetAll("received"); len(all) != 2 {
		t.Fatalf("GetAll = %d", len(all))
	}
	m.AddHeader("Received", "c")
	if all := m.Header().GetAll("Received"); len(all) != 3 {
		t.Errorf("after add = %d", len(all))
	}
	if m.Header().Get("nonexistent") != nil {
		t.Error("Get(nonexistent) should be nil")
	}
	if m.Header().GetAll("nonexistent") != nil {
		t.Error("GetAll(nonexistent) should be nil")
	}
}

func TestFieldDecodedAndString(t *testing.T) {
	f := &Field{Name: "Subject", Value: "=?utf-8?B?SGk=?="}
	if f.Decoded() != "Hi" {
		t.Errorf("decoded = %q", f.Decoded())
	}
	if f.String() != "Subject: =?utf-8?B?SGk=?=" {
		t.Errorf("string = %q", f.String())
	}
}

func TestEncodedRoundTrip(t *testing.T) {
	raw := "From: a@b.com\r\nTo: c@d.com\r\nSubject: Hi\r\n\r\nHello\r\n"
	m := New(raw)
	out := m.Encoded()
	m2 := New(out)
	if m2.Subject() != "Hi" || m2.From()[0] != "a@b.com" {
		t.Errorf("round trip lost data: %q %v", m2.Subject(), m2.From())
	}
	if m.String() != out {
		t.Error("String != Encoded")
	}
}

func TestEncodeSubjectNonASCII(t *testing.T) {
	m := New("").SetSubject("Héllo Wörld")
	out := m.Encoded()
	if !strings.Contains(out, "=?UTF-8?Q?H=C3=A9llo_W=C3=B6rld?=") {
		t.Errorf("subject not encoded:\n%s", out)
	}
	// It must decode back.
	m2 := New(out)
	if m2.Subject() != "Héllo Wörld" {
		t.Errorf("decode back = %q", m2.Subject())
	}
}

func TestFoldLongHeader(t *testing.T) {
	long := strings.Repeat("word ", 40)
	m := New("").SetHeader("X-Long", strings.TrimSpace(long))
	out := m.Encoded()
	for _, ln := range strings.Split(out, "\r\n") {
		if len(ln) > 78 {
			t.Errorf("line too long (%d): %q", len(ln), ln)
		}
	}
	// The folded value unfolds back.
	m2 := New(out)
	if m2.Field("X-Long") != strings.TrimSpace(long) {
		t.Errorf("unfold mismatch")
	}
}

func TestFoldShortHeaderUnchanged(t *testing.T) {
	line := foldField("To", "a@b.com")
	if line != "To: a@b.com" {
		t.Errorf("short fold = %q", line)
	}
}

func TestFoldHeaderWithEmbeddedNewlineFolds(t *testing.T) {
	// A value already containing a newline triggers the fold path.
	line := foldField("X", "one two three")
	if strings.Contains(line, "\r\n") {
		// short, should not fold
		t.Errorf("unexpected fold = %q", line)
	}
}

func TestMultipartEncoded(t *testing.T) {
	raw := "Content-Type: multipart/mixed; boundary=\"B\"\r\n\r\n" +
		"pre\r\n--B\r\nContent-Type: text/plain\r\n\r\npart1\r\n" +
		"--B\r\nContent-Type: text/plain\r\n\r\npart2\r\n--B--\r\nepi\r\n"
	m := New(raw)
	out := m.Encoded()
	if !strings.Contains(out, "--B\r\n") || !strings.Contains(out, "--B--\r\n") {
		t.Errorf("boundaries missing:\n%s", out)
	}
	if !strings.Contains(out, "part1") || !strings.Contains(out, "part2") {
		t.Errorf("parts missing:\n%s", out)
	}
	if !strings.Contains(out, "pre") || !strings.Contains(out, "epi") {
		t.Errorf("preamble/epilogue missing:\n%s", out)
	}
	// Re-parse.
	m2 := New(out)
	if len(m2.Parts()) != 2 {
		t.Errorf("re-parsed parts = %d", len(m2.Parts()))
	}
}

func TestMultipartEncodedDefaultBoundary(t *testing.T) {
	// A message with parts but no boundary param falls back to "boundary".
	m := &Message{header: &Header{}}
	m.SetContentType("multipart/mixed")
	m.AddPart(New("Content-Type: text/plain\r\n\r\nx"))
	out := m.bodyEncoded()
	if !strings.Contains(out, "--boundary\r\n") {
		t.Errorf("default boundary missing:\n%s", out)
	}
}

func TestAddPart(t *testing.T) {
	m := New("")
	m.AddPart(New("Content-Type: text/plain\r\n\r\nx"))
	if len(m.Parts()) != 1 {
		t.Errorf("parts = %d", len(m.Parts()))
	}
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "msg.eml")
	raw := "From: a@b.com\r\nSubject: from file\r\n\r\nbody\r\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject() != "from file" {
		t.Errorf("subject = %q", m.Subject())
	}
	if _, err := Read(filepath.Join(dir, "missing.eml")); err == nil {
		t.Error("expected error reading missing file")
	}
}

func TestTextPartOnPlainMessage(t *testing.T) {
	m := New("Content-Type: text/plain\r\n\r\nhi")
	if m.TextPart() != m {
		t.Error("TextPart of plain message should be itself")
	}
	// A plain message with no content type is still text.
	m2 := New("Subject: x\r\n\r\nbody")
	if m2.TextPart() != m2 {
		t.Error("no-CT message TextPart should be itself")
	}
	if m2.HTMLPart() != nil {
		t.Error("no-CT message has no HTMLPart")
	}
}
