# Third-party software and map data

The embedded frontend uses the following third-party components. Their licence terms remain in force; the application licence does not replace them.

| Component | Version | Licence |
| --- | --- | --- |
| React and React DOM | 19.1.1 | MIT |
| MapLibre GL JS | 6.11.2 | BSD-3-Clause |
| Apache ECharts | 6.1.0 | Apache-2.0 |
| Natural Earth world geometry | 1:110 million | Public domain |
| world-atlas geometry distribution | 2.0.2 | ISC |
| Go standard library | Build toolchain version | BSD-3-Clause |
| modernc.org/sqlite and compiled native dependencies | See go.mod and embedded notices | Individual licences in go-dependencies.txt |
| golang.org/x/sys | See go.sum | BSD-3-Clause |

Complete dependency licence and notice texts are bundled in [THIRD-PARTY-NOTICES.txt](../internal/web/frontend/public/THIRD-PARTY-NOTICES.txt), and the map terms are in [maps/LICENSE.txt](../internal/web/frontend/public/maps/LICENSE.txt). Both are available from the local web UI. Fonts come from the device; no font assets are downloaded.

Native dependency notices are embedded in the executable and available at `/licenses.txt` or with the `--licenses` command. The complete application AGPLv3 text is also served at `/LICENSE`; the UI links to the public source repository.

Build tooling and its dependencies are pinned in the frontend package lock. They are required to rebuild the web assets, but they are not required to run the executable.

## Frontend import review

The owner authorised reuse of the existing Looking Glass application frontend. The retained components are its stylesheet and device font policy; atlas frame, local map, marker and viewport helpers; transparent map callout layout and arrival animation; and ECharts history presentation. The new application shell retains the atlas, signal directory, detail panel, theme toggle, favorites and comparison layout while using the local API.

The imported map assets are `world.geojson` and `maps/LICENSE.txt`. The selected frontend dependency manifest and lock retain the pinned third-party dependencies. No target records or measurement histories are included in the frontend.

The local adapters preserve the method-aware LG view contract: IPv4 and IPv6, provider/category/world-area filters, same-method comparisons, batch and rolling-hour statistics, positive 24-hour baselines, and one-hour, one-day, seven-day and thirty-day histories. Statistics come from the local collector and storage API. The one-hour grid uses one-minute buckets to match the desktop schedule; long windows retain the original five-minute, thirty-minute and two-hour presentation grids. No missing samples or catalogue verification are substituted for local observations. Coverage outside the configured retention period is labelled separately from N/A gaps.

Brand names, fixed origin, publication endpoints, old data contracts, verification claims and publication-specific controls were removed or replaced. New local settings and API adapters provide configurable origin and manual targets. No original repository history, private configuration, credentials, deployment files or infrastructure records were imported.
