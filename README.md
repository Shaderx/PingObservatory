# The Ping Observatory catalogue channel

This branch contains reviewed public catalogue data, verification receipts,
documentation and the licence. Source development and executable releases are
separate. The complete stable download is:

```
https://raw.githubusercontent.com/Shaderx/PingObservatory/catalogue/catalogue/latest.json
```

`catalogue/latest.json` is byte-identical to immutable `catalogue/v3.json`.
Schema 3 preserves 171 definitions and 196 configured families: 116 historical
numeric-destination and 22 explicitly hostname-level successes are verified;
58 families remain pending without automatic probing. These are dated October 9
observations, not current or desktop reachability guarantees. See the evidence
README for the exact distinction and the limits of derived literal receipts.

Hostname receipts omit a numeric destination because the old probe's peer was
not retained. Each client resolves and validates its own destination. Curator
provenance never globally pins geographic DNS or creates local measurements.

The application embeds the same reviewed v3 snapshot for immediate offline
startup. Startup and six-hour refreshes accept complete bounded manifests and
retain a last-valid cache. Failed, malformed, oversized, downgraded or conflicting
same-version updates keep valid local data. Older schema-2 applications reject
schema 3 and keep their v2 cache/starter; no executable release is published.

Publish future releases as new immutable snapshots and reviewed evidence, then
advance `latest.json` to that exact higher-version document. Preserve v2/v3 files
and evidence commits. Intrinsic probe-definition changes require higher target
revisions; provenance-only updates preserve local measurement identity/history.
This data-only channel supplies no executable code or installation commands.
