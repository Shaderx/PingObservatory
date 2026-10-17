# The Ping Observatory v0.1.0-rc.1

Release candidate for **Windows amd64 preview**. Source: [`51edfab96143b3b6051f65d02c29ee4be024ee35`](https://github.com/Shaderx/PingObservatory/commit/51edfab96143b3b6051f65d02c29ee4be024ee35). Final MVP acceptance remains pending.

## What is included

A standalone executable combines a background collector with an embedded local web UI. It supports ICMP, GameLift UDP echo, TCP connect, HTTPS and DNS, with separate IPv4/IPv6 measurements, method-aware failure rates, baseline statistics, a map, filters, favorites and 1h/24h/7d/30d history. Data stays in a local profile.

The bundled schema-3 catalogue contains 171 definitions and 196 configured family series: 138 families have dated curator verification and 58 remain pending without automatic probing. GitHub catalogue updates retain the last-valid fallback. These receipts do not guarantee current reachability from your network or create local samples.

Hostnames remain stable identities. Each client records its actual attempted numeric address without another DNS lookup, then shows the first captured baseline and dated address changes. Changes are informational; normal DNS or CDN rotation does not alter endpoint health. Curator provenance never pins the client's destination.

## Start and stop

Extract the Windows amd64 ZIP into a folder and run `PingObservatory.exe`. No Go, Node.js, installer or external database is required. A local browser opens at `http://127.0.0.1:8742`; the application process must remain running. Closing the browser leaves collection running. Use Ctrl+C in the application console to stop it.

**Normal startup enables probes to verified public catalogue endpoints and configured manual targets.** To inspect the bundled catalogue or saved history with collection paused:

```powershell
.\PingObservatory.exe --collect=false --open-browser=false --listen 127.0.0.1:0
```

The console prints the available loopback URL. Use `--data-dir "C:\path\to\separate-profile"` for an isolated profile. The default Windows profile is `%AppData%\PingObservatory`. A second process cannot share an open profile. If port 8742 is occupied, choose `--listen 127.0.0.1:0`.

The binary is unsigned. Windows may display publisher or reputation prompts. This candidate does not include an installer, auto-start service or automatic executable updates. Catalogue updates are data-only. `--help`, `--licenses` and the local `/licenses.txt` page work without installing development tools.

## Data and upgrade limits

Opening a SQLite schema-2 profile upgrades it to schema 3. Existing measurement bytes are preserved and unknown historical peers stay unknown. **Older schema-2 executables cannot reopen the upgraded profile.** For rollback testing, stop the application and copy the entire closed profile before opening it with this candidate, or use a separate `--data-dir`.

Closed/asleep gaps remain N/A; observed probe failures are separate. Local destination history keeps at most 64 recent event details per series within retention, while the total accepted forward-time count can be larger. The default retention is 30 days, with a 4 GiB main database cap and visible capacity errors. Retention is not a guarantee that every permitted workload fits that cap. Details: [local destination tracking](https://github.com/Shaderx/PingObservatory/blob/51edfab96143b3b6051f65d02c29ee4be024ee35/docs/DESTINATIONS.md).

## Platform and acceptance limits

- **Windows amd64:** the candidate is intended for preview review on the tested Windows environment. Native ICMP IPv4/IPv6, protocol loopback fixtures and desktop/mobile UI checks pass. No claim is made for untested Windows versions.
- **Linux amd64:** source tests, permission-denial behavior, local protocol/API/persistence checks and bounded storage/journal tests pass on the approved cloud host. Successful native ICMP and visual browser behavior remain unverified under that host's restrictions. The Linux binary is prepared for review and withheld from the proposed supported download set.
- **Windows/Linux arm64:** compilation only; no runtime validation. These binaries are withheld from the proposed supported download set.
- **macOS:** deferred.

Active-clock interruption tests pass, but a physical machine suspend/resume acceptance run has not been performed for this candidate. No host power or security policy was changed. This is a release candidate for owner evaluation, not final MVP acceptance. No new live public endpoint verification was performed for this candidate.

The application is `AGPL-3.0-only`. The ZIP includes the licence and embedded third-party notices; the proposed release also includes corresponding source and SHA-256 checksums.
