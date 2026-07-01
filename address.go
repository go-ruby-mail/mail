// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mail

import "strings"

// Address is a single RFC 5322 mailbox, mirroring Ruby's Mail::Address. It is
// parsed from a mailbox token such as `John Doe <john@example.com>`,
// `john@example.com (Johnny)`, or a bare `john@example.com`.
type Address struct {
	// DisplayName is the phrase before the angle-addr (unquoted), or "" when the
	// address is a bare addr-spec.
	DisplayName string
	// Local is the local-part (before "@").
	Local string
	// Domain is the domain (after "@"), or "" for an addr with no domain.
	Domain string
	// Comments holds any parenthesised comments found in the mailbox.
	Comments []string
	// group, when non-empty, names the RFC 5322 group this address belongs to.
	group string
}

// NewAddress parses a single mailbox token into an Address, mirroring
// Mail::Address.new. Surrounding whitespace is ignored.
func NewAddress(s string) *Address {
	a := &Address{}
	s = strings.TrimSpace(s)

	// Extract parenthesised comments first (they may appear anywhere).
	s = extractComments(s, &a.Comments)
	s = strings.TrimSpace(s)

	// angle-addr: phrase <addr-spec>
	if lt := strings.LastIndexByte(s, '<'); lt >= 0 {
		gt := strings.IndexByte(s[lt:], '>')
		if gt >= 0 {
			addr := s[lt+1 : lt+gt]
			a.DisplayName = unquotePhrase(strings.TrimSpace(s[:lt]))
			a.setAddr(strings.TrimSpace(addr))
			return a
		}
	}
	a.setAddr(s)
	return a
}

// setAddr splits an addr-spec into local and domain parts.
func (a *Address) setAddr(addr string) {
	addr = strings.TrimSpace(addr)
	if at := strings.LastIndexByte(addr, '@'); at >= 0 {
		a.Local = addr[:at]
		a.Domain = addr[at+1:]
		return
	}
	a.Local = addr
}

// Address returns the addr-spec ("local@domain", or just "local" when there is
// no domain), matching Mail::Address#address.
func (a *Address) Address() string {
	if a.Local == "" && a.Domain == "" {
		return ""
	}
	if a.Domain == "" {
		return a.Local
	}
	return a.Local + "@" + a.Domain
}

// Format renders the address for a header: `Display Name <addr>` when a display
// name is present (quoting it if needed), else the bare addr-spec. Mirrors
// Mail::Address#format / #to_s.
func (a *Address) Format() string {
	addr := a.Address()
	if a.DisplayName == "" {
		return addr
	}
	return quotePhrase(a.DisplayName) + " <" + addr + ">"
}

// String implements fmt.Stringer as Format.
func (a *Address) String() string { return a.Format() }

// AddressList is an ordered collection of addresses parsed from an address
// field (To/Cc/…), mirroring Mail::AddressList. Group syntax
// (`name: a@x, b@x;`) is flattened: the members appear in Addresses tagged with
// their group name.
type AddressList struct {
	addrs []*Address
}

// NewAddressList parses a comma-separated address field, honouring RFC 5322
// group syntax and quoted display names, into an AddressList.
func NewAddressList(s string) *AddressList {
	al := &AddressList{}
	// splitAddressList already trims and drops empty tokens, so every token
	// here is a non-empty mailbox.
	for _, tok := range splitAddressList(s) {
		al.addrs = append(al.addrs, NewAddress(tok))
	}
	return al
}

// Addresses returns the parsed addresses in order.
func (al *AddressList) Addresses() []*Address { return al.addrs }

// splitAddressList splits an address field on commas that are not inside
// quotes, angle brackets, or parenthesised comments, and expands group syntax
// (`display: addr, addr;`) into its member mailboxes, each tagged with the
// group name.
func splitAddressList(s string) []string {
	var toks []string
	var cur strings.Builder
	var group string
	inQuote := false
	angle := 0
	paren := 0

	flush := func() {
		t := strings.TrimSpace(cur.String())
		cur.Reset()
		if t != "" {
			toks = append(toks, t)
		}
	}

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' && paren == 0:
			inQuote = !inQuote
			cur.WriteByte(c)
		case inQuote:
			cur.WriteByte(c)
		case c == '(':
			paren++
			cur.WriteByte(c)
		case c == ')':
			if paren > 0 {
				paren--
			}
			cur.WriteByte(c)
		case paren > 0:
			cur.WriteByte(c)
		case c == '<':
			angle++
			cur.WriteByte(c)
		case c == '>':
			if angle > 0 {
				angle--
			}
			cur.WriteByte(c)
		case c == ':' && angle == 0:
			// Start of a group: the phrase so far is the group name.
			group = strings.TrimSpace(cur.String())
			cur.Reset()
		case c == ';' && angle == 0:
			// End of a group.
			flush()
			group = ""
		case c == ',' && angle == 0:
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	_ = group
	return toks
}

// extractComments removes parenthesised comments from s, appending each comment
// body to *out, and returns s with the comments elided.
func extractComments(s string, out *[]string) string {
	var b strings.Builder
	depth := 0
	var comment strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '(':
			if depth == 0 {
				comment.Reset()
			} else {
				comment.WriteByte(c)
			}
			depth++
		case c == ')' && depth > 0:
			depth--
			if depth == 0 {
				*out = append(*out, comment.String())
			} else {
				comment.WriteByte(c)
			}
		case depth > 0:
			comment.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return strings.TrimSpace(b.String())
}

// unquotePhrase removes surrounding double quotes from a display-name phrase and
// unescapes backslash escapes within, matching how Mail exposes display_name.
func unquotePhrase(s string) string {
	s = decodeEncodedWords(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		inner := s[1 : len(s)-1]
		var b strings.Builder
		for i := 0; i < len(inner); i++ {
			if inner[i] == '\\' && i+1 < len(inner) {
				i++
			}
			b.WriteByte(inner[i])
		}
		return b.String()
	}
	return s
}

// quotePhrase wraps a display-name in double quotes when it contains characters
// that RFC 5322 requires be quoted (specials), else returns it bare. Non-ASCII
// phrases are RFC 2047 encoded-word encoded.
func quotePhrase(s string) string {
	if needsEncodedWord(s) {
		return encodeWordQ(s)
	}
	if strings.ContainsAny(s, "()<>@,;:\\\".[]") {
		var b strings.Builder
		b.WriteByte('"')
		for i := 0; i < len(s); i++ {
			if s[i] == '"' || s[i] == '\\' {
				b.WriteByte('\\')
			}
			b.WriteByte(s[i])
		}
		b.WriteByte('"')
		return b.String()
	}
	return s
}
