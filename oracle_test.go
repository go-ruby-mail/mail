// Copyright (c) the go-ruby-mail/mail authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mail

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// rubyBin locates a usable `ruby` with the `mail` gem once. The oracle tests
// skip themselves when it is absent (the qemu cross-arch lanes and the Windows
// lane), so the deterministic suite alone drives the 100% gate there. The gem is
// version-gated to Ruby >= 4.0 to match the reference the port targets.
func rubyBin(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("ruby")
	if err != nil {
		t.Skip("ruby not on PATH; skipping mail-gem oracle")
	}
	// Require Ruby >= 4.0 and the mail gem.
	out, err := exec.Command(path, "-e",
		`exit(RUBY_VERSION >= "4.0" ? 0 : 3)`).CombinedOutput()
	if err != nil {
		t.Skipf("ruby present but not >= 4.0 (%s); skipping oracle", strings.TrimSpace(string(out)))
	}
	if err := exec.Command(path, "-e", `require "mail"`).Run(); err != nil {
		t.Skip("mail gem not installed; skipping oracle")
	}
	return path
}

// rubyEval runs a mail-gem script and returns its stdout. The script must
// $stdout.binmode itself so Windows text-mode does not pollute the bytes.
func rubyEval(t *testing.T, bin, script string) string {
	t.Helper()
	cmd := exec.Command(bin, "-rmail", "-e", "$stdout.binmode\n"+script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ruby error: %v\nscript:\n%s\noutput:\n%s", err, script, out)
	}
	return string(out)
}

// heredoc wraps raw as a Ruby heredoc literal assigned to `raw`.
func heredoc(raw string) string {
	// Ensure the terminator sits on its own line.
	if !strings.HasSuffix(raw, "\n") {
		raw += "\n"
	}
	return "raw = <<'__MSG__'\n" + raw + "__MSG__\n"
}

// TestOracleParseMatchesGem parses a corpus here and in the `mail` gem, and
// asserts our accessors agree with the gem's for from/to/subject/message-id and
// the body decode.
func TestOracleParseMatchesGem(t *testing.T) {
	bin := rubyBin(t)

	cases := []struct {
		name string
		raw  string
	}{
		{
			name: "basic",
			raw: "From: John <john@example.com>\r\n" +
				"To: a@x.com, b@y.com\r\n" +
				"Subject: Hello there\r\n" +
				"Message-ID: <abc@example.com>\r\n\r\n" +
				"Body text here\r\n",
		},
		{
			name: "encoded-word-B",
			raw:  "From: s@x.com\r\nSubject: =?utf-8?B?SGVsbG8gV29ybGQ=?=\r\n\r\nbody",
		},
		{
			name: "encoded-word-Q",
			raw:  "From: s@x.com\r\nSubject: =?utf-8?Q?H=C3=A9llo?=\r\n\r\nbody",
		},
		{
			name: "folded-subject",
			raw: "Subject: This is a very long subject line that\r\n" +
				" has been folded across\r\n lines\r\nFrom: a@b.com\r\n\r\nbody",
		},
		{
			name: "cc-reply-to",
			raw:  "From: a@x.com\r\nCc: c@x.com, c2@x.com\r\nReply-To: r@x.com\r\n\r\nbody",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := New(c.raw)
			script := heredoc(c.raw) +
				"m = Mail.new(raw)\n" +
				"puts m.from.inspect\n" +
				"puts m.to.inspect\n" +
				"puts m.cc.inspect\n" +
				"puts m.reply_to.inspect\n" +
				"puts m.subject.inspect\n" +
				"puts m.message_id.inspect\n"
			lines := strings.Split(strings.TrimRight(rubyEval(t, bin, script), "\n"), "\n")
			if len(lines) != 6 {
				t.Fatalf("unexpected oracle output: %q", lines)
			}
			assertArrayEq(t, "from", m.From(), lines[0])
			assertArrayEq(t, "to", m.To(), lines[1])
			assertArrayEq(t, "cc", m.Cc(), lines[2])
			assertArrayEq(t, "reply_to", m.ReplyTo(), lines[3])
			assertScalarEq(t, "subject", m.Subject(), lines[4])
			assertScalarEq(t, "message_id", m.MessageID(), lines[5])
		})
	}
}

// TestOracleMultipartMatchesGem parses a multipart message with an attachment
// here and in the gem, and asserts the part count, filenames and decoded
// attachment bytes agree.
func TestOracleMultipartMatchesGem(t *testing.T) {
	bin := rubyBin(t)
	raw := "From: a@b.com\r\n" +
		"Content-Type: multipart/mixed; boundary=\"BOUND\"\r\n\r\n" +
		"--BOUND\r\nContent-Type: text/plain\r\n\r\nPlain text part\r\n" +
		"--BOUND\r\nContent-Type: text/html\r\n\r\n<p>HTML part</p>\r\n" +
		"--BOUND\r\n" +
		"Content-Type: application/octet-stream\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"Content-Disposition: attachment; filename=\"file.bin\"\r\n\r\n" +
		"aGVsbG8gd29ybGQ=\r\n--BOUND--\r\n"

	m := New(raw)
	script := heredoc(raw) +
		"m = Mail.new(raw)\n" +
		"puts m.multipart?\n" +
		"puts m.parts.size\n" +
		"puts m.attachments.size\n" +
		"puts m.attachments.first.filename\n" +
		"puts m.attachments.first.decoded\n"
	lines := strings.Split(strings.TrimRight(rubyEval(t, bin, script), "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("oracle output: %q", lines)
	}
	if strconv.FormatBool(m.Multipart()) != lines[0] {
		t.Errorf("multipart: got %v, gem %s", m.Multipart(), lines[0])
	}
	if strconv.Itoa(len(m.Parts())) != lines[1] {
		t.Errorf("parts: got %d, gem %s", len(m.Parts()), lines[1])
	}
	if strconv.Itoa(len(m.Attachments())) != lines[2] {
		t.Errorf("attachments: got %d, gem %s", len(m.Attachments()), lines[2])
	}
	if m.Attachments()[0].Filename() != lines[3] {
		t.Errorf("filename: got %q, gem %q", m.Attachments()[0].Filename(), lines[3])
	}
	if string(m.Attachments()[0].Decoded()) != lines[4] {
		t.Errorf("attachment decode: got %q, gem %q", string(m.Attachments()[0].Decoded()), lines[4])
	}
}

// TestOracleEncodeParsesInGem serialises a message built here and confirms the
// gem parses it back to the same subject/from/body — the reverse direction.
func TestOracleEncodeParsesInGem(t *testing.T) {
	bin := rubyBin(t)
	m := New("").
		SetFrom("me@here.com").
		SetTo("you@there.com").
		SetSubject("Héllo from Go").
		SetBody("Hello body line")
	encoded := m.Encoded()

	script := heredoc(encoded) +
		"m = Mail.new(raw)\n" +
		"puts m.from.inspect\n" +
		"puts m.subject.inspect\n" +
		"puts m.body.decoded.inspect\n"
	lines := strings.Split(strings.TrimRight(rubyEval(t, bin, script), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("oracle output: %q", lines)
	}
	assertArrayEq(t, "from", m.From(), lines[0])
	// The gem must decode our RFC 2047 subject back to the original.
	if got := rubyInspectString("Héllo from Go"); got != lines[1] {
		t.Errorf("subject: gem parsed our encoding as %s, want %s", lines[1], got)
	}
}

// assertArrayEq compares a Go []string against the gem's inspected Array output
// (e.g. `["a@x.com", "b@y.com"]`). An absent address field is nil here and the
// gem reports it as `nil`, so an empty/nil slice matches the gem's `nil`.
func assertArrayEq(t *testing.T, field string, got []string, gemInspect string) {
	t.Helper()
	if len(got) == 0 && gemInspect == "nil" {
		return
	}
	want := rubyInspectArray(got)
	if want != gemInspect {
		t.Errorf("%s: ours=%s gem=%s", field, want, gemInspect)
	}
}

// assertScalarEq compares a Go string against the gem's inspected String output.
// An absent scalar field is "" here and the gem reports it as `nil`.
func assertScalarEq(t *testing.T, field, got, gemInspect string) {
	t.Helper()
	if got == "" && gemInspect == "nil" {
		return
	}
	if want := rubyInspectString(got); want != gemInspect {
		t.Errorf("%s: ours=%s gem=%s", field, want, gemInspect)
	}
}

// rubyInspectArray renders a []string as Ruby's Array#inspect would.
func rubyInspectArray(ss []string) string {
	parts := make([]string, len(ss))
	for i, s := range ss {
		parts[i] = rubyInspectString(s)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// rubyInspectString renders a string as Ruby's String#inspect would for the
// ASCII/UTF-8 values in the corpus (double-quoted, no escaping needed here).
func rubyInspectString(s string) string {
	return strconv.Quote(s)
}
