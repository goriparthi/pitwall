# Component specification

| Component | Anatomy | States / rules |
|---|---|---|
| Metric card | Source, metric name, value/unit, trend, timestamp | Loading/live/stale/unavailable. Stable number width. Unknown never becomes 0. |
| Status card | Source, label, icon, explanation, timestamp | Healthy/degraded/failed/unknown only with evidence. |
| Attention item | Severity icon, title, source, time, next action | New/acknowledged/resolved. No color-only distinction. |
| Template card | Category, name, preview, connection requirements | Available/setup required/incompatible. Explain incompatibility. |
| Workspace selector | Name, profile, active state | Keyboard accessible; no overwrites when switching. |
| Connection row | Provider, scope, auth status, freshness | Connected/stale/disconnected/permission required. Mask secrets. |
| Quick action | Icon, verb + object, shortcut | Ready/running/succeeded/failed/disabled. Confirm destructive operations. |
| Display card | Device name, resolution, workspace, preview | Connected/offline/unsupported. No unsupported control claims. |
| Primary button | Verb, optional directional icon | Default/hover/focus/pressed/disabled/busy. One prominent primary per region. |
| Toast | Result, useful next action | Nonblocking; does not replace persistent attention errors. |

Light theme: lightCanvas/lightSurface/lightText/lightSecondary tokens. Use graphite brand buttons with lime detailing on light surfaces; recompute borders and semantic foreground colors for contrast. OS appearance belongs to user preference.
