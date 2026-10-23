# The Ping Observatory

A standalone network latency map for Windows and Linux.

## Status

A [Windows amd64 full release, v0.1.0](https://github.com/Shaderx/PingObservatory/releases/tag/v0.1.0), contains the reviewed standalone collector and atlas presentation. Its product code and embedded assets match tested commit `46f1c5b`; the release source adds documentation only. This update restores travelling connector styling, adds an approximate location picker on the map and makes loss/status cues clearer. Use the release tag or corresponding-source archive to build it. Cross-platform MVP acceptance remains open.

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

Configuration, identity history, the last-valid catalogue and measurements are stored in the per-user configuration directory under `PingObservatory` (Windows `%AppData%`, Linux `$XDG_CONFIG_HOME` or `~/.config`). Use `--data-dir` for a separate profile. Pick an approximate origin on the map and confirm it locally to enable distance colours, or enter latitude/longitude in settings. Set user-provided IPv4 or IPv6 targets in the UI. Target changes require a new revision.

The executable embeds the [version-3 master catalogue](catalogue/v3.json), so a fresh offline profile immediately displays 171 public definitions and 196 configured family series. There are 116 numeric-destination and 22 explicitly hostname-level historical success receipts; the remaining 58 families stay pending without automatic probing. Hostname-level evidence does not claim an unrecorded numeric peer. Local history starts empty; catalogue evidence does not create local measurements. User-provided targets are labelled separately and never claim catalogue verification.

Updates run at startup and every six hours from the [GitHub catalogue channel](https://raw.githubusercontent.com/Shaderx/PingObservatory/catalogue/catalogue/latest.json). This data-only branch keeps the download independent of source development and executable releases. The channel's `latest.json` is a complete manifest, currently identical to version 3. A failed or rejected update retains the last-valid local cache or populated bundled starter and displays a warning. Earlier preview defaults pointing to `main/catalogue/v1.json` or `main/catalogue/v2.json` migrate automatically; an empty URL still disables updates, and explicit custom or pinned URLs are preserved. Verification records success from the owner's validation network at the stated time, and does not guarantee current or desktop reachability. Each client resolves hostnames from its own network; curator evidence never supplies a global IP pin.

The collector also records the actual attempted numeric destination, without another DNS lookup. Each method/family/revision keeps a first captured baseline and dated address changes. The UI flags these as informational; normal DNS or CDN rotation does not change health. Catalogue refreshes preserve local history and never create local destination events. See [destination tracking](docs/DESTINATIONS.md) for retention and migration details: opening an older profile upgrades SQLite to schema 3, which earlier schema-2 executables cannot open.

Linux needs existing permission for ICMP ping sockets or raw sockets. If neither is available, the application records `permission_denied`; it does not change system settings. The correction supports ICMP, GameLift UDP echo, TCP connect, HTTPS and DNS for IPv4 and IPv6. Packet loss applies to ICMP/UDP; other methods report failed network attempts. Pure-Go SQLite retains recent raw batches and exact timing samples in older 30-minute chunks, with 30-day default retention and a 4 GiB main-file limit. Capacity errors are visible rather than silently evicting unexpired history. The UI provides 1h, 24h, 7d and 30d history windows. Use `--collect=false` to inspect saved data without probing.

## Initial milestones

See [the plan](docs/PLAN.md), [implementation breakdown](docs/MVP.md), and [verification results](docs/TESTING.md). Windows native ICMP and protocol fixtures are tested. Linux startup and cloud Firefox presentation checks pass; successful Linux native ICMP, full Linux workflow acceptance and arm64 runtime checks remain pending.

The [release notes](docs/RELEASE_NOTES.md) describe the Windows release, presentation changes, profile migration and remaining platform limits. Linux and arm64 binaries are excluded from the supported download set. Physical suspend/resume and cross-platform product acceptance remain open.

## Licence

The application is licensed under the GNU Affero General Public License version 3 only (`AGPL-3.0-only`). See [LICENSE](LICENSE).

Third-party terms are preserved in [the notices](docs/THIRD_PARTY.md), embedded in the executable (`--licenses`), and available at `/licenses.txt` in the local UI.
