# The Ping Observatory catalogue channel

This branch contains public catalogue data, verification receipts and their
documentation. It is the application's update channel, separate from source
development and executable releases.

The stable download is:

```
https://raw.githubusercontent.com/Shaderx/PingObservatory/catalogue/catalogue/latest.json
```

`catalogue/latest.json` is a complete manifest, currently identical to the
immutable `catalogue/v2.json` snapshot. Version 2 preserves 171 public target
definitions and 196 configured family series: 116 literal ICMP/IPv4 families
have historical success evidence dated 2026-10-09; 80 hostname families remain
pending and are not automatically probed. See `catalogue/evidence/README.md`.
Historical verification does not guarantee current or desktop reachability.

Each application embeds the same reviewed starter snapshot. A fresh offline
profile can therefore display the complete catalogue immediately. Startup and
six-hour refreshes download only this GitHub manifest; accepted updates are
persisted as a last-valid local cache. Failed, malformed, oversized, downgraded
or same-version conflicting updates keep the existing valid data. No Homelab
access is required by the application.

To publish an update, add a new immutable versioned snapshot and its reviewed
evidence, give the complete manifest a higher version, then advance `latest.json`
to that exact snapshot on this branch. Preserve earlier versioned files and
evidence commits. A probe-definition change requires a higher target revision.
Review pending-family changes explicitly. This channel contains data only and
cannot supply executable code or installation commands to the application.
