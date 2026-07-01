// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

// This file drives the last few branches to keep coverage at 100% without a
// Ruby runtime: defensive paths, nested recursion misses, and edge tokens the
// broader behavioural tests do not naturally hit.

package mail

import (
	"strings"
	"testing"
)

func TestFindPartNestedNoMatch(t *testing.T) {
	// An outer multipart whose only sub-multipart contains no matching type:
	// exercises the recursive walk's "return nil" leaf.
	raw := "Content-Type: multipart/mixed; boundary=\"OUT\"\r\n\r\n" +
		"--OUT\r\n" +
		"Content-Type: multipart/related; boundary=\"IN\"\r\n\r\n" +
		"--IN\r\n" +
		"Content-Type: image/png\r\n\r\ndata\r\n" +
		"--IN--\r\n" +
		"--OUT--\r\n"
	m := New(raw)
	if m.HTMLPart() != nil {
		t.Error("no html part should be found in nested tree")
	}
	if m.TextPart() != nil {
		t.Error("no text part should be found in nested tree")
	}
}

func TestAddressListTrailingCommaEmptyToken(t *testing.T) {
	// A trailing comma leaves an empty token that must be skipped by the
	// tok=="" continue in NewAddressList.
	al := NewAddressList("a@x.com,")
	if len(al.Addresses()) != 1 {
		t.Errorf("trailing comma addresses = %d", len(al.Addresses()))
	}
}

func TestQuotePhraseEscapesQuoteAndBackslash(t *testing.T) {
	// Display name containing a quote and a backslash and a special: must be
	// wrapped in quotes with both escaped.
	got := quotePhrase(`a"b\c;d`)
	if got != `"a\"b\\c;d"` {
		t.Errorf("quotePhrase = %q", got)
	}
}

func TestParseHeaderInternalBlankLine(t *testing.T) {
	// A header block that itself contains a blank line (before the true
	// header/body split) exercises the empty-line continue in parseHeader.
	h := parseHeader("A: 1\n\nB: 2")
	if h.value("A") != "1" || h.value("B") != "2" {
		t.Errorf("internal blank line header = %v", h.fields)
	}
}

func TestNewRawWithBuilder(t *testing.T) {
	// New with a raw message AND a builder: the builder mutates the parsed
	// message (mail.go builder loop after parseMessage).
	m := New("Subject: orig\r\n\r\nbody", func(m *Message) {
		m.SetSubject("changed")
	})
	if m.Subject() != "changed" {
		t.Errorf("builder on parsed = %q", m.Subject())
	}
	if !strings.Contains(m.Body().Raw, "body") {
		t.Errorf("body lost = %q", m.Body().Raw)
	}
}
