# Standalone LG-equivalent MVP contract

The initial scaffold omitted supported product semantics. The correction preserves the approved application's behavior in a portable executable with per-user local storage, without a desktop runtime dependency on the homelab.

## Data and catalogue

- Preserve each public target's stable ID, intrinsic method, configured families, endpoint definition and public provider/geography/source metadata.
- Admit method/family pairs separately. Dated successful evidence must match the configured endpoint, method, family and actual recorded time, with numeric-destination or hostname-level scope stated explicitly. Hostname-level evidence omits an unrecorded numeric peer; it does not imply current reachability. HTTPS or DNS success does not verify an ICMP entry.
- Preserve unverified definitions explicitly without scheduling them. Reject an oversized manifest visibly rather than truncating it.
- The curator uses bounded ordinary homelab observations as verification input, then publishes a sanitized versioned GitHub JSON manifest. Desktop applications download that manifest and probe locally.

## Probing and measurement semantics

- Restore ICMP, UDP echo, TCP connect, HTTPS and DNS for IPv4 and IPv6 with the original bounded protocol settings.
- Stagger configured batches once per minute, with no historical catch-up. Preserve actual packet counts, spacing, successes and measurement timestamps.
- Keep unsent DNS, permission, unsupported and local errors separate from genuine sent timeouts or failures. Packet loss applies to ICMP/UDP; other methods expose failed network attempts with accurate labels.
- Detect clock discontinuity during active probes. Discard affected samples and loss denominators; stopped/asleep gaps remain N/A.

## Storage, statistics and views

- Use pure-Go SQLite. Retain recent raw data and lossless 30-minute count/RTT rollups, with 30-day retention by default and explicit storage-capacity errors.
- Support the complete known catalogue without a 100,000-row history cap. The main database has a 4 GiB bound; journal and maintenance space must also be bounded.
- Compute actual batch median/min/max, 1h sent/received/loss or failed-attempt rate, type-7 p95, population standard deviation and a fresh positive 24h baseline with sufficient observed samples.
- Restore family selection, provider/category/world-area filters, method-aware labels, conservative degradation, same-method/family comparison, favorites and origin location.
- History windows use truthful local sampling: 1h/1-minute, 24h/5-minute, 7d/30-minute and 30d/2-hour grids. Missing bins contain no invented measurements. Retention-expired regions must be explicit.

## Completion checks

Native Windows/Linux protocol fixtures, actual loopback behavior, persistence/rollup capacity, statistics against reference calculations, sleep during an active probe, populated-manifest evidence, restored UI behavior, and an independent vision/publication audit are required before claiming completion. macOS remains deferred. No host security or live service changes are authorized.
