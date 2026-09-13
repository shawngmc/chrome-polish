# TODO

Ideas for expanding cleanup scope beyond cookies/service-workers/storage
and notification permissions. All still bound by DESIGN.md's constraints:
CDP-only, no filesystem access, never navigate to a suspect origin, and
removal should use a real protocol command rather than synthetic UI
clicks.

## Other content-setting types

`chrome://settings/content/all` is already scraped for notification
grants (see DESIGN.md §4.2). The same shadow-DOM scraper could be
extended to cover other permission types malvertising abuses:

- Camera / microphone
- Geolocation
- Clipboard
- Auto-downloads (classic fake-installer vector)
- Popups/redirects

Reuses the existing scraper rather than adding a new one, so this is
lower-effort than it looks — main cost is the same shadow-DOM fragility
DESIGN.md §7.2 already calls out, just spread across more selectors.

## Protocol handlers

`chrome://settings/handlers` — malicious sites sometimes register custom
protocol handlers. Same read-only local-settings-page pattern as the
permission scraper.

## Background sync / periodic background sync registrations

Same CDP-domain shape as the existing service-worker enumeration
(`app/internal/scan/extensions.go` neighbors, conceptually) — browser-wide,
no navigation needed.

## Installed extensions

Today, extensions are purely cosmetic: `scan.DiscoverExtensionNames`
(`app/internal/scan/extensions.go`) reads `chrome://extensions-internals`
only to label `chrome-extension://` origins that already surfaced via the
cookie/service-worker scan (`id`/`name` fields only — see struct at
extensions.go:35-38). Nothing scores or removes extensions themselves.

To make extensions a real cleanup target:

1. **Detection/scoring** — extend `extensionInfo` to pull more fields
   already likely present in the `chrome://extensions-internals` JSON:
   permissions / host permissions, install location (webstore vs.
   sideloaded/unpacked/external), enabled state, from-webstore flag. Score
   the same way origins are scored today: `<all_urls>`-style host
   permissions, non-webstore install location, and a match against the
   existing blocklist manager — keyed by extension ID instead of hostname.

2. **Removal — open question, needs verification before committing to
   this feature.** The CDP `Extensions` domain has an `uninstall` command,
   but it's unconfirmed whether it works against normally-installed
   (webstore/sideloaded) extensions or only unpacked dev-mode ones. If
   it's dev-mode-only, that's the wrong case (scareware doesn't install as
   unpacked dev extensions), and there may be no real protocol command for
   removal at all — which would leave this blocked on the same synthetic-
   UI-click problem DESIGN.md explicitly avoids elsewhere.

   **Next step:** check what `chrome://extensions-internals` actually
   contains for permissions/install-location, and test `Extensions.uninstall`
   against a normally-installed extension on a live profile.
