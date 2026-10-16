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

Hostname-based families remain pending under the current numeric-peer evidence
contract. Existing successful hostname probes do not disclose the numeric peer
that was selected at that earlier time. No private target, station/origin record,
raw registry, local path, credential or deployment configuration belongs here.
