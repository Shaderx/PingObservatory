# The Ping Observatory: Plan

A standalone network latency map for Windows and Linux. The MVP is implemented on `feature/mvp`; no executable release is published.

## Product requirements

- Deliver a single executable that runs a background collector and a local web UI.
- Use generic branding.
- Store data locally.
- Display gaps while the application is closed or the system is asleep as `N/A`.
- Display normal probe failures separately from those gaps.
- Periodically fetch a versioned, verified endpoint catalogue from GitHub.
- Include a bundled starter catalogue and retain a last-valid fallback.
- Defer macOS support until testing is available.

## Milestone sequence

1. **Data contract and platform probe support:** Implemented stable target IDs/revisions, actual attempt records and native Windows/Linux IPv4 probes. Missing minute buckets are N/A; attempted failures retain their explicit status.
2. **Local storage and UI:** Implemented bounded local storage, one-minute staggered background collection, per-user settings and the embedded local map/history UI. The origin is configurable.
3. **Catalogue updates:** Implemented periodic bounded GitHub JSON fetching, strict validation, bundled starter and last-valid fallback. The starter remains empty pending real owner-homelab endpoint verification evidence.
4. **Tests and packaging:** Unit/integration tests and Windows/Linux amd64 native loopback checks pass. Single executable builds are prepared for Windows/Linux amd64/arm64. Arm64 runtime and successful unprivileged Linux ping sockets still require suitable test environments; no release is published.

See [verification details](TESTING.md) and [third-party notices](THIRD_PARTY.md).

## Licence decision

The application is licensed under the GNU Affero General Public License version 3 only (`AGPL-3.0-only`). See [LICENSE](../LICENSE).
