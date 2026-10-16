# Standalone implementation

Development on `feature/mvp` preserves the approved LG interface in a standalone Windows/Linux executable. This is a development checkpoint; final acceptance remains pending. No executable release is published. The application licence is `AGPL-3.0-only`.

## Runtime and probes

The application owns a background collector and an embedded loopback web UI. Closing the browser does not stop collection. Settings, identity revisions, the last-valid catalogue and observations remain in a per-user profile. Each scheduled series uses the target's intrinsic method and declared address family.

Native probes support ICMP, GameLift UDP echo on port 7770, TCP connect, HTTPS and DNS on resolver port 53 with explicit expected answers. IPv4 and IPv6 are separate series. Batches are bounded to 30 seconds and staggered once per minute, without catch-up after downtime. The application never changes host permissions or security settings.

Actual sends, received outcomes and local inability to measure are separate counters. Packet loss is calculated only for measured ICMP/UDP outcomes; TCP/HTTPS/DNS use failed-attempt rates. Outcomes made unmeasurable by local errors are excluded from denominators. Clock discontinuities during a probe discard that batch's statistical outcomes. Missing history remains N/A.

## Storage and statistics

Pure-Go SQLite keeps 26 hours of compact raw batches, then preserves original counts and individual RTT values, timestamps and sequence numbers in 30-minute chunks. The default retention is 30 days. The main database is bounded to 4 GiB, with one connection and an 8 MiB page cache. Capacity failure is explicit; no unexpired row cap truncates history. Mature catalogue density and active journal bounds remain acceptance checks.

Statistics separate the latest batch from the rolling hour. Medians, type-7 p95 and population standard deviation are recalculated from individual timings. A 24-hour baseline needs at least five successful timings, including valid zero-duration replies, and a positive resulting median. Freshness uses observation timestamps. Timing is elevated only when a fresh batch is both at least 50% and at least 10 ms above its fresh baseline and rolling data is fresh.

The map, animations, favorites, theme, configurable origin, family selection, provider/category/world-area filters, method filter and same-method/family comparison are retained. History uses completed UTC-aligned 1h/1-minute, 24h/5-minute, 7d/30-minute and 30d/2-hour grids. Expired retention is labelled separately from ordinary gaps.

## Catalogue and acceptance

The catalogue pipeline is ordinary homelab validation, sanitized versioned GitHub JSON, then desktop download. Each admitted method/family needs an exact matching endpoint and recorded successful observation, including the actual numeric destination and timestamp. Pending definitions stay unverified and are not scheduled. Schema, revision continuity, size limits and last-valid fallback are enforced. No commands or plugins are executed from catalogue data.

The embedded version-2 starter preserves 171 definitions, with 116 dated historical literal successes and 80 pending hostname families. Its evidence does not claim current or desktop reachability, and local history starts empty. Updates use the data-only `catalogue` branch's complete `catalogue/latest.json`, independent of the application's source branch. A fresh offline profile is populated immediately; rejected updates retain the valid local cache or starter. The desktop has no runtime homelab dependency. Verification and remaining acceptance are recorded in [TESTING.md](TESTING.md). Independent review is required before accepting the correction as complete. macOS remains deferred.
