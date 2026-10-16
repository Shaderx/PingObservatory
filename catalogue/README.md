# Endpoint catalogue

The application downloads versioned JSON from `Shaderx/PingObservatory/<ref>/catalogue/v<number>.json` on `https://raw.githubusercontent.com`. An empty URL disables updates. The bundled `v1.json` is an empty legacy starter; the schema-2 master and its verified population remain pending exact current evidence. No candidate is promoted by inference.

## Schema 2

A document contains `schema_version: 2`, a positive JavaScript-safe monotonically increasing `version`, an RFC 3339 `published_at` and a `targets` array. Limits are 4 MiB, 512 targets and 1,024 declared family slots; an oversized update is rejected visibly and never truncated. Fetches have a 15-second bound. Unknown or duplicate fields, trailing documents, invalid versions and invalid identities are rejected. An unavailable or rejected update retains the last valid local catalogue or bundled starter.

Each record retains its stable lowercase `id` (up to 64 characters), positive safe-integer `revision`, `label`, public DNS hostname or IPv4/IPv6 `host`, exact endpoint `port`/`scheme`/`path` when applicable, and intrinsic `measurement` settings. The `manual-` ID prefix is reserved locally. Public provider, category, region, world area, country, tags, approximate location, interpretation and source attribution may be retained. Private targets and operational configuration must be excluded.

Measurement methods are `icmp`, `udp_echo`, `tcp_connect`, `https` and `dns`, with explicit `ipv4`/`ipv6` families and a 60-second schedule. Settings must fit the native collector's supported bounds: ICMP requests may overlap while maintaining their configured spacing, GameLift echo uses port 7770, DNS uses resolver port 53 with explicit expected answers, and HTTPS uses GET or HEAD with expected statuses. The entire cycle must fit within 30 seconds.

Every declared family has an `admission` entry with state `verified`, `unverified`, `unsupported` or `disabled`. A pending state must explain its reason and cannot contain success evidence. It remains visible without automatic probing. A verified family requires `evidence` with the exact matching method, family, actual numeric destination, actual UTC `observed_at` and an HTTPS source URL. ICMP destinations are IP addresses; other methods use IP:port, with bracket notation for IPv6. The recorded port must match the endpoint, and a literal host must match the observed IP. Catalogue and runtime use the same public-unicast destination boundary.

A source review date, deployment date, generic validation label, hostname resolution performed later, synthetic fixture, HTTPS success or DNS success cannot substitute for the required method/family observation. Verification records a success from the owner's validation network at that time; it does not guarantee reachability from a desktop network. Desktop measurements remain separate.

## Identity and promotion

An intrinsic probe-definition change (host, port, scheme, path or measurement settings) requires a higher target revision. Display metadata and refreshed admission evidence can change at a higher catalogue version without partitioning historical measurements. A repeated catalogue version must be identical. Removed IDs remain in a bounded persistent revision ledger, so deletion cannot reset their revision. Schema 1 remains readable for legacy cache compatibility.

Before promotion, obtain only approved public endpoint definitions and bounded current ordinary successful observations. Match exact target identity, configured method, family, resolved numeric destination, port and actual timestamp. Preserve source attribution, increment the document version and review the exact public diff. Do not force probe batches or include credentials, private source addresses, local paths, private history or unrelated infrastructure records. Publish the sanitized manifest to GitHub; applications consume it without a homelab runtime dependency.
