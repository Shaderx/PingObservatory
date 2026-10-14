# The Ping Observatory

A standalone network latency map for Windows and Linux.

## Status

This project is in the planning stage. No executable is released.

## Planned product

- A single executable that runs a background collector and a local web UI.
- Generic branding and local data storage.
- Gaps while the application is closed or the system is asleep displayed as `N/A`.
- Normal probe failures displayed separately from those gaps.
- Periodic updates from a versioned, verified endpoint catalogue on GitHub, with a bundled starter catalogue and a last-valid fallback.

macOS support is deferred until testing is available.

## Initial milestones

Define the data contract and platform probe support, then implement local storage and the UI, catalogue updates, and Windows/Linux tests and packaging. See [the plan](docs/PLAN.md).

## Licence

The application licence is pending owner selection.
