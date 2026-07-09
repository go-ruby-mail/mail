# Ruby examples

Pure-Ruby examples of the `mail` gem — the Ruby face of this library — verified
by running under [go-embedded-ruby](https://github.com/go-embedded-ruby/ruby) (rbgo).

```sh
rbgo examples/mail_usage.rb
```

| File | Shows |
| --- | --- |
| [`mail_usage.rb`](mail_usage.rb) | Build with the `Mail.new { … }` DSL, parse a raw RFC 5322 message, read fields and dates, walk `header_fields`, and inspect a multipart message's parts and attachments. |
