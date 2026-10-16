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

The starter catalogue is empty until actual owner-homelab verification evidence is available. User-provided targets are labelled separately and never claim catalogue verification. Updates run at startup and every six hours; an update failure retains the last-valid catalogue or bundled starter. Catalogue verification means reachable from the owner's homelab at the recorded time, and does not guarantee reachability from every client.

Linux needs existing permission for ICMP ping sockets or raw sockets. If neither is available, the application records `permission_denied`; it does not change system settings. IPv6 probing is deferred. Storage retains up to the configured 1–30 days, subject to 100,000 rows and a 128 MiB database cap; compaction may temporarily use an additional bounded copy. UI history is limited to 24 hours.

## Initial milestones

See [the plan](docs/PLAN.md), [implementation breakdown](docs/MVP.md), and [verification results](docs/TESTING.md). Windows/Linux amd64 native loopback behavior is tested; arm64 is cross-compiled and awaits runtime testing. No release is published.

## Licence

The application is licensed under the GNU Affero General Public License version 3 only (`AGPL-3.0-only`). See [LICENSE](LICENSE).

Third-party terms are preserved in [the notices](docs/THIRD_PARTY.md), embedded in the executable (`--licenses`), and available at `/licenses.txt` in the local UI.
