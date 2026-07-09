# frozen_string_literal: true
#
# Basic usage of the `mail` gem — building, parsing, and reading MIME messages.
# Runs under go-embedded-ruby (rbgo); see examples/README.md.

require "mail"

# Build a message with the block DSL, then re-emit it on the wire.
msg = Mail.new do
  from "alice@example.com"
  to "bob@example.com"
  subject "Greetings"
  body "Hello body"
end
puts msg.subject                                   # => Greetings
puts msg.body.decoded                              # => Hello body
puts msg.encoded.include?("Subject: Greetings")    # => true

# Parse a raw RFC 5322 message; a multi-recipient field comes back as an Array.
raw = "From: a@x.com\nTo: b@y.com, c@z.com\nSubject: Hi\n" \
      "Date: Mon, 01 Jul 2024 10:00:00 +0000\n\nBody text\n"
parsed = Mail.new(raw)
p parsed.to                                        # => ["b@y.com", "c@z.com"]
puts parsed.date.year                              # => 2024
puts parsed["Subject"]                             # => Hi (indexed header access)

# Walk the ordered header fields as Mail::Field value objects.
parsed.header_fields.each { |f| puts "#{f.name} = #{f.value}" }

# Parse a multipart/mixed message and inspect its parts and attachments.
mime = "MIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=B\n\n" \
       "--B\nContent-Type: text/plain\n\ntext part\n" \
       "--B\nContent-Disposition: attachment; filename=\"report.bin\"\n\ndata\n" \
       "--B--\n"
mp = Mail.new(mime)
puts mp.multipart?                                 # => true
puts mp.parts.length                               # => 2
puts mp.attachments.first.filename                 # => report.bin
