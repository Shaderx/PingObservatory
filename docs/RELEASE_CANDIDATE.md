# Release candidate review packet

The local packet proposes **v0.1.0-rc.1 as a Windows amd64 prerelease**. No release, tag or merge has been created. Final product acceptance remains pending. Release publication requires the owner's explicit decision; the prior source-publication approval does not cover publishing executable assets.

## Frozen source and artifacts

All four executables were built from clean source commit `51edfab96143b3b6051f65d02c29ee4be024ee35`, before the release-documentation reconciliation. Their Go build metadata records that exact VCS revision and `vcs.modified=false`. The release notes and acceptance-document updates are separate from the frozen executable source.

Toolchain: Go 1.26.5. Build: `CGO_ENABLED=0`, `-trimpath`, `-ldflags '-s -w'`, using the existing `scripts/build.ps1`. Windows acceptance ran with an ordinary user token on Windows build 26200, amd64. The executable is unsigned. Windows builds are PE binaries; Linux builds are static pure-Go executables. No installer, service registration or executable updater is included.

The prepared files are under `build/release-candidate/v0.1.0-rc.1/` in the working checkout. The `proposed-windows-release` directory contains:

| File | Bytes | Purpose |
| --- | ---: | --- |
| `PingObservatory-v0.1.0-rc.1-windows-amd64.zip` | 5,748,484 | Executable, start/stop instructions, release notes, application licence and embedded third-party notices |
| `PingObservatory-v0.1.0-rc.1-source.zip` | 1,334,221 | Complete Git archive of the exact executable source commit |
| `RELEASE_NOTES.md` | See checksums | Proposed release description and platform/migration limits |
| `SHA256SUMS.txt` | See packet | SHA-256 hashes of those three files |

Windows ZIP SHA-256: `4457a09fc4f5f6a40fbbbd2452afc53cb581df3ec8cf0ddd39e10f25ce28bffb`.

Windows executable SHA-256: `5ae87c3dfade469a14a4460c0ff1ea1c034bc8271e4a682db2e70b3eeb4354cc`.

Source ZIP SHA-256: `dc32a6656490079fc08bcb26a7ffa665025790a5096f418593b326afc9448bd5`.

The source archive includes Go and frontend sources, embedded assets, dependency manifests and locks, build scripts, the application licence and third-party notices. It contains no Git metadata, local profiles, SQLite histories, test outputs or node_modules. Source is also available at the [exact public commit](https://github.com/Shaderx/PingObservatory/tree/51edfab96143b3b6051f65d02c29ee4be024ee35). `main` still contains the earlier scaffold, so it is not the corresponding-source reference for this candidate.

The packet's `inventory.json` records all executable/archive hashes, byte lengths, build metadata and platform status. Linux amd64 and Windows/Linux arm64 archives are in `withheld-platforms`, outside the proposed supported download set. Linux amd64 has source/CLI/integration evidence but lacks successful native ICMP and visual browser acceptance. Arm64 is build-only.

## Acceptance evidence

The extracted Windows ZIP passes from a path containing spaces, with Go and Node.js absent from the application's PATH. Checks include help, embedded/local licence routes, populated offline startup with collection paused, local API boundaries, bounded shared-profile rejection, visible startup failures, real Ctrl+C exits with code zero, settings reopening and synthetic saved destination/history persistence. Test-owned processes exited.

The extracted executable's browser check passes manual method forms, map/callouts/origin, provider/category/world-area filters, loss sorting, comparison limits, IPv4/IPv6, all history windows, N/A and retention labels, favorites/theme persistence, motion and a 390-pixel layout. Screenshots were inspected. No horizontal overflow, browser errors or external UI requests were observed. The approved transparent callout design is retained.

An additional collector check first opens an isolated temporary profile with collection paused and verifies that all 196 public catalogue families are pending, updates are disabled and only three manual loopback jobs are eligible. The extracted executable then receives 3/3 ICMP replies from both literal loopback families and records a newer TCP result after browser closure. Measurements, captured addresses and zero-change baselines survive paused reopening. No public probes run. A prior `localhost` fixture hit its 300 ms DNS deadline; the application correctly recorded zero sent attempts and no captured peer. The deterministic native check uses literal loopback addresses, without changing product code.

Cloud acceptance for exact `51edfab` is recorded in [TESTING.md](TESTING.md), including the bounded 196-series journal workload. That expensive run was not repeated. The [release notes](RELEASE_NOTES.md) disclose the SQLite-3 profile upgrade, default public collection, historical catalogue evidence, unsigned binary, platform limits and unfinished physical suspend/resume acceptance.

## Precise publication decision

Approve or defer creating a **GitHub prerelease** named `v0.1.0-rc.1`, with its tag targeting exact source `51edfab96143b3b6051f65d02c29ee4be024ee35` and four attached assets: the Windows amd64 ZIP, corresponding-source ZIP, `RELEASE_NOTES.md` and `SHA256SUMS.txt`. Use the same reviewed notes for the release body, so every checksum entry has a downloadable artifact. Keep Linux/arm64 archives withheld. Do not merge the feature branch, change `main`, update the catalogue channel or label this final MVP acceptance as part of that action.

Successful Linux native ICMP, Linux visual browser behavior, arm64 runtime, physical suspend/resume acceptance and final product acceptance remain open. Observed bounded storage/journal results do not provide a universal 30-day capacity guarantee.
