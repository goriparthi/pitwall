# Pitwall / brand and product system v1.0

## Product definition
Pitwall is a personal command center built from templates. It brings useful metrics, status, and actions into a glanceable workspace on a dedicated display or main screen. AI development, system administration, and everyday usage are equally valid entry points.

Tagline: **Your setup. In view.**
Descriptor: **A personal command center, built from templates.**
Short description: **Turn the things you track into a dashboard that fits your work.**
Avoid promising universal device support, remote control, exact costs, cloud privacy, or available integrations before they exist. This kit is a design foundation, not a launched product or name-clearance result.

## Asset map
- brand/: custom SVG wordmarks, symbols, and horizontal lockups in light/dark/lime. Font-independent paths.
- icons/macos/: legacy .icns, AppIcon.appiconset, transparent menu-bar template images, and separate background/foreground SVG layers for modern Icon Composer authoring.
- icons/windows/: .ico, PNG size exports, and baseline MSIX scale and target-size assets.
- icons/web/: SVG/ICO favicon, 192/512 PNGs, Apple touch icon, manifest, and social card.
- design/: JSON tokens, CSS variables/base components, component specifications.
- previews/: full-resolution display and desktop concepts. All figures are demo data.
- website/: responsive static landing-page concept; replace placeholder contact and unimplemented claims before publishing.
- Brand-Guide.html: visual guide. Open after extracting the entire folder.

## Logo rules
Use the lowercase spelling in the drawn wordmark; prose uses Pitwall. Keep the symbol’s lanes and counter intact. Clear space is at least one stem width around the symbol and one lowercase i stem width around wordmarks. Minimum digital wordmark width: 96 px; lockup: 132 px; full symbol: 24 px. At 16–20 px use the supplied simplified micro glyph, which omits the lower lane. Do not squash, rotate, outline, apply gradients to the wordmark, or recolor semantic status icons to brand lime. Prefer light lockups on graphite and dark lockups on off-white. Lime is for sparse emphasis; never use lime text on white.

## Typography
Native apps: SF system fonts on macOS, Segoe UI/Variable on Windows, system-ui on web. Do not redistribute platform fonts. Data: SF Mono or Consolas through OS fallbacks. Use tabular numbers. Native UI sizes: 12 metadata, 14 labels, 16 body, 20 section, 28 title. Website: 16–20 body, 40 section, 64–88 hero. Display reference: 18 px labels minimum, 24 px support, 48–56 px primary values; verify physical reading distance before final release. Avoid dense all-caps except short metadata.

## Template model
Template → configured Workspace → layout of Widgets → Connections as data sources.
Template cards show title, category, preview, widgets, required connections, compatible display profiles, and author/version. Category names are Systems, AI Development, Everyday; add future categories without changing the brand.
Picking a template must create an editable workspace, never silently overwrite one. If a connection is missing, show a setup action and honest placeholder. Export/import versioned template JSON with explicit bindings. Never package credentials, machine paths, or user identities into shared templates.

## State model
Every widget supports loading, live, stale, unavailable, empty, and permission-required. An unavailable connection means its service state is unknown, not unhealthy. Include last-updated time. Agent states add working, idle, waiting, completed, failed, unknown; process presence alone does not prove task activity. Attention items need source, reason, timestamp, and a useful next action. Acknowledge and resolve are different actions.

## Layout and interaction
The 1920×480 reference uses 24 px outer margins, 16 px gaps, a 64 px header, and four equal cards. Other templates may use 2–6 widgets based on information density. The display is glance-first: editing occurs on the main computer. Do not assume touch. Without input, never show controls that appear tappable on the hardware. Use companion controls or keyboard shortcuts to switch workspaces. Native desktop shell: sidebar, workspace toolbar, configurable grid, attention list, connections and display settings. Persist layout per display profile.

## Color and accessibility
Use tokens.json and pitwall.css as the authority. Brand accent is Pit Lime #D5FF45, with #10171C text. Text #F3F5EF and secondary #B1BFC7 on #141C22 are high-contrast combinations. Muted text is for supplementary information only; verify contrast for every actual combination. Status colors always pair with label + shape/icon. Focus uses a 3 px blue outline with 4 px offset. Target at least 44×44 px for touch and 32×32 px desktop control hit areas. Full keyboard navigation, accessible names, announcements for significant state changes, reduced motion, OS contrast/theme behavior, and 200% zoom are release requirements.

## Motion
120 ms hover/focus, 180 ms state transitions, 280 ms panel entry. No continuously pulsing idle gauges or flashing errors. Animate a working state gently only when supported by telemetry. Disable decorative motion when reduced-motion is enabled. Cap refresh to useful intervals and make paused/frozen data obvious.

## Native icons
The ICNS and Xcode appiconset are legacy raster deliverables. Modern Apple workflows use Icon Composer; supplied SVG background and foreground layers are inputs, not a compiled .icon document. Review masks and material behavior in Xcode before shipping. Keep menu-bar template glyphs black with transparent background and set the native template-image flag.
Windows ICO includes 16/20/24/32/40/48/64/128/256 sizes. Baseline MSIX assets cover Square44x44Logo and Square150x150Logo scales 100/125/150/200/400, target-size variants, and StoreLogo. Connect these to your application manifest and verify any additional requirements for your chosen packaging model. Do not stretch the icon to fill wide tiles. Test real taskbar, dock, tray, installer, and Store rendering.

## Voice and website
Clear, compact, operational. Prefer “Choose a template,” “Connect a source,” “Edit workspace,” “Last updated 12s ago,” and “Connection unavailable.” Avoid AI hype and unsupported claims. Primary website CTA before release: Explore templates or Join the waitlist. After verified release: Download for macOS / Download for Windows, with minimum OS and hardware support nearby. Never add fabricated testimonials or numbers.

## Launch handoff
Before release: check name/trademark and domain availability; confirm software and hardware compatibility; wire real download/contact URLs; publish truthful privacy/data handling documentation; produce screenshots from working builds; validate accessibility and OS icons; sign/notarize installers as appropriate; add support and release notes. No legal/name clearance or native build validation has been performed in this kit.

Primary platform references (checked 2026-10-03):
https://developer.apple.com/design/human-interface-guidelines/app-icons
https://learn.microsoft.com/en-us/windows/apps/design/style/app-icons-and-logos
https://learn.microsoft.com/en-us/windows/apps/design/style/iconography/app-icon-construction
