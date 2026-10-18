# Windows preview release packet: v0.1.0-rc.2

The owner approved publication of the reviewed presentation changes and the next Windows amd64 prerelease. This packet follows the rc.1 process: publish the feature source and an exact tag, attach the Windows ZIP, corresponding source, release notes and checksums, then verify public downloads. Main, the catalogue channel and earlier releases remain unchanged.

## Frozen product and release source

Product code and embedded frontend assets match independently reviewed and Windows/Firefox-tested commit `be141f5cad1dd76529494e38bd016bbcee619a00`. The release source is a documentation-only successor, identified by the immutable `v0.1.0-rc.2` tag and each binary's exact build record. Compare the product tree with `be141f5` before building. Any product change requires another test checkpoint before release.

Build only Windows amd64 for this supported download set, with Go 1.26.5, `CGO_ENABLED=0`, `-trimpath` and `-ldflags '-s -w'`. Build from a clean checkout. Require the exact release revision and `vcs.modified=false` in Go build metadata. The binary is unsigned. No installer, service registration or executable updater is included.

## Four public assets

| File | Contents |
| --- | --- |
| `PingObservatory-v0.1.0-rc.2-windows-amd64.zip` | Executable, start/stop instructions, release notes, application licence and third-party notices |
| `PingObservatory-v0.1.0-rc.2-source.zip` | Complete Git archive of the exact release source, including built frontend assets/maps and dependency manifests/locks |
| `RELEASE_NOTES.md` | Accurate preview scope, presentation changes, profile migration, normal probe behavior and platform limits |
| `SHA256SUMS.txt` | SHA-256 hashes of the three assets above |

The local packet is under `build/release-candidate/v0.1.0-rc.2/`. Its inventory records exact commit, binary/archive hashes, byte lengths and build metadata. Hashes are generated after packaging; the public checksum file is authoritative for downloads. Corresponding source excludes Git metadata, node_modules, test output and local profiles/history.

## Acceptance and limits

The product checkpoint passes 75 frontend tests, TypeScript/production build, Windows Go tests with native IPv4/IPv6 loopback and vet. Paused packaged Windows Chrome checks cover dense layouts, list pointer/keyboard input, camera preservation, fullscreen, tooltips and paced arrivals. Approved cloud Firefox ESR replay verified the exact source/packet/assets, corrected overlap/control obstruction and resize preservation, shared-location fairness, expiry, hidden-tab resume and reduced motion. Linux startup passed. Its deliberate HTTP 501 history/settings fixtures did not requalify backend workflows.

Before publishing rc.2, smoke-test the fresh extracted Windows package from a path containing spaces without Go/Node in PATH. Keep collection paused and updates disabled. Check embedded/licence/map routes, local API boundaries, profile locking, visible startup failures, Ctrl+C exit, settings and saved-history persistence, plus the local browser workflows. Do not rerun expensive storage stress or public endpoint batches for this documentation-only successor.

Successful Linux native ICMP, full Linux workflow acceptance, arm64 runtime, physical suspend/resume and final product acceptance remain open. macOS is deferred. Linux and arm64 binaries are excluded from public assets. Existing SQLite schema-2 profiles upgrade to schema 3, which older executables cannot reopen. Stop and copy a closed profile for rollback testing. The 4 GiB main-database cap is not a universal 30-day capacity guarantee.

## Publication verification

Publish `v0.1.0-rc.2` as a GitHub prerelease using the same reviewed notes as its body. Verify the remote feature source, exact tag target, `prerelease=true`, `draft=false`, four uploaded assets and anonymous HTTPS downloads. Hash every downloaded byte stream against the local packet and checksum file. Preserve rc.1, main and the catalogue channel. No homelab deployment is part of this action.
