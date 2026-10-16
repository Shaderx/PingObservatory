# Catalogue verification receipts

`v2.json` preserves 116 historical ICMP/IPv4 success receipts dated 2026-10-09.
It establishes endpoint/method/family success at the recorded time, not current
availability or reachability from a desktop network.

- 101 records combine a captured public literal definition with its matching
  contemporaneous successful 3/3 batch. Their numeric destination is derived from
  that literal definition; the original public observation did not contain a
  separate destination field. No later DNS lookup was used.
- 15 records retain three explicit numeric-destination rounds per endpoint:
  three replies from three sends in each round. Their verification timestamp is
  the actual recorded completion of the final successful round. The provider
  URL identifies the endpoint definition; the round receipts are the success
  evidence published here.

The source observation and round timestamps are batch timestamps. They are not
newly generated per-packet success times. Display coordinates remain approximate.

The standalone relay profile uses three packets per minute, two-second spacing,
a two-second timeout and a 32-byte payload. The payload is an explicit desktop
default, not a recovered source wire-payload measurement. The 15 added relay
definitions also use explicitly selected desktop spacing; it is not claimed as
their recovered historical spacing. The success receipts verify the exact
endpoint, method and family, rather than every desktop sampling parameter.

`v3.json` adds 22 explicitly hostname-level receipts from the same saved October 9
audit: 16 GameLift UDP family observations, four Fortnite IPv4 ICMP observations
and two cdnjs HTTPS family observations. Each retains the configured public
hostname, applicable port/path/method, matching successful batch and its exact
recorded timestamp. `numeric_peer_recorded: false` states the evidence limit.
The records contain no numeric destination and use no later DNS lookup. These
receipts support hostname-level success, rather than a claim about an unrecorded
historical IP. The version-3 manifest keeps the 116 earlier numeric receipts and
admits these 22 hostname families; 58 families without matching saved success
remain pending. Version 2 and its evidence remain immutable.

Curator evidence is provenance. Clients resolve hostnames from their own network;
neither a numeric receipt nor a provenance refresh supplies a global destination
pin or a local measurement. Public source snapshots may later disappear; these
sanitized, commit-pinned receipts preserve the selected observations and their
limits. No private target, station/origin record, raw registry, local path,
credential or deployment configuration belongs here.
