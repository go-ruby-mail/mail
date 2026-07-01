<p align="center"><img src="https://raw.githubusercontent.com/go-ruby-mail/brand/main/social/go-ruby-mail-mail.png" alt="go-ruby-mail/mail" width="720"></p>

# mail — go-ruby-mail

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-DC2626)](https://go-ruby-mail.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests--coverage)

**A pure-Go (no cgo) reimplementation of Ruby's [`mail`](https://github.com/mikel/mail)
gem** — the RFC 5322 / MIME message parser and generator. It builds and reads
email messages: header fold/unfold, structured address fields, RFC 2047
encoded-words, and RFC 2045 MIME (multipart, boundaries, nested parts,
attachments, and base64 / quoted-printable transfer encodings) — **without any
Ruby runtime**.

It is the message backend for
[go-embedded-ruby](https://github.com/go-embedded-ruby/ruby), but is a
**standalone, reusable** module — a sibling of
[go-ruby-yaml](https://github.com/go-ruby-yaml/yaml) and
[go-ruby-regexp](https://github.com/go-ruby-regexp/regexp).

> **What it is — and isn't.** Parsing and generating the on-the-wire form of a
> message (RFC 5322 grammar, MIME structure, transfer encodings) is fully
> deterministic and needs **no interpreter**, so it lives here as pure Go.
> *Delivery* — actually sending over SMTP or fetching over IMAP/POP — is a **host
> seam**: the go-ruby ecosystem ships those protocol libraries
> ([go-ruby-net-smtp](https://github.com/go-ruby-net-smtp/net-smtp),
> [go-ruby-net-imap](https://github.com/go-ruby-net-imap/net-imap),
> [go-ruby-net-pop](https://github.com/go-ruby-net-pop/net-pop)), and a host such
> as go-embedded-ruby wires this message model to them. This package produces and
> consumes the **bytes**; it never opens a socket.

## Features

Faithful port of the gem's parse + generate, validated against the `mail` gem on
every supported platform:

- **RFC 5322 headers** — fold/unfold of continuation lines, ordered multi-valued
  fields, and the standard fields From / To / Cc / Bcc / Reply-To / Subject /
  Date / Message-ID / In-Reply-To / References / MIME-Version /
  Content-Type / Content-Transfer-Encoding / Content-Disposition / Content-ID.
- **Structured addresses** — display-name / angle-addr / bare addr-spec /
  parenthesised comments / RFC 5322 group syntax, with quoted and RFC 2047
  encoded display names (`Mail::Address`, `Mail::AddressList`).
- **RFC 2047 encoded-words** — `=?charset?B?…?=` and `?Q?…?=` decode (UTF-8,
  US-ASCII, ISO-8859-1) and encode, with the §6.2 adjacent-word whitespace fold.
- **RFC 2045 MIME** — `multipart/{mixed,alternative,related}` with boundaries and
  nested parts, `.parts`, `.attachments` (filename from `Content-Disposition` or
  `Content-Type; name=`, content-id), and body decode of base64 /
  quoted-printable / 7bit / 8bit.
- **Generate** — `Encoded()` / `String()` re-emit the message, RFC 2047 encoding
  non-ASCII unstructured fields and folding long headers at 78 columns.

CGO-free, dependency-free, **100% test coverage**, `gofmt` + `go vet` clean, and
green across the six 64-bit Go targets (amd64, arm64, riscv64, loong64, ppc64le,
s390x).

## Install

```sh
go get github.com/go-ruby-mail/mail
```

## Usage

```go
package main

import (
	"fmt"

	"github.com/go-ruby-mail/mail"
)

func main() {
	// Parse a raw message (Mail.new).
	m := mail.New("From: John <john@example.com>\r\n" +
		"To: a@x.com, b@y.com\r\n" +
		"Subject: =?utf-8?B?SGVsbG8=?=\r\n\r\n" +
		"Body text\r\n")

	fmt.Println(m.From())               // [john@example.com]
	fmt.Println(m.To())                 // [a@x.com b@y.com]
	fmt.Println(m.Subject())            // Hello   (encoded-word decoded)
	fmt.Println(m.Body().DecodedString())

	// Build a message (Mail.new { … }).
	out := mail.New("", func(m *mail.Message) {
		m.SetFrom("me@here.com").
			SetTo("you@there.com").
			SetSubject("Héllo").      // RFC 2047 encoded on emit
			SetBody("Hello body")
	})
	fmt.Print(out.Encoded())
}
```

## API

```go
func New(raw string, builders ...func(*Message)) *Message // Mail.new / Mail.new { … }
func Read(path string) (*Message, error)                  // Mail.read

// Accessors (gem-faithful)
func (m *Message) From/To/Cc/Bcc/ReplyTo() []string
func (m *Message) Subject() string
func (m *Message) MessageID/InReplyTo() string
func (m *Message) References() []string
func (m *Message) Date() (time.Time, bool)
func (m *Message) Body() *Body
func (m *Message) Multipart() bool
func (m *Message) Parts() []*Part
func (m *Message) Attachments() []*Part
func (m *Message) TextPart/HTMLPart() *Part
func (m *Message) ContentType/MimeType/Charset() string
func (m *Message) Header() *Header

// MIME part / attachment
func (m *Message) Filename/ContentID/ContentDisposition() string
func (m *Message) IsAttachment() bool
func (m *Message) Decoded() []byte

// Generate
func (m *Message) Encoded() string // == String() ; Mail#encoded / #to_s

// Structured types
type Address struct { DisplayName, Local, Domain string; Comments []string }
func NewAddress(s string) *Address
func NewAddressList(s string) *AddressList
type Body struct { Raw, Encoding string }
type Field struct { Name, Value string }
type Header struct { /* ordered fields + case-insensitive lookup */ }
type Part = Message
```

## Tests & coverage

The suite pairs deterministic, ruby-free tests (which alone hold coverage at
100%, so the qemu cross-arch and Windows lanes pass the gate) with a
**differential `mail`-gem oracle** (version-gated to Ruby ≥ 4.0): a corpus of
folded-header / encoded-word / address-group / multipart / attachment messages is
parsed here and in the gem and the accessors compared, and a message built here
is re-parsed by the gem. The oracle scripts `$stdout.binmode` so Windows
text-mode never pollutes the bytes, and skip themselves where `ruby` / the gem is
absent.

```sh
COVERPKG=$(go list ./... | paste -sd, -)
go test -race -coverpkg="$COVERPKG" -coverprofile=cover.out ./...
go tool cover -func=cover.out | tail -1   # 100.0%
```

## Deferred RFC edge cases

Documented honestly for future work; none affect the corpus above:

- **RFC 2231** continued/extended parameters (`name*0*=…`, charset-tagged
  parameter values) are not yet decoded — plain and quoted `filename=` / `name=`
  are.
- **Charsets** beyond UTF-8 / US-ASCII / ISO-8859-1 in encoded-words are passed
  through byte-for-byte rather than transcoded (no data loss).
- **Delivery** (SMTP/IMAP/POP) is intentionally a host seam, not implemented here.

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright the go-ruby-mail/mail authors.
