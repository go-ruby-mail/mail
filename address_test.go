// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mail

import (
	"reflect"
	"testing"
)

func TestAddressAngleAddr(t *testing.T) {
	a := NewAddress("John Doe <john@example.com>")
	if a.DisplayName != "John Doe" {
		t.Errorf("display = %q", a.DisplayName)
	}
	if a.Local != "john" || a.Domain != "example.com" {
		t.Errorf("local/domain = %q/%q", a.Local, a.Domain)
	}
	if a.Address() != "john@example.com" {
		t.Errorf("address = %q", a.Address())
	}
	if a.Format() != "John Doe <john@example.com>" {
		t.Errorf("format = %q", a.Format())
	}
	if a.String() != a.Format() {
		t.Error("String != Format")
	}
}

func TestAddressBareAddr(t *testing.T) {
	a := NewAddress("bare@example.com")
	if a.DisplayName != "" {
		t.Errorf("display = %q", a.DisplayName)
	}
	if a.Address() != "bare@example.com" {
		t.Errorf("address = %q", a.Address())
	}
	if a.Format() != "bare@example.com" {
		t.Errorf("format bare = %q", a.Format())
	}
}

func TestAddressLocalOnly(t *testing.T) {
	a := NewAddress("justlocal")
	if a.Local != "justlocal" || a.Domain != "" {
		t.Errorf("local-only = %q/%q", a.Local, a.Domain)
	}
	if a.Address() != "justlocal" {
		t.Errorf("address = %q", a.Address())
	}
}

func TestAddressEmpty(t *testing.T) {
	a := NewAddress("")
	if a.Address() != "" {
		t.Errorf("empty address = %q", a.Address())
	}
	if a.Format() != "" {
		t.Errorf("empty format = %q", a.Format())
	}
}

func TestAddressComment(t *testing.T) {
	a := NewAddress("john@example.com (Johnny)")
	if a.Address() != "john@example.com" {
		t.Errorf("address = %q", a.Address())
	}
	if !reflect.DeepEqual(a.Comments, []string{"Johnny"}) {
		t.Errorf("comments = %v", a.Comments)
	}
}

func TestAddressNestedComment(t *testing.T) {
	a := NewAddress("x@y.com (outer (inner) tail)")
	if len(a.Comments) != 1 || a.Comments[0] != "outer (inner) tail" {
		t.Errorf("nested comment = %v", a.Comments)
	}
}

func TestAddressQuotedDisplayName(t *testing.T) {
	a := NewAddress(`"Doe, John" <john@x.com>`)
	if a.DisplayName != "Doe, John" {
		t.Errorf("quoted display = %q", a.DisplayName)
	}
	// It re-quotes on format because of the comma.
	if a.Format() != `"Doe, John" <john@x.com>` {
		t.Errorf("re-quote = %q", a.Format())
	}
}

func TestAddressEscapedQuoteInDisplay(t *testing.T) {
	a := NewAddress(`"a\"b" <x@y.com>`)
	if a.DisplayName != `a"b` {
		t.Errorf("escaped = %q", a.DisplayName)
	}
}

func TestAddressEncodedWordDisplayName(t *testing.T) {
	a := NewAddress("=?utf-8?B?SsO2cmc=?= <j@x.com>")
	if a.DisplayName != "Jörg" {
		t.Errorf("encoded display = %q", a.DisplayName)
	}
	// It re-encodes on format because of the non-ASCII.
	if a.Format() != "=?UTF-8?Q?J=C3=B6rg?= <j@x.com>" {
		t.Errorf("re-encode = %q", a.Format())
	}
}

func TestAddressUnclosedAngle(t *testing.T) {
	// No closing ">" — falls through to bare addr-spec handling.
	a := NewAddress("name <john@x.com")
	if a.Address() != "name <john@x.com" && a.Local == "" {
		t.Errorf("unclosed angle = %#v", a)
	}
}

func TestAddressList(t *testing.T) {
	al := NewAddressList("a@x.com, b@y.com, John <c@z.com>")
	got := make([]string, 0)
	for _, a := range al.Addresses() {
		got = append(got, a.Address())
	}
	if !reflect.DeepEqual(got, []string{"a@x.com", "b@y.com", "c@z.com"}) {
		t.Errorf("list = %v", got)
	}
}

func TestAddressListGroup(t *testing.T) {
	al := NewAddressList("group: a@x.com, b@x.com;, single@y.com")
	got := make([]string, 0)
	for _, a := range al.Addresses() {
		got = append(got, a.Address())
	}
	if !reflect.DeepEqual(got, []string{"a@x.com", "b@x.com", "single@y.com"}) {
		t.Errorf("group list = %v", got)
	}
}

func TestAddressListQuotedComma(t *testing.T) {
	// A comma inside a quoted display name must not split.
	al := NewAddressList(`"Last, First" <a@x.com>, b@y.com`)
	if len(al.Addresses()) != 2 {
		t.Fatalf("quoted comma split wrong: %d", len(al.Addresses()))
	}
	if al.Addresses()[0].DisplayName != "Last, First" {
		t.Errorf("display = %q", al.Addresses()[0].DisplayName)
	}
}

func TestAddressListCommaInComment(t *testing.T) {
	// A comma inside a parenthesised comment must not split.
	al := NewAddressList("a@x.com (one, two), b@y.com")
	if len(al.Addresses()) != 2 {
		t.Fatalf("comment comma split wrong: %d", len(al.Addresses()))
	}
}

func TestAddressListCommaInAngle(t *testing.T) {
	al := NewAddressList("<a@x.com>, <b@y.com>")
	if len(al.Addresses()) != 2 {
		t.Fatalf("angle list = %d", len(al.Addresses()))
	}
}

func TestAddressListEmptyAndBlankTokens(t *testing.T) {
	al := NewAddressList("a@x.com, , b@y.com")
	if len(al.Addresses()) != 2 {
		t.Errorf("blank token = %d", len(al.Addresses()))
	}
	if len(NewAddressList("").Addresses()) != 0 {
		t.Error("empty list should yield nothing")
	}
}

func TestQuotePhrasePlain(t *testing.T) {
	// No specials, no encoding: bare.
	if got := quotePhrase("John"); got != "John" {
		t.Errorf("plain quote = %q", got)
	}
}
