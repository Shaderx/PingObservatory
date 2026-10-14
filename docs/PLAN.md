# The Ping Observatory: Plan

A standalone network latency map for Windows and Linux. This project is in the planning stage; no executable is released.

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

1. **Data contract and platform probe support:** Define the data contract and probe support for Windows and Linux, including the distinction between gaps and normal probe failures.
2. **Local storage and UI:** Implement local data storage, the background collector, and the local web UI in a single executable.
3. **Catalogue updates:** Implement periodic fetching of the versioned, verified GitHub catalogue, the bundled starter catalogue, and the last-valid fallback.
4. **Tests and packaging:** Test and package the application for Windows and Linux.

## Licence decision

The application is licensed under the GNU Affero General Public License version 3 only (`AGPL-3.0-only`). See [LICENSE](../LICENSE).
