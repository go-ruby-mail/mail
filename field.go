// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mail

import "strings"

// Field is a single header field (a name and its unfolded value), mirroring the
// role of Mail::Field. The stored Value is already unfolded but not yet
// encoded-word decoded; Decoded returns the human-readable form.
type Field struct {
	Name  string // canonical field name, e.g. "Subject"
	Value string // unfolded raw value (whitespace-collapsed continuation)
}

// Decoded returns the field value with RFC 2047 encoded-words decoded, matching
// Mail::Field#decoded for unstructured fields.
func (f *Field) Decoded() string { return decodeEncodedWords(f.Value) }

// String returns "Name: Value" without folding, for debugging.
func (f *Field) String() string { return f.Name + ": " + f.Value }

// Header is an ordered list of fields plus a case-insensitive lookup, mirroring
// Mail::Header. Multiple fields with the same name are preserved in order.
type Header struct {
	fields []*Field
}

// Fields returns the header's fields in order.
func (h *Header) Fields() []*Field { return h.fields }

// Get returns the first field whose name matches (case-insensitively), or nil.
func (h *Header) Get(name string) *Field {
	name = strings.ToLower(name)
	for _, f := range h.fields {
		if strings.ToLower(f.Name) == name {
			return f
		}
	}
	return nil
}

// GetAll returns every field whose name matches (case-insensitively).
func (h *Header) GetAll(name string) []*Field {
	name = strings.ToLower(name)
	var out []*Field
	for _, f := range h.fields {
		if strings.ToLower(f.Name) == name {
			out = append(out, f)
		}
	}
	return out
}

// set replaces every field named name with a single field of value v; if none
// exists it is appended. Passing "" removes the field entirely.
func (h *Header) set(name, v string) {
	lower := strings.ToLower(name)
	kept := h.fields[:0]
	replaced := false
	for _, f := range h.fields {
		if strings.ToLower(f.Name) == lower {
			if !replaced && v != "" {
				kept = append(kept, &Field{Name: name, Value: v})
				replaced = true
			}
			continue
		}
		kept = append(kept, f)
	}
	h.fields = kept
	if !replaced && v != "" {
		h.fields = append(h.fields, &Field{Name: name, Value: v})
	}
}

// add appends a field without removing existing same-named fields.
func (h *Header) add(name, v string) {
	h.fields = append(h.fields, &Field{Name: name, Value: v})
}

// value returns the unfolded raw value of the first field named name, or "".
func (h *Header) value(name string) string {
	if f := h.Get(name); f != nil {
		return f.Value
	}
	return ""
}

// parseHeader splits the header block (already separated from the body) into
// ordered Fields, unfolding continuation lines (a line starting with space or
// tab continues the previous field). It stops at the first blank line handled by
// the caller, so head here contains only header lines.
func parseHeader(head string) *Header {
	h := &Header{}
	lines := splitLines(head)
	var name, value string
	have := false

	commit := func() {
		if have {
			h.fields = append(h.fields, &Field{Name: name, Value: value})
		}
	}

	for _, ln := range lines {
		if ln == "" {
			continue
		}
		if ln[0] == ' ' || ln[0] == '\t' {
			// Continuation (folded) line: unfold by replacing the fold (CRLF +
			// leading WSP) with a single space, per RFC 5322 §2.2.3.
			if have {
				value += " " + strings.TrimLeft(ln, " \t")
			}
			continue
		}
		commit()
		colon := strings.IndexByte(ln, ':')
		if colon < 0 {
			// Malformed line with no colon; treat the whole line as a nameless
			// field so nothing is silently dropped.
			name, value, have = ln, "", true
			continue
		}
		name = strings.TrimSpace(ln[:colon])
		value = strings.TrimSpace(ln[colon+1:])
		have = true
	}
	commit()
	return h
}

// splitLines splits s on CRLF or LF into lines without their terminators.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
