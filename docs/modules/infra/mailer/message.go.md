# Module: infra/mailer/message.go

## Purpose

`Message` and its `build` method assemble the RFC 5322 bytes the sender puts on the wire.
It was split out when the mailer grew past myidsan's single-recipient plain-text reset link:
the notification channel (`infra/notification/mail_channel.go.md`) needs several recipients,
custom headers, and an image attachment, and all three are places a naive builder leaks or
corrupts something.

## Responsibilities

- `Message{To, Subject, Body, Headers, Attachments}` — the whole outbound message.
- `Attachment{Filename, ContentType, Data}` — one file, defaulting to
  `application/octet-stream` when the type is blank.
- `build(from)` emits `From`, `To`, `Subject`, `Date`, any extra headers, `MIME-Version`,
  then either a `text/plain; charset=utf-8` body or a `multipart/mixed` document with the
  body as the first part and each attachment base64-encoded after it.

## Guards, and why each is not optional

- **CR/LF stripped from every header value** (`stripHeader`, shared with `mailer.go`). A
  templated value — a camera name, a rule name, an operator-typed recipient — can never open
  a new header line, only appear as inert text on the legitimate one. This is the classic
  email-header-injection class; `TestBuildMessageStripsHeaderInjection` asserts no line
  *starts* with an injected header name.
- **Reserved headers cannot be overridden** (`reservedHeaders`: from, to, subject, date,
  mime-version, content-type, content-transfer-encoding). Caller-supplied `Headers` are
  merged, but not these. Overriding `Content-Type` on a multipart message would detach the
  body from its parts and hide the alert text entirely.
- **Non-ASCII subjects are RFC 2047 encoded** (`encodeHeaderWord`). Camera and rule names in
  this suite are routinely Malay, Chinese or Arabic; a raw UTF-8 subject is mangled or
  rejected by a strict relay. ASCII subjects are left readable rather than needlessly
  encoded.
- **Base64 is folded at 76 columns** (`wrapBase64`), which RFC 2045 requires and some relays
  enforce by rejecting the message. `TestBuildMessageAttachmentIsDecodable` folds a 500-byte
  payload, decodes it back, and compares — a snapshot that arrives corrupt is worse than
  none, because the operator believes they have looked at the evidence.
- **Extra headers are emitted in sorted order** (`sortedHeaderKeys`). Go map iteration is
  randomised; without this, any assertion on a rendered message is flaky.

## Notes

- The multipart boundary is a fixed token no encoder emits, so it cannot collide with base64
  or with plain text.
- The body is normalised to CRLF (`crlf`). Dot-stuffing is `net/smtp`'s job and is asserted
  end to end by `TestSendMessageDotStuffing`.
