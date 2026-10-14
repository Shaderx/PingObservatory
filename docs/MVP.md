# MVP implementation breakdown

The MVP follows the public plan and uses AGPL-3.0-only. Implementation is on `feature/mvp`. The owner approved sanitized reuse and publication of the existing application frontend; independent review is required before pushing. No executable release is published.

## Build and runtime

- Use Go to produce one executable per Windows/Linux architecture, with the local verification UI and starter catalogue embedded.
- Keep the collector in the application process. Closing a browser tab does not stop collection.
- Bind the HTTP UI to loopback. Store configuration and the database in a per-user application directory.
- Probe IPv4 through Windows ICMP APIs and Linux ICMP datagram sockets. Report unsupported or permission-denied probe operations explicitly. Do not change host security settings.

## Data contract and collector

- Identify each target by a stable ID and integer revision. Store both on each measurement so target changes do not rewrite history.
- Record only actual attempts, with a timestamp, status and optional latency. Represent absent minute buckets as `N/A`; never turn missing records into packet loss.
- Stagger probes across each minute, with bounded concurrency and timeouts. After a stop, sleep or clock gap, resume from the current schedule without replaying missed work.
- Allow the user to set an origin location and enter an explicitly user-provided target. User-provided targets are not labelled as verified catalogue records.

## Storage and catalogue

- Use a local embedded database, bounded retention and a row cap. Keep configuration and the last-valid catalogue locally.
- Fetch bounded JSON over HTTPS from a configured GitHub catalogue URL. Validate schema, stable target identities, revisions, verification metadata and version progression before adopting it.
- Embed an empty starter catalogue until real targets have been verified. Do not invent verified target records. If an update fails, retain the last-valid catalogue or bundled starter.
- Treat catalogue data as data: do not execute commands, scripts or plugins supplied by it.

## UI and verification

- Integrate the existing application's map, directory, detail and history presentation with the local API. Remove fixed origin, old branding, external publication URLs and private records; preserve map and dependency licences.
- Test storage restart/retention, failed attempts versus missing buckets, missed-schedule handling, catalogue rejection/fallback, bounded HTTP input and loopback access.
- Test native probes with loopback in each available OS. Cross-compilation alone does not verify runtime behavior.
- Obtain an independent implementation review, fix findings, and record available test environments and unresolved blockers.
