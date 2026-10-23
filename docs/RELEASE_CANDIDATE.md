# Windows release packet: v0.1.0

The owner approved publication of the reviewed atlas correction as a full Windows amd64 release. Publish the feature source and an exact tag, attach the Windows ZIP, corresponding source, release notes and checksums, then verify public downloads. Main, the catalogue channel and earlier releases remain unchanged.

## Frozen product and release source

Product code and embedded frontend assets match independently reviewed and locally tested commit `46f1c5b2bc5f5c07bd22d18b0ecfe5b0ca3a7f2d`. The release source is a documentation-only successor, identified by the immutable `v0.1.0` tag and each binary's exact build record. Compare the product tree with `46f1c5b` before building. Any product change requires another test checkpoint before release.

Build only Windows amd64 for this supported download set, with Go 1.26.5, `CGO_ENABLED=0`, `-trimpath` and `-ldflags '-s -w'`. Build from a clean checkout. Require the exact release revision and `vcs.modified=false` in Go build metadata. The binary is unsigned. No installer, service registration or executable updater is included.

## Four public assets

| File | Contents |
| --- | --- |
| `PingObservatory-v0.1.0-windows-amd64.zip` | Executable, start/stop instructions, release notes, application licence and third-party notices |
| `PingObservatory-v0.1.0-source.zip` | Complete Git archive of the exact release source, including built frontend assets/maps and dependency manifests/locks |
| `RELEASE_NOTES.md` | Accurate Windows scope, presentation changes, profile migration, normal probe behavior and platform limits |
| `SHA256SUMS.txt` | SHA-256 hashes of the three assets above |

The local packet is under `build/release-candidate/v0.1.0/`. Its inventory records exact commit, binary/archive hashes, byte lengths and build metadata. Hashes are generated after packaging; the public checksum file is authoritative for downloads. Corresponding source excludes Git metadata, node_modules, test output and local profiles/history.

## Acceptance and limits

The v0.1.0 product checkpoint passes 77 frontend tests, TypeScript/production build and a clean Windows Go build. An isolated Chrome fixture verifies map picking, save/cancel/reload and stale-poll behavior, coloured rows and status/loss cues across ICMP/TCP/DNS, both themes, 390-pixel layout and an advancing connector stroke. It made no external requests or live probes. Independent Sol source review approved the final product patch. The unchanged backend retains prior Windows native IPv4/IPv6 loopback, protocol/API/persistence and vet evidence. Earlier rc.2 cloud Firefox acceptance and Linux startup cover the earlier product, not this v0.1.0 correction.

Before publishing v0.1.0, smoke-test the fresh extracted Windows package from a path containing spaces without Go/Node in PATH. Keep collection paused and updates disabled. Check embedded/licence/map routes, local API boundaries, profile locking, visible startup failures, Ctrl+C exit, origin saving and retained settings/history, plus the local browser workflows. Do not rerun expensive storage stress or public endpoint batches for this frontend correction.

Successful Linux native ICMP, full Linux workflow acceptance, arm64 runtime, physical suspend/resume and final product acceptance remain open. macOS is deferred. Linux and arm64 binaries are excluded from public assets. Existing SQLite schema-2 profiles upgrade to schema 3, which older executables cannot reopen. Stop and copy a closed profile for rollback testing. The 4 GiB main-database cap is not a universal 30-day capacity guarantee.

## Publication verification

Publish `v0.1.0` as a full GitHub release using the same reviewed notes as its body. Verify the remote feature source, exact tag target, `prerelease=false`, `draft=false`, four uploaded assets and anonymous HTTPS downloads. Hash every downloaded byte stream against the local packet and checksum file. Preserve rc.1, rc.2, main and the catalogue channel. No homelab deployment is part of this action.
