# smtp-debug

SMTP relay that forwards each message upstream. If delivery fails, it writes one text file and returns the upstream error with that filename appended.

The original message bytes are sent upstream, including attachment bodies. The failed file keeps headers and text (`text/plain`, `text/html`). Other parts are listed by filename only.

There is no inbound authentication. The default listen address is `127.0.0.1:2525`.

## Run

`SMTP_DEBUG_UPSTREAM` is required (`host:port`). The process exits if it is missing.

```bash
SMTP_DEBUG_UPSTREAM=smtp.example.com:587 \
SMTP_DEBUG_STARTTLS=true \
SMTP_DEBUG_UPSTREAM_USER=user \
SMTP_DEBUG_UPSTREAM_PASS=secret \
SMTP_DEBUG_FAILED_DIR=/failed \
go run .
```

## Config

| Variable | Default | Purpose |
| --- | --- | --- |
| `SMTP_DEBUG_LISTEN` | `127.0.0.1:2525` | Address to accept mail on |
| `SMTP_DEBUG_UPSTREAM` | | Upstream `host:port` |
| `SMTP_DEBUG_UPSTREAM_USER` | | PLAIN auth user |
| `SMTP_DEBUG_UPSTREAM_PASS` | | PLAIN auth password |
| `SMTP_DEBUG_SSL` | false | Implicit TLS (usually port 465) |
| `SMTP_DEBUG_STARTTLS` | false | Upgrade with STARTTLS (usually port 587) |
| `SMTP_DEBUG_INSECURE_TLS` | false | Skip upstream certificate checks |
| `SMTP_DEBUG_FAILED_DIR` | `/failed` | Directory for failed mail |
| `SMTP_DEBUG_DOMAIN` | `localhost` | Name used in EHLO |
| `SMTP_DEBUG_MAX_BYTES` | `26214400` | Maximum message size (25 MiB) |

`true`, `1`, and `yes` turn a boolean on. `false`, `0`, `no`, and an empty value turn it off.

`SMTP_DEBUG_SSL` and `SMTP_DEBUG_STARTTLS` cannot both be set. With neither set, the upstream connection stays plain.

## Failed mail

Files are written to `SMTP_DEBUG_FAILED_DIR`. The name is UTC time to a hundredth of a second, plus 8 hex characters:

`20261004T150405.12Z_ab12cd34.txt`

The SMTP reply keeps the upstream code and appends the filename:

`550 5.1.1 user unknown (saved as 20261004T150405.12Z_ab12cd34.txt)`

A dial, TLS, or auth failure is returned as `451 4.4.1`, also with the filename. If the file cannot be written, the sender still gets the failure, without a filename. Accepted mail is not saved.

## Docker

The image listens on `0.0.0.0:2525` and writes failed mail to `/failed`. `SMTP_DEBUG_UPSTREAM` is still required at start.

```bash
docker build -t smtp-debug .
docker run --rm -p 2525:2525 \
  -e SMTP_DEBUG_UPSTREAM=smtp.example.com:587 \
  -e SMTP_DEBUG_STARTTLS=true \
  -v smtp-failed:/failed \
  smtp-debug
```

## AI disclosure

This module was written with Grok 4.7, a language model from SpaceXAI. Review the code before you install it on a phone.
