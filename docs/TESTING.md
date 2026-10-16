# Verification checkpoint

The intended standalone MVP is not yet accepted as complete. The populated verified catalogue, mature storage-capacity evidence and final independent acceptance remain pending. No executable release is published.

## Current Windows checks

On the resumed correction, Go 1.26.5 on Windows amd64 passes `go test -p 2 -count=1 -timeout=90s ./...` and `go vet -p 2 ./...`, with `PINGOBSERVATORY_NATIVE_TEST=1`. Native ICMP IPv4 and IPv6 loopback bursts succeed. TCP, UDP echo, HTTPS and DNS use isolated IPv4/IPv6 loopback fixtures. No public endpoint probes or host policy changes are used.

Tests cover persistence, exact uneven-sample quantiles, population standard deviation, separate method/family/revision series, real sends versus unobservable outcomes, valid zero-duration baseline samples, completed history grids, explicit retention expiry, active-probe clock jumps, revision ledgers, catalogue rejection/fallback and local API boundaries. Test workers are bounded to two.

Recovered HTTPS catalogue definitions retain their five-second application timeouts. The shared validator permits HTTPS timeouts up to ten seconds while retaining the existing three-second limit for other methods and the 30-second complete-cycle bound. The expanded catalogue remains a local review draft; decoder acceptance does not establish admission evidence or publication.

Frontend TypeScript compilation, 17 Vitest tests and the production Vite build pass using existing pinned dependencies. The map assets and third-party notices remain embedded. The packaged Windows browser checks pass for manual method settings, map/callouts/origin, provider/category/world-area filters, loss sorting, comparison limits, family switching, all history windows, explicit retention expiry, theme, motion and a 390-pixel mobile viewport. The isolated UI made no external requests and reported no browser errors. A separate loopback TCP fixture recorded a newer successful measurement after browser closure, confirming independent background collection. Temporary test processes were closed. A regression guards both family-switch history races and middle-target revision changes.

## Linux validation handoff

WSL is not used for this correction's resumed validation. On the owner's separately approved cloud computer, exact commit `78f5e1a2a4c6328923fc1f2731c4a9e1b6785414` passes the full Go suite, vet, IPv4/IPv6 permission-denial tests, static Linux build, 17 frontend tests and the production build. Packaged HTTP/config, all four history grids, TCP loopback background collection, shutdown/reopen persistence and `--collect=false` pass. Chromium could not launch under the existing cloud sandbox, so Windows provides the current browser evidence. Successful Linux native ICMP and arm64 runtime behavior still require a suitable existing environment.

The same cloud run measured a synthetic 342-series, 30-day profile with three successful samples per minute: 14,774,400 minute batches occupied 2,288,648,192 bytes (2.13 GiB). A 30-day series history took about 120 ms and all-series statistics about 1.07 seconds on that host. This scenario is not the final admitted catalogue workload. Extrapolation at the allowed maximum ICMP packet setting exceeds the 4 GiB cap for sufficiently many series; a universal 30-day capacity claim is not supported.

That mature profile also exposed a 91.3-second synchronous one-day cleanup. The correction removes cleanup from profile opening and retention changes. The app runs resumable background maintenance, commits at most one 64-row modifying batch per step and yields the storage lock between steps. Reads enforce retention immediately, before physical deletion. A regression verifies reopening without draining, interrupted archival, atomic replacement without duplicate raw/chunk records, exact history and immediate expiry. The mature cloud profile must rerun against the corrected commit to establish observed startup/foreground latency and active journal/memory bounds.

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

Verify mature full-catalogue retention and active journal/memory bounds, exact current catalogue evidence and admission, fresh cross-platform packaging, and independent source/publication review. Earlier scaffold-only Windows/WSL results do not establish completion of this correction. macOS is deferred.
