# MVP verification

## Automated checks

```sh
go test -count=1 -timeout=90s ./...
go vet ./...
```

The Windows amd64 suite passes, including storage reopen/pruning/compaction, revision history, actual failures versus N/A, scheduler missed-cycle selection, strict catalogue/fallback handling and local HTTP/configuration boundaries. Independent review reran these checks successfully and the reported core findings were fixed.

The same six package test executables pass under Ubuntu WSL on Linux amd64. The packaged Linux executable, running as the existing WSL root user, serves its embedded UI/licences and records native loopback success alongside N/A gaps in its local history.

The frontend passes TypeScript compilation and five Vitest checks covering revision-aware data, attempted failures versus gaps, user-provided target revision updates and faithful verification metadata. Build assets, map geometry and licence notices are committed and embedded. Rebuilding the frontend uses the pinned lock file in `internal/web/frontend`.

Packaged Windows browser checks pass for settings, origin, map rendering under the content policy, directory selection, comparison, history and mobile layout. The isolated browser session made 16 local requests with no external UI requests or console/page errors. After browser closure, a newer successful loopback measurement was observed through the API, confirming that collection continues.

## Native probe checks

- Windows amd64, outside the tool's AppContainer sandbox: native ICMP loopback succeeds.
- Linux amd64, Ubuntu WSL ordinary user: existing `ping_group_range` is `1 0`; the probe reports `permission_denied` as expected.
- Linux amd64, the existing WSL root user: native raw ICMP fallback loopback succeeds. No capabilities, sysctls, firewall rules or other host settings were changed.
- Windows/Linux arm64: cross-compilation succeeds; runtime behavior is not yet verified.

Native integration tests are opt-in with `PINGOBSERVATORY_NATIVE_TEST=1`; a permission-denial check uses `PINGOBSERVATORY_PERMISSION_TEST=1`. They use loopback, not public targets or live Homelab services.

## Remaining verification

Successful Linux unprivileged ping sockets require a Linux environment whose existing policy permits them. Arm64 requires Windows/Linux devices or suitable runners. Owner-homelab endpoint verification evidence is required before populating the verified starter catalogue. No verification times are invented, and no executable release is published.
