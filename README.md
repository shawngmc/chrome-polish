# Chrome Polish

A reusable, one-on-one tech-support tool for triaging and removing
scareware/malvertising artifacts (rogue notification permissions, service
workers, site storage) from a Chrome/Chromium profile — without wiping the
profile and without ever loading the suspected-malicious pages.

See [DESIGN.md](./DESIGN.md) for the full architecture and rationale.

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
