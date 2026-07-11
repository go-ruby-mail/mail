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

> **Parsing, generating _and_ delivery.** Parsing and generating the on-the-wire
> form of a message (RFC 5322 grammar, MIME structure, transfer encodings) is
> fully deterministic and needs **no interpreter**, so it lives here as pure Go.
> **Delivery and retrieval** — sending over SMTP/sendmail and fetching over
> POP3/IMAP — are implemented here too, in pure Go, mirroring the gem's delivery
> and retriever methods. Every transport reaches its server through an injectable
> dialer seam, so the package stays CGO-free and is driven in tests by in-process
> SMTP/POP3/IMAP servers — never a real mail server.

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
- **Delivery** — the gem's delivery methods: `SMTP` (STARTTLS / implicit TLS,
  PLAIN / LOGIN / CRAM-MD5 auth), `Sendmail`, `TestDelivery`, `FileDelivery` and
  `LoggerDelivery`, plus the SMTP envelope (Return-Path / Sender / From sender,
  To+Cc+Bcc recipients, Bcc stripped from the transmitted message).
- **Retrieval** — POP3 and IMAP retrievers (`Find` / `First` / `Last` / `All`
  with the gem's what/order/count selection; IMAP mailbox select/search/fetch),
  both pure-Go clients over an injectable dialer seam.

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

// Delivery (Mail.defaults { delivery_method … } / message.deliver)
func Defaults(fn func(*Config))                     // configure delivery + retriever
func (m *Message) Deliver() error                   // via the configured method
func (m *Message) DeliverWith(d DeliveryMethod) error
type DeliveryMethod interface { Deliver(m *Message) error }
type SMTP struct { Address, Port, Domain, UserName, Password, Authentication,
    OpenSSLVerifyMode string; EnableStartTLSAuto, SSL, TLS bool; Dial Dialer; … }
type Sendmail struct { Location string; Arguments []string; Run … }
type TestDelivery struct{ … }   // records deliveries, like Mail::TestMailer
type FileDelivery struct{ Location, Extension string }
type LoggerDelivery struct{ Logger Logger }
type Dialer func(network, address string) (net.Conn, error) // the transport seam

// Retrieval (retriever_method :pop3 / :imap ; Mail.find/first/last/all)
func Find(opts FindOptions) ([]*Message, error)
func First() (*Message, error); func Last() (*Message, error); func All() ([]*Message, error)
type RetrieverMethod interface { Find(opts FindOptions) ([]*Message, error) }
type POP3 struct { Address, UserName, Password string; Port int; EnableSSL bool; Dial Dialer; … }
type IMAP struct { Address, UserName, Password string; Port int; EnableSSL, EnableStartTLS bool; Dial Dialer; … }
```

## Sending and retrieving

```go
// Configure a delivery method, then deliver (Mail.defaults + message.deliver).
mail.Defaults(func(c *mail.Config) {
    c.SetDeliveryMethod(&mail.SMTP{
        Address: "smtp.example.com", Port: 587,
        UserName: "me", Password: "secret", Authentication: "plain",
        EnableStartTLSAuto: true,
    })
})
msg := mail.New("", func(m *mail.Message) {
    m.SetFrom("me@example.com").SetTo("you@example.com").
        SetSubject("Hi").SetBody("Hello")
})
_ = msg.Deliver()

// Retrieve over POP3 or IMAP (Mail.find/first/all).
mail.Defaults(func(c *mail.Config) {
    c.SetRetrieverMethod(&mail.IMAP{
        Address: "imap.example.com", Port: 993, EnableSSL: true,
        UserName: "me", Password: "secret",
    })
})
inbox, _ := mail.All()
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

Delivery and retrieval are driven by **in-process fake SMTP / POP3 / IMAP
servers** reached through the injectable dialer seam — no real mail server is
contacted — and the delivery path is additionally differential-tested against
the gem: the same message is delivered to an in-process SMTP sink from both the
gem and this package, and the envelope and DATA put on the wire are compared.

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

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright the go-ruby-mail/mail authors.

## WebAssembly

Being pure Go (CGO=0), this library also compiles to **WebAssembly** — both
`GOOS=js GOARCH=wasm` (browser / Node.js) and `GOOS=wasip1 GOARCH=wasm` (WASI).
CI builds both targets on every push, alongside the six 64-bit native/qemu arches.

```sh
GOOS=js     GOARCH=wasm go build ./...   # browser / Node
GOOS=wasip1 GOARCH=wasm go build ./...   # WASI (wasmtime, wasmer, wasmedge, …)
```
