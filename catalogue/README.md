# Endpoint catalogue

`v1.json` is the schema-1 starter catalogue embedded in the executable. The
starter is empty until actual endpoint verification evidence is available. Do
not invent verification dates or copy a private endpoint inventory into it.

The application accepts only HTTPS raw GitHub URLs in this repository with the
path `Shaderx/PingObservatory/<ref>/catalogue/v<number>.json`. Catalogue JSON is
data only: unknown fields, duplicate keys, trailing documents and oversized
responses are rejected. A fetch has a 15-second limit and a 256 KiB body limit.
Invalid or unavailable updates leave the last valid catalogue active. The
bundled starter is used if no valid local cache exists.

The document contains `schema_version` (currently `1`), a positive monotonically
increasing `version` (at most `9007199254740991`, JavaScript's largest exact
integer), an RFC 3339 `published_at`, and a `targets` array with at
most 64 records. Publication times cannot move backwards. A repeated version
must have identical content.

Each target contains:

- `id`: a stable lowercase identifier, up to 64 characters (`a-z`, `0-9`, `.`,
  `_`, `-`; starts with a letter or digit).
  The `manual-` prefix is reserved for local user targets.
- `revision`: a positive integer at most `9007199254740991`. Every change to the target record requires a
  higher revision, including a change in host or verification metadata.
- `label`: a nonempty plain text display name, up to 128 bytes.
- `host`: a lowercase fully qualified public DNS name or a public unicast IPv4
  address; URLs, ports and command arguments are not accepted.
  IPv6 literals are deferred until the native probes support IPv6.
- `location` (optional): numeric `latitude` and `longitude` in degrees, bounded
  to `[-90, 90]` and `[-180, 180]` respectively.
- `verified_at`: the actual RFC 3339 time of a successful verification, no later
  than `published_at`.
- `verification_source`: `owner-homelab`. This means the endpoint was tested
  from the owner's homelab. It does not guarantee a reply from every client.
- `verification_method`: the actual successful method, one of `icmp`,
  `udp_echo`, `tcp_connect`, `https`, or `dns`.
- `verification_family`: `ipv4` or `ipv6`, matching the observed destination.
- `verification_destination`: the exact public IP used for an ICMP result, or
  public IP and port for UDP echo, TCP connect, HTTPS or DNS. An IPv6 destination
  with a port uses bracket notation. For a literal target host, the observed IP
  must match that host. For a DNS target, review the actual result's target
  identity and resolved destination against the public endpoint definition.

The MVP collector probes IPv4 ICMP. Catalogue verification records identify
the method and family that actually succeeded; another method or IPv6 success
does not establish IPv4 ICMP reachability. Clients can record attempted ICMP
failures for an endpoint whose verification used another method or network.

Removed IDs remain in the local revision ledger, so a reintroduced target
cannot reset its revision or change content at the same revision. The ledger
is bounded to 4,096 identities and 2 MiB. The cache is written to a temporary
file in the same directory, synced, and renamed over the previous cache.
New directories use mode `0700` and cache files use mode `0600` on Linux.
Windows uses the permissions inherited from the per-user data directory.

## Verification before promotion

Public endpoint definitions and provider documentation identify candidates.
They do not prove that an endpoint replied to a probe from the owner's homelab.
A source review date, a synthetic fixture, a deployment date or a general
`validated` label is insufficient without an actual reachability timestamp and
the relevant probe result.

Before adding a record to `v1.json`:

1. Confirm that the endpoint definition is public and keep its public source
   attribution. Exclude private targets, local origin settings and operational
   configuration from the catalogue.
2. Prefer bounded, read-only current production metadata or detail results, or
   a scoped current saved result. An existing successful homelab observation
   with its actual recorded timestamp qualifies; forced probe batches are not
   needed. Use production results rather than synthetic fixtures, and do not
   promote a record that only reports failure.
3. Match the result's exact target and destination to the public definition.
   Record its actual UTC success timestamp, successful method, address family,
   and public destination IP (plus port for a transport method). A source-review
   date or a label alone cannot establish this match.
   Keep credentials, private source addresses, local paths and unrelated
   measurement history out of public evidence.
4. Set `verified_at` to that actual timestamp and `verification_source` to
   `owner-homelab`. Preserve the observed method, family and destination in the
   corresponding verification fields. Set `published_at` to the publication time, increment the
   catalogue version, and assign or increment the target revision as required.
5. Run catalogue validation and review the exact public diff before publishing.
   A client can still record timeout or other attempted-probe failures for an
   endpoint verified from a different network.

An unverified candidate file must use a distinct candidate schema, mark every
record `unverified`, and omit `verified_at`. It must never be embedded as the
starter, used as the updater URL, or loaded into automatic probing. The
application only embeds `v1.json`; candidate JSON is a review artifact.
