# Verification checkpoint

The intended standalone MVP is not yet accepted as complete. The populated verified catalogue, mature storage-capacity evidence and final independent acceptance remain pending. No executable release is published.

## Current Windows checks

On the resumed correction, Go 1.26.5 on Windows amd64 passes `go test -p 2 -count=1 -timeout=90s ./...` and `go vet -p 2 ./...`, with `PINGOBSERVATORY_NATIVE_TEST=1`. Native ICMP IPv4 and IPv6 loopback bursts succeed. TCP, UDP echo, HTTPS and DNS use isolated IPv4/IPv6 loopback fixtures. No public endpoint probes or host policy changes are used.

Tests cover persistence, exact uneven-sample quantiles, population standard deviation, separate method/family/revision series, real sends versus unobservable outcomes, valid zero-duration baseline samples, completed history grids, explicit retention expiry, active-probe clock jumps, revision ledgers, catalogue rejection/fallback and local API boundaries. Test workers are bounded to two.

Frontend TypeScript compilation, 15 Vitest tests and the production Vite build pass using existing pinned dependencies. The map assets and third-party notices remain embedded. Corrected packaged browser verification is still required.

## Linux validation handoff

WSL is not used for this correction's resumed validation. Linux tests will run on the owner's separately approved cloud computer against an exact reviewed revision. The cloud environment denies ICMP datagram and raw sockets, so permission-denied behavior can be tested without changing policy. Successful Linux native ICMP and arm64 runtime behavior still require a suitable existing environment.

Run the ordinary suite and vet first, without the native-success flag:

```sh
GOMAXPROCS=2 go test -p 2 -count=1 -timeout=90s ./...
GOMAXPROCS=2 go vet -p 2 ./...
PINGOBSERVATORY_PERMISSION_TEST=1 GOMAXPROCS=2 go test -p 2 -count=1 -timeout=30s ./internal/probe -run Permission
CGO_ENABLED=0 go build -trimpath -o PingObservatory ./cmd/pingobservatory
./PingObservatory --licenses
```

Do not set `PINGOBSERVATORY_NATIVE_TEST=1` on a host whose existing policy denies ICMP. Permission denial must have no sent-packet loss denominator. For UI checks, use a temporary data directory and `--open-browser=false --collect=false --listen 127.0.0.1:0`; add only loopback fixture targets. The default verified catalogue is still pending.

## Remaining acceptance

Verify mature full-catalogue retention and active journal/memory bounds, corrected packaged browser behavior and background collection, exact current catalogue evidence and admission, fresh cross-platform packaging, and independent source/publication review. Earlier scaffold-only Windows/WSL results do not establish completion of this correction. macOS is deferred.
