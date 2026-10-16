# Verification checkpoint

The intended standalone MVP is not yet accepted as complete. The populated starter and GitHub update channel pass Windows integration checks. Schema 3 adds explicitly labelled historical hostname evidence; remaining platform checks and final product acceptance remain pending. No executable release is published.

## Current Windows checks

On the resumed correction, Go 1.26.5 on Windows amd64 passes `go test -p 2 -count=1 -timeout=90s ./...` and `go vet -p 2 ./...`, with `PINGOBSERVATORY_NATIVE_TEST=1`. Native ICMP IPv4 and IPv6 loopback bursts succeed. TCP, UDP echo, HTTPS and DNS use isolated IPv4/IPv6 loopback fixtures. No public endpoint probes or host policy changes are used.

Tests cover persistence, exact uneven-sample quantiles, population standard deviation, separate method/family/revision series, real sends versus unobservable outcomes, valid zero-duration baseline samples, completed history grids, explicit retention expiry, active-probe clock jumps, revision ledgers, catalogue rejection/fallback and local API boundaries. Test workers are bounded to two.

Recovered HTTPS catalogue definitions retain their five-second application timeouts. The shared validator permits HTTPS timeouts up to ten seconds while retaining the existing three-second limit for other methods and the 30-second complete-cycle bound. The expanded master is checked against its historical receipts; these checks do not establish current endpoint availability.

Frontend TypeScript compilation, 17 Vitest tests and the production Vite build pass using existing pinned dependencies. The map assets and third-party notices remain embedded. The packaged Windows browser checks pass for manual method settings, map/callouts/origin, provider/category/world-area filters, loss sorting, comparison limits, family switching, all history windows, explicit retention expiry, theme, motion and a 390-pixel mobile viewport. The isolated UI made no external requests and reported no browser errors. A separate loopback TCP fixture recorded a newer successful measurement after browser closure, confirming independent background collection. Temporary test processes were closed. A regression guards both family-switch history races and middle-target revision changes.

## Populated catalogue onboarding

The default URL is [the complete manifest on the data-only catalogue branch](https://raw.githubusercontent.com/Shaderx/PingObservatory/catalogue/catalogue/latest.json). Channel commit `c6dce37a8072619d63bad0a07899c0679042369f` was independently reviewed as a parentless six-file tree. Its actual GitHub download matches the reviewed immutable version-2 Git blob `d14c2415445af82705137eaa40abf00782f324d3`. Main remains unchanged. The executable embeds this same version-2 snapshot: 171 targets, 196 configured family slots, 116 historically verified ICMP/IPv4 literal families and 80 pending hostname families. The hostname schema and evidence policy are unchanged.

Production-path tests cover immediate populated startup, same-version remote activation and cache reopening, a valid newer cache taking precedence, rejected downgrades and malformed updates, HTTP failures and a cancelled download, malformed caches and rejection of the old empty version-1 cache. Both exact obsolete default URLs migrate persistently; empty and explicit pinned URLs are preserved. A disabled updater retains rejected-cache warnings. Inspect mode uses a one-millisecond test schedule with a probe counter and makes no probe calls. Missing and expired observations remain N/A rather than becoming local inability failures; real failure statuses remain intact.

The packaged Windows executable was then tested with `--collect=false --open-browser=false --listen 127.0.0.1:0` in fresh temporary profiles. Its first API snapshot was populated from the bundle before the actual GitHub download activated and persisted the cache. Reopening, a real GitHub 404 with an unchanged last-valid cache, updates disabled, a corrupt cache, both migrated defaults and an explicit immutable URL passed. Verification receipts never became desktop samples: all 196 local series had no observation timestamp or sent/received counts, and history buckets stayed N/A.

The populated browser check passed for the map, all 171 directory records, settings URL/version/source, provider/category/world-area/method filters, 116 empty-local-history IPv4 states versus 55 unverified states, and 25 unverified versus 146 unconfigured IPv6 states. Desktop and 390-pixel mobile layouts had no horizontal overflow, browser errors or external UI requests. Test-owned processes were closed. The full Windows Go suite, native IPv4/IPv6 loopback tests and vet passed after the integration fixes. Static builds for Windows/Linux amd64/arm64 also passed; cross-compilation does not establish Linux or arm64 runtime behavior for this populated integration. No public endpoint probes were performed.

## Schema-3 hostname evidence

The reviewed evidence-only commit `62c8fef7839f43dad3f8a84e8e68e4187556475e` preserves 22 genuine successful hostname-family batches from the saved October 9 audit: 16 GameLift UDP batches at 3/3, four Fortnite ICMP batches at 5/5, and two cdnjs HTTPS batches at 1/1. Independent review matched the configured hostname, applicable port/scheme/path/HTTP method, exact method/family, timestamps, status and counts. Every receipt explicitly states that the numeric peer was not recorded; no later DNS lookup supplies one. The 116 earlier numeric-destination receipts, v2 snapshot and evidence pins remain unchanged.

The v3 embedded starter and complete `latest.json` match the immutable v3 snapshot: 171 definitions, 196 configured families, 138 eligible jobs (120 ICMP, 16 UDP, two HTTPS), and 58 pending families. Eligibility comprises 116 numeric-destination and 22 hostname-level historical successes. New tests reject missing/mixed scopes, including forbidden empty/null fields, wrong hostname/port/method/family/time and hostname evidence on literal targets. Schema 2 remains numeric-only. Cache tests cover schema-3 reopen, higher valid cache, older-cache fallback, removed-ID revision guards across upgrade, and rollback/equivocation rejection. Provenance-only promotion and refresh preserve intrinsic probe identity and revision.

The Windows suite, native IPv4/IPv6 loopback checks, vet, TypeScript build, 18 frontend tests and embedded production build pass. A packaged offline browser run with collection paused passed explicit hostname/numeric/pending labels, all 171 directory records, IPv4/IPv6 admission states and N/A local statistics/history. Desktop and 390-pixel mobile layouts had no horizontal overflow, external UI requests or browser errors. No public endpoint probes were performed. Verification of the published v3 channel and actual older schema-2 executable follows publication.

## Linux validation handoff

WSL is not used for this correction's resumed validation. On the owner's separately approved cloud computer, exact commit `78f5e1a2a4c6328923fc1f2731c4a9e1b6785414` passes the full Go suite, vet, IPv4/IPv6 permission-denial tests, static Linux build, 17 frontend tests and the production build. Packaged HTTP/config, all four history grids, TCP loopback background collection, shutdown/reopen persistence and `--collect=false` pass. Chromium could not launch under the existing cloud sandbox, so Windows provides the current browser evidence. Successful Linux native ICMP and arm64 runtime behavior still require a suitable existing environment.

The same cloud run measured a synthetic 342-series, 30-day profile with three successful samples per minute: 14,774,400 minute batches occupied 2,288,648,192 bytes (2.13 GiB). A 30-day series history took about 120 ms and all-series statistics about 1.07 seconds on that host. This scenario is not the final admitted catalogue workload. Extrapolation at the allowed maximum ICMP packet setting exceeds the 4 GiB cap for sufficiently many series; a universal 30-day capacity claim is not supported.

That mature profile also exposed a 91.3-second synchronous one-day cleanup. The correction removes cleanup from profile opening and retention changes. The app runs resumable background maintenance, commits at most one 64-row modifying batch per step and yields the storage lock between steps. Reads enforce retention immediately, before physical deletion. A regression verifies reopening without draining, interrupted archival, atomic replacement without duplicate raw/chunk records, exact history and immediate expiry.

The cloud retest of exact `660ad6c1116d9391a8990b0a7993ec03b7190fcc` passed at the same synthetic 342-series mature scale. Store opening took 0.45 ms; the packaged executable announced its URL in 8.65 ms. During cleanup, 24 API requests passed, with worst state/history durations of 25.1/174.2 ms. Cleanup completed in 196.5 seconds across 7,605 bounded steps under 838 concurrent correct history reads; SQLite integrity checking passed. Peak RSS was 104.4 MiB, and shutdown took 11.2 ms. These are observations from that synthetic cloud sandbox, not performance guarantees or a measurement of the final admitted catalogue. The startup regression is resolved; allowed extreme workloads can still exceed the storage cap.

Run the ordinary suite and vet first, without the native-success flag:

```sh
GOMAXPROCS=2 go test -p 2 -count=1 -timeout=90s ./...
GOMAXPROCS=2 go vet -p 2 ./...
PINGOBSERVATORY_PERMISSION_TEST=1 GOMAXPROCS=2 go test -p 2 -count=1 -timeout=30s ./internal/probe -run Permission
CGO_ENABLED=0 go build -trimpath -o PingObservatory ./cmd/pingobservatory
./PingObservatory --licenses
```

Do not set `PINGOBSERVATORY_NATIVE_TEST=1` on a host whose existing policy denies ICMP. Permission denial must have no sent-packet loss denominator. For UI checks, use a temporary data directory and `--open-browser=false --collect=false --listen 127.0.0.1:0`. The v3 populated default has 138 eligible public series, so leave collection paused during inspection. A loopback collection test must also disable or replace catalogue jobs with its isolated fixtures.

## Remaining acceptance

Verify active journal bounds for the final workload, run the populated schema-3 integration on Linux, complete successful Linux native ICMP and arm64 runtime checks, and obtain final product acceptance. Families without adequate evidence remain pending. Earlier scaffold-only Windows/WSL results do not establish completion of this correction. macOS is deferred.
