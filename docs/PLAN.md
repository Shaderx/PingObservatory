# The Ping Observatory: Plan

A standalone network latency map for Windows and Linux. An initial scaffold is on `main`; the intended standalone LG-equivalent MVP is being corrected on `feature/mvp`. No executable release is published.

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
3. **Catalogue updates:** Implemented periodic bounded GitHub JSON fetching, strict validation, bundled starter and last-valid fallback. The version-2 master contains 171 definitions with 116 dated historical verified literal families and 80 pending hostname families. The legacy starter remains empty during final catalogue integration.
4. **Tests and packaging:** Windows native ICMP IPv4/IPv6 and protocol loopback checks pass. Linux suites, protocol fixtures, packaged TCP loopback and ICMP socket-denial checks pass; successful Linux native ICMP remains unverified. Single executable builds are prepared for Windows/Linux amd64/arm64. Arm64 runtime still requires a suitable test environment; no release is published.

See [verification details](TESTING.md) and [third-party notices](THIRD_PARTY.md).

## Vision correction in progress

The initial implementation narrowed the supported protocols, statistics and history too far. Preserve the existing application views with actual local measurements: five methods, IPv4/IPv6, provider/category/region filters, real batch/rolling statistics and baselines, and 1h/24h/7d/30d history. Replace the 100,000-row storage policy with sufficient bounded raw storage and lossless rollups. Populate the full sanitized master manifest from exact matching successful verification evidence. An independent vision audit must pass before completion.

## Licence decision

The application is licensed under the GNU Affero General Public License version 3 only (`AGPL-3.0-only`). See [LICENSE](../LICENSE).
