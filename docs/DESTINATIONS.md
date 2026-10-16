# Local destination tracking

A hostname remains the stable probe identity. Each client resolves it on its own network and records the exact numeric address passed to its native probe or socket adapter. The collector reuses that address for the attempt; it does not perform a second DNS lookup to populate history. HTTPS still uses the configured hostname for Host and TLS SNI.

The optional local measurement field `destination_ip` contains a canonical IPv4 or IPv6 address without a port. It means **attempted destination**, not a guarantee that the address replied. A measured timeout or refused connection can retain its attempted address. DNS failures, local setup or permission errors without an attempt, and discarded batches omit it. Clock discontinuities discard both statistical outcomes and the captured address.

## Baseline and changes

Local state is isolated by target ID, intrinsic revision, method and address family. The first accepted capture seeds the baseline and latest address, with zero changes. Later forward-time captures update the latest address and timestamp. A different address increments `change_count` and records `observed_at`, `old_ip` and `new_ip`. Returning from A to B to A counts as two changes.

These timestamps are batch completion times, not DNS resolution or packet-send times. A retained late batch can keep its captured IP, but does not rewind the latest address or reconstruct transitions. The existing newest-per-minute storage rule still applies: an older batch in an occupied minute is ignored. An identical retry does not add a change, and conflicting addresses at the same accepted timestamp are rejected atomically.

Destination changes are informational. DNS and CDN rotation can be normal. They do not change probe status, loss, latency or health. The UI shows the dated last attempted address, first captured baseline, total accepted forward-time change count and recent old/new events separately from curator verification.

The API exposes optional `destination` metadata on observations and history responses:

```json
{
  "baseline_ip": "127.0.0.1",
  "baseline_observed_at": "2026-10-16T12:00:00Z",
  "last_ip": "127.0.0.2",
  "last_observed_at": "2026-10-16T12:01:00Z",
  "change_count": 1,
  "history_limit": 64,
  "history": [
    {
      "observed_at": "2026-10-16T12:01:00Z",
      "old_ip": "127.0.0.1",
      "new_ip": "127.0.0.2"
    }
  ]
}
```

This is a synthetic loopback example, not catalogue evidence. Unknown historical peers stay unknown. The `destination_ip` field and this state describe local attempts; they do not replace the catalogue's verification scope or destination fields.

## Retention and profile compatibility

At most 64 recent change details are kept per series, and API responses also apply the requested history window and local retention. The baseline, latest address, their dates and total count are constant-size metadata. They can outlive individual retained events while the series remains active; the count can therefore exceed the number of displayed details. A local error without a captured address leaves the dated last attempted address intact.

When an inactive series falls outside retention, its destination state is immediately hidden from reads and is deleted with the series during bounded maintenance. A new active epoch seeds a fresh baseline even if physical cleanup has not finished. Destination rows have a cascading foreign key to the existing series table.

Opening an existing SQLite schema-2 profile upgrades `user_version` to **3** and adds the destination table. It does not scan old records, perform DNS lookups, backfill addresses or rewrite their compact bytes. Compact measurement version 2 remains readable; records with captured addresses use version 3, including in mixed archived chunks. **Older schema-2 executables cannot open the upgraded profile.** Keep a separate profile when comparing older executables.

## Catalogue updates

The manifest remains the same GitHub-hosted schema-3 document. Curator verification describes a dated success on the curator's network. An observed curator peer is provenance, never a global pin or a substitute for the client's geographic DNS answer.

A refresh that changes only display or verification provenance preserves intrinsic target identity and revision, so it preserves local destination state and measurement history. Fetching, caching or importing verification never seeds a local baseline or emits a destination event. Failed downloads, rejected downgrades and last-valid fallback leave existing state intact. A family that becomes pending stops automatic probing while its retained local destination history remains visible and dated. Changes to the intrinsic host, port, method or other probe identity require the usual new revision and a separate local series.
