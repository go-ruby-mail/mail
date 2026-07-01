// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mail

import "strings"

// parseParams splits an RFC 2045 parameterised value (e.g. a Content-Type or
// Content-Disposition body) into its main value and a parameter map. The main
// value is everything before the first ";"; each remaining ";"-separated token
// is an "attribute=value" pair (value optionally quoted). Attribute names are
// lower-cased for lookup. Mirrors how Mail exposes content_type_parameters.
func parseParams(s string) (main string, params map[string]string) {
	params = map[string]string{}
	// splitSemicolons always yields at least one token (the text before the
	// first ";"), so toks[0] is the media/main value.
	toks := splitSemicolons(s)
	main = strings.TrimSpace(toks[0])
	for _, t := range toks[1:] {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		eq := strings.IndexByte(t, '=')
		if eq < 0 {
			params[strings.ToLower(t)] = ""
			continue
		}
		k := strings.ToLower(strings.TrimSpace(t[:eq]))
		v := strings.TrimSpace(t[eq+1:])
		v = unquoteParam(v)
		params[k] = v
	}
	return main, params
}

// splitSemicolons splits on ";" not inside double quotes.
func splitSemicolons(s string) []string {
	var toks []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQuote = !inQuote
			cur.WriteByte(c)
		case c == ';' && !inQuote:
			toks = append(toks, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	toks = append(toks, cur.String())
	return toks
}

// unquoteParam strips surrounding double quotes and backslash escapes from a
// parameter value.
func unquoteParam(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		inner := v[1 : len(v)-1]
		var b strings.Builder
		for i := 0; i < len(inner); i++ {
			if inner[i] == '\\' && i+1 < len(inner) {
				i++
			}
			b.WriteByte(inner[i])
		}
		return b.String()
	}
	return v
}

// formatParams renders a main value plus parameters back to an RFC 2045 field
// body, quoting any value that needs it. Parameter keys are emitted in the given
// order for determinism.
func formatParams(main string, order []string, params map[string]string) string {
	var b strings.Builder
	b.WriteString(main)
	for _, k := range order {
		v, ok := params[k]
		if !ok {
			continue
		}
		b.WriteString("; ")
		b.WriteString(k)
		b.WriteByte('=')
		if needsParamQuote(v) {
			b.WriteByte('"')
			b.WriteString(v)
			b.WriteByte('"')
		} else {
			b.WriteString(v)
		}
	}
	return b.String()
}

// needsParamQuote reports whether a parameter value must be double-quoted.
func needsParamQuote(v string) bool {
	if v == "" {
		return true
	}
	return strings.ContainsAny(v, " \t()<>@,;:\\\"/[]?=")
}
