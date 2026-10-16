# The Ping Observatory

A standalone network latency map for Windows and Linux.

## Status

The initial scaffold is available on `main`. Development on `feature/mvp` is correcting it to preserve the stable Looking Glass application's supported methods, families, filters, statistics and history views. The intended standalone MVP is not complete. No executable release is published.

## MVP

- A single executable that runs a background collector and an embedded local web UI.
- Generic branding and local data storage.
- Gaps while the application is closed or the system is asleep displayed as `N/A`.
- Normal probe failures displayed separately from those gaps.
- Periodic updates from a versioned, verified endpoint catalogue on GitHub, with a bundled starter catalogue and a last-valid fallback.

macOS support is deferred until testing is available.

## Build and run

Requires Go 1.25 or newer to build. The committed frontend assets are embedded, so Node.js is not required to build or run the Go executable.

```sh
go build -trimpath -o PingObservatory ./cmd/pingobservatory
./PingObservatory
```

On Windows, use `go build -trimpath -o PingObservatory.exe ./cmd/pingobservatory` and run `PingObservatory.exe`. The application opens `http://127.0.0.1:8742`. Keep the application process running; closing the browser does not stop its one-minute staggered collector. Use `--open-browser=false` to print the URL without opening a browser, or `--listen 127.0.0.1:0` for an available port.

Configuration, identity history, the last-valid catalogue and measurements are stored in the per-user configuration directory under `PingObservatory` (Windows `%AppData%`, Linux `$XDG_CONFIG_HOME` or `~/.config`). Use `--data-dir` for a separate profile. Set the origin location and user-provided IPv4 or IPv6 targets in the UI. Target changes require a new revision.

The [version-2 master catalogue](catalogue/v2.json) preserves 171 public definitions: 116 literal ICMP/IPv4 families have dated historical success evidence, and 80 hostname families remain pending. The executable still embeds the empty legacy starter during final integration. User-provided targets are labelled separately and never claim catalogue verification. Updates run at startup and every six hours; an update failure retains the last-valid catalogue or bundled starter. Verification records success from the owner's validation network at the stated time, and does not guarantee current or desktop reachability.

Linux needs existing permission for ICMP ping sockets or raw sockets. If neither is available, the application records `permission_denied`; it does not change system settings. The correction supports ICMP, GameLift UDP echo, TCP connect, HTTPS and DNS for IPv4 and IPv6. Packet loss applies to ICMP/UDP; other methods report failed network attempts. Pure-Go SQLite retains recent raw batches and exact timing samples in older 30-minute chunks, with 30-day default retention and a 4 GiB main-file limit. Capacity errors are visible rather than silently evicting unexpired history. The UI provides 1h, 24h, 7d and 30d history windows. Use `--collect=false` to inspect saved data without probing.

## Initial milestones

See [the plan](docs/PLAN.md), [implementation breakdown](docs/MVP.md), and [verification results](docs/TESTING.md). Windows native ICMP and cross-platform protocol fixtures are tested; successful Linux native ICMP and arm64 runtime checks remain pending. No release is published.

## Licence

The application is licensed under the GNU Affero General Public License version 3 only (`AGPL-3.0-only`). See [LICENSE](LICENSE).

Third-party terms are preserved in [the notices](docs/THIRD_PARTY.md), embedded in the executable (`--licenses`), and available at `/licenses.txt` in the local UI.
