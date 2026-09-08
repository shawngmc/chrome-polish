# Chrome Polish

A reusable, one-on-one tech-support tool for triaging and removing
scareware/malvertising artifacts (rogue notification permissions, service
workers, site storage) from a Chrome/Chromium profile — without wiping the
profile and without ever loading the suspected-malicious pages.

See [DESIGN.md](./DESIGN.md) for the full architecture and rationale.

## In-Chrome debugging setup

Chrome Polish connects to an already-running Chrome/Chromium profile over
the DevTools Protocol — no relaunch, no separate `--user-data-dir`. From
inside the browser you want to clean up (Chrome/Brave/Edge/Vivaldi 144+),
go to:

```
chrome://inspect/#remote-debugging
```

and check **"Allow remote debugging for this browser instance."** Chrome
will show a permission dialog on each new connection request and a
persistent "being controlled by automated test software" banner while a
session is active.

Then, in Chrome Polish, connect to `127.0.0.1:<port>` (local mode) — or
see the [Remote Sessions
docs](docs/content/docs/remote-sessions.md) for connecting from a
different machine, e.g. a ChromeOS target. Uncheck the remote-debugging
toggle when you're done. See [Getting
Started](docs/content/docs/getting-started.md) for the full walkthrough.

## Layout

This is a monorepo with three parts:

- [`app/`](./app) — the Go + Fyne control UI. Connects to a Chrome/Chromium
  instance over the DevTools Protocol (CDP), scans for candidate origins,
  scores them, and performs precise per-origin removal.
- [`relay/`](./relay) — the optional relay server for remote-mode sessions
  (e.g. ChromeOS targets that can't run an arbitrary executable). For v1, a
  plain `sshd` with a forwarding-only account is sufficient and this binary
  isn't required; it's the placeholder for a future purpose-built relay
  issuing short-lived tunnel tokens.
- [`docs/`](./docs) — end-user documentation, built as a static site with
  [Hugo](https://gohugo.io/) and the [Hextra](https://github.com/imfing/hextra)
  theme.

`app` and `relay` are independent Go modules tied together with a
[`go.work`](./go.work) workspace for local development.

## Building

```sh
# GUI app
cd app && go build -o chrome-polish ./cmd/chrome-polish

# Relay (not yet implemented — see relay/cmd/chrome-polish-relay)
cd relay && go build -o chrome-polish-relay ./cmd/chrome-polish-relay

# Docs site
cd docs && hugo server
```

Or via the root `Makefile`: `make build-app`, `make build-relay`, `make docs-serve`.

## Packaging for distribution

The GUI app is packaged with [Fyne's `fyne package`
tool](https://docs.fyne.io/started/packaging), which produces a real,
double-clickable app for each OS (a macOS `.app` bundle with an `.icns`
icon, a Windows `.exe` with the icon embedded, a Linux `.tar.xz`):

```sh
make package
```

This packages for whatever OS you run it on, into `app/cmd/chrome-polish/`
(gitignored). Override the version/build number with `APP_VERSION=1.2.0
APP_BUILD=3 make package`.

**Cross-platform builds happen in CI, not locally.** Fyne uses CGO for
native graphics, so a macOS machine can't produce a working Windows or
Linux build (see DESIGN.md section 6) — each OS's package has to be built
on that OS. [`.github/workflows/build.yml`](.github/workflows/build.yml)
handles this:

- **Every push/PR:** builds, vets, and tests both Go modules on Linux.
- **On a `v*` tag, or manually via "Run workflow":** packages the app for
  macOS, Linux, and Windows (one job per OS), cross-compiles the relay
  binary for linux/darwin/windows (amd64 + arm64 where applicable — the
  relay has no GUI/CGO dependencies, so unlike the app it *can* cross-compile
  from one runner), and on a tag push, publishes everything as a GitHub
  Release.

To cut a release: push a tag like `v0.1.0`.

The app icon lives at `app/Icon.png`; `fyne package` regenerates the
platform-specific icon formats (`.icns`, `.ico`) from it automatically.
The running (unpackaged) app also embeds it via a generated
`app/cmd/chrome-polish/bundled.go` — regenerate that file if the icon
ever changes with (run from `app/cmd/chrome-polish`):

```sh
go run fyne.io/fyne/v2/cmd/fyne bundle -package main ../../Icon.png > bundled.go
```

(strip the "tool is deprecated" notice `fyne bundle` prints above the
generated code before committing).
