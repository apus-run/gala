# tlscfg

Option-driven way to build a well configured `*tls.Config` with secure
defaults: `New` pins TLS 1.2 as the minimum version and leaves cipher suites
and curves on the Go runtime's curated defaults (they improve with every Go
release; a frozen list would not). Options apply in order — a later option
wins when two options touch the same field.

## Usage

```go
cfg, err := tlscfg.New(
    tlscfg.WithSystemCertPool(),                      // system certs, plus the CA below
    tlscfg.MaybeWithDiskCA(caPath, tlscfg.ForClient), // optional: no-op when empty
    tlscfg.WithDiskKeyPair("cert.pem", "key.pem"),
)
```

- `WithCA`/`WithDiskCA` append to the pool selected by the kind:
  `RootCAs` for `ForClient`, `ClientCAs` plus `RequireAndVerifyClientCert`
  for `ForServer`. A nil pool starts fresh (custom CA only) — apply
  `WithSystemCertPool` first to keep the system certs.
- `WithServerName` matters for clients verifying servers outside
  `http.Transport` (which sets it from the URL host).
- Any field without an option is reachable with a trailing closure:

```go
cfg, err := tlscfg.New(
    tlscfg.WithDiskCA("ca.pem", tlscfg.ForClient),
    func(c *tls.Config) error { c.MaxVersion = tls.VersionTLS13; return nil },
)
```

## Config struct (config-file binding)

`Config` is the data-shaped API for YAML/JSON configuration: embed it in a
config struct and call `Config()`. It decides **whether** TLS is built at
all — when `Enable` is off, or no CA and no `Insecure` is set, it returns
`nil, nil`, which callers conventionally treat as "listen without TLS".

## Certificate

`Certificate(hosts...)` generates a self-signed ECDSA P-256 certificate for
local development and tests — valid for both server and client roles, so a
pair of them is enough to demo mTLS without any fixture files.

## Example

[examples/mtls](examples/mtls) — a runnable mTLS round trip: dev certs are
written to `testdata/` (simulating ops-managed files) and both peers are
configured through the disk options `WithDiskKeyPair`/`WithDiskCA`; an
authorized client gets through, a bare one is rejected at the handshake.

## Known limits

- **Order-sensitive CA composition.** `WithSystemCertPool` must come before
  the CA options it is meant to combine with.
- **A half-set cert/key pair is an error**, not a silent skip — a typo
  fails loudly at `New`.
