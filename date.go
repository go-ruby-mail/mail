// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mail

import (
	"strings"
	"time"
)

// dateLayouts are the RFC 5322 / RFC 2822 date formats the parser accepts, in
// order of preference. The gem tolerates an optional leading day-of-week and a
// numeric or named zone; these layouts cover the forms that appear in real mail.
var dateLayouts = []string{
	"Mon, 02 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"02 Jan 2006 15:04:05 -0700",
	"2 Jan 2006 15:04:05 -0700",
	"Mon, 02 Jan 2006 15:04:05 MST",
	"Mon, 2 Jan 2006 15:04:05 MST",
	"Mon, 02 Jan 2006 15:04 -0700",
	"Mon, 2 Jan 2006 15:04 -0700",
	"02 Jan 2006 15:04:05 MST",
}

// parseDate parses an RFC 5322 Date header value, mirroring the tolerance of
// Mail's date parsing. The bool reports success.
func parseDate(v string) (time.Time, bool) {
	v = strings.TrimSpace(collapseWS(v))
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, v); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// formatDate renders a time in the RFC 5322 Date header format the `mail` gem
// emits (e.g. "Wed, 01 Jul 2026 14:04:39 +0200").
func formatDate(t time.Time) string {
	return t.Format("Mon, 02 Jan 2006 15:04:05 -0700")
}

// collapseWS collapses runs of ASCII whitespace to a single space.
func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
