# Local atlas presentation

This candidate adapts the owner-approved Looking Glass presentation to the standalone local application. Its runtime base is `51edfab`; later inherited changes document that release candidate. This change updates the frontend and its embedded assets. Collector, settings, catalogue, SQLite, history, rollups and actual-destination tracking retain their existing contracts.

Compact provider aliases, method labels, timing values, positive-rate bars and status patterns retain full target names and reasons in row help. ICMP and UDP use packet loss; TCP, HTTPS and DNS use failed-attempt rates. Unknown rates remain unknown, and a positive rate below 0.1% has precise help rather than a displayed zero. Elevated timing uses the existing fresh-baseline policy. An unsent local failure is informational; an observed network failure has a separate pattern.

Distance colours use the configured local origin, including latitude/longitude zero, and the original target coordinates. The reference is `max(10 ms, distance_km * 0.01 + 10 ms)`, with green through 150% and red from 300%. It is a display heuristic. Missing, stale, invalid and non-RTT inputs use neutral colours. Regional aliases affect display grouping only.

Only a newly completed local batch can enter arrival motion. Saved data, the initial snapshot, delivery recovery, hidden snapshots, filters and revision changes do not replay old results. Pending work retains its original monotonic acceptance time and 15-second expiry when a newer result replaces it. Each location has one active signal; equally old siblings rotate after actual launches across subsequent batches. Pacing follows demand and measured frame/construction pressure. Expired or invisible animations can be dropped without affecting measurements or storage.

Floating labels use measured dimensions and actual control rectangles. If they cannot fit, every mapped reading moves into a scrollable list beside the map. Narrow frames put that list below the map. The list accepts pointer, scrollbar and keyboard input. Layout mode follows the frame width. The initial map fit runs once; resize, theme, filters and fullscreen preserve the current camera.

Native fullscreen has an in-page fallback, keyboard containment and focus return. Row help uses the active fullscreen root and closes before an additional Escape exits the fallback. The local origin marker, signal directory, detailed statistics, settings and history remain available.

Cloud Firefox ESR acceptance passed on exact product commit `be141f5cad1dd76529494e38bd016bbcee619a00`, including resize, shared-location queue fairness, expiry, visibility, reduced motion and fullscreen. Linux startup and exact packaged assets also passed. Synthetic history/settings routes returned HTTP 501 deliberately, so that replay did not requalify backend workflows. The Windows preview retains the other platform and final-product limits in [verification notes](TESTING.md).
