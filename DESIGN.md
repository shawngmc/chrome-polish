# Chrome Cleanup Assistant — Design Document

**Status:** Draft v1
**Purpose:** A reusable, one-on-one tech-support tool for triaging and removing
scareware/malvertising artifacts (rogue notification permissions, service
workers, site storage) from a Chrome/Chromium profile belonging to someone
else — a family member, a friend, a client — without wiping their profile
and without ever loading the pages you suspect are malicious.

---

## 1. Goals and non-goals

**Goals**
- Rapid, low-friction cleanup: minutes, not a full profile reset.
- Reusable across sessions/people — not hard-coded to one relative or one
  machine.
- Works uniformly across Windows, macOS, Linux, and ChromeOS (the last one
  being the hard case: no ability to run an arbitrary executable there).
- Works whether you're physically at the machine, remoted in via
  screen-share, or genuinely on a different network entirely.
- **Never loads a page from a suspected-malicious origin.** Every read of
  browser state comes from the browser's own local bookkeeping, not from
  fetching/executing the flagged site's content.
- Removal is precise (per-origin, per-data-type) — cookies, service
  workers, cache storage, IndexedDB, local storage — not "clear everything."

**Non-goals**
- Fleet / enterprise device management. This is a one-on-one support tool.
- Password/credential decryption or recovery.
- A general-purpose browser automation framework. CDP usage here is
  scoped tightly to what cleanup needs.
- Defending against a sophisticated attacker who already has code
  execution on the target machine — this tool assumes the browser profile
  is compromised by ordinary scareware/malvertising (rogue permissions,
  push-spam service workers), not that the OS itself is compromised.

---

## 2. Preconditions

- **Chrome ≥ 144** on the target machine (current stable is 152.x, so "make
  sure Chrome is up to date" is a reasonable, low-friction first step for
  any session). This applies to Chrome, and should also hold for other
  Chromium-based browsers (Brave, Edge, Vivaldi) that ship the same
  `chrome://inspect/#remote-debugging` surface — worth a quick check per
  browser the first time, not assumed.
- The person enables remote debugging **once per session**, from inside
  their already-running browser:
  `chrome://inspect/#remote-debugging` → check *"Allow remote debugging for
  this browser instance."*
  - This attaches to their real, already-open profile. No relaunch, no
    `--user-data-dir` trick, no profile copying.
  - Chrome shows a permission dialog on each new connection request and a
    persistent "being controlled by automated test software" banner while
    a session is active — the person always has a visible signal and an
    easy way to know when to turn it back off.
  - Confirmed present on ChromeOS as the same in-browser toggle (not
    gated behind Developer Mode, since it's a browser feature rather than
    an OS/CLI-flag feature).

---

## 3. Two connection modes

### 3a. Local mode (control UI and browser on the same machine)

No relay needed. The debug endpoint Chrome opens is bound to
`127.0.0.1`, so the control tool just connects directly to
`ws://127.0.0.1:<port>/...`. This is the path for: you're sitting at the
machine yourself, or you're already fully remoted into it via
screen-share/remote-desktop software (in which case you have a real local
session on that machine and can just run the tool there).

### 3b. Remote mode (control UI on a different machine/network)

Used when you can't get a full remote-desktop session onto the target
machine, or specifically to solve the Chromebook problem: you cannot run
your own executable there.

The debug port stays bound to loopback (Chrome doesn't expose it on the
network by itself, and shouldn't). Reachability comes from an **SSH
reverse tunnel** initiated *from* the target machine *to* a relay host you
control:

```
ssh -R <relay_port>:127.0.0.1:<local_debug_port> user@your-relay-host
```

- **Windows / macOS / Linux:** the OS-bundled OpenSSH client runs this
  command as-is — no custom binary of yours needed on their machine.
- **ChromeOS:** the built-in **Secure Shell** app (a real Chrome Web Store
  extension, not Crostini/Linux) is a full SSH client and runs the same
  kind of command.
- Your control tool then connects to `relay-host:<relay_port>` instead of
  `127.0.0.1:<port>` — everything downstream (CDP session, scanning,
  removal) is identical code; only the connection target differs.

This means **the relay is optional infrastructure, not a hard dependency
of the tool's architecture** — the CDP session layer just takes a
host:port and doesn't care how it got reachable.

**Relay host itself:** simplest option is a small VPS or homelab box
running plain `sshd` with `AllowTcpForwarding yes` and a restricted,
single-purpose account (no shell, `ForceCommand` or a locked-down
`authorized_keys` entry, forwarding only). A custom-written mini SSH
server (Go's `golang.org/x/crypto/ssh` can act as a server, not just a
client) is a further-out option if per-session, auto-expiring, no-shell
tunnel tokens turn out to matter — noted as a possible v2, not required
for v1.

---

## 4. Component architecture

```
┌───────────────────────────────────────────────────────────┐
│ Control UI (GUI)                                           │
│  - connection panel (local port  |  relay host:port)        │
│  - scored checklist table, multi-select                    │
│  - detail pane (why flagged)                                │
│  - remove button + confirmation                             │
└───────────────────────────────────────┬────────────────────┘
                                         │
┌────────────────────────────────────────▼───────────────────┐
│ CDP session layer (transport-agnostic: ws://host:port)      │
├───────────────────────────────────────────────────────────  │
│ Origin discovery       │ Permission-state read │ Removal    │
│ - Storage.getCookies   │ - navigate a tab to   │ - Storage. │
│   (browser-wide, no    │   chrome://settings/  │ clearData  │
│   navigation)          │   content/all (local, │ ForOrigin  │
│ - ServiceWorker domain │   no network to the   │   per      │
│   registration events  │   flagged origin)     │   selected │
│   (browser-wide, no    │ - DOM read, piercing  │   origin   │
│   navigation)          │   shadow roots         │            │
├────────────────────────┴────────────────────────┴───────────┤
│ Reputation scoring (pure function over collected data:       │
│  low visit signal + granted notifications, abused TLDs,      │
│  suspicious hostnames, optional user-supplied blocklist)      │
└───────────────────────────────────────────────────────────  ┘
```

Key property carried over from the last round of discussion: **at no
point does this pipeline load a page from a candidate/suspect origin.**
Cookies and service-worker origins come from browser-wide CDP
enumeration; permission state comes from Chrome's own local settings UI,
not the flagged site.

### 4.1 Origin discovery
- `Storage.getCookies` — full cookie table, browser-wide, one call.
- `ServiceWorker.enable` + collect `workerRegistrationUpdated` events for
  a short window — every currently-registered service worker and its
  scope, browser-wide, no navigation.
- These two sources are unioned into the candidate-origin set.

### 4.2 Permission-state read
- Navigate a background tab to `chrome://settings/content/all` (a local,
  static Chrome page — no network request to any listed site).
- The page is a Shadow-DOM-heavy web-components (Polymer) app, so reading
  it needs a DOM walk that pierces shadow roots — either CDP's
  `DOM.getDocument`/`DOM.describeNode` with a pierce option, or an
  injected recursive script via `Runtime.evaluate` that descends through
  `.shadowRoot` on each element.
- This is read-only scraping and is expected to be somewhat fragile
  across Chrome versions (selectors may shift). Because it's read-only,
  a version mismatch degrades gracefully — that data point is just
  unavailable until updated, not a functional break. **Do not** attempt to
  drive UI clicks on this page for removal — there's at least one
  community report of Chrome resisting synthetic clicks on settings
  pages, and removal has a real protocol command anyway (below), so
  clicking the UI is never necessary.

### 4.3 Removal
- `Storage.clearDataForOrigin(origin, storageTypes)` per selected origin,
  with `storageTypes` scoped to exactly what's selected (cookies,
  service_workers, cache_storage, indexeddb, local_storage, etc.) —
  precise, not a blanket wipe.
- No file-level edits, no LevelDB parsing, no profile copying: this
  command is the actual, supported mechanism, and it's already covered
  by the live-mode work from the earlier prototype.

### 4.4 Reputation scoring
Unchanged in principle from the earlier prototype: heuristic scoring
(notification permission + low visit signal, abused TLDs, suspicious
hostname keywords) plus an optional user-supplied blocklist file. This
step operates purely on already-collected data and has no transport or
navigation dependency of its own.

---

## 5. Session lifecycle

1. Confirm Chrome ≥ 144 (upgrade first if not).
2. Person enables `chrome://inspect/#remote-debugging`.
3. (Remote mode only) person runs the one-line SSH reverse-tunnel command.
4. Tool connects (`127.0.0.1:port` locally, or `relay-host:port`
   remotely), accepts the on-screen Chrome permission dialog.
5. Scan: origin discovery + permission read. No origin pages loaded.
6. Score + present checklist; person/you multi-select what to remove.
7. Confirm, then `Storage.clearDataForOrigin` per selection.
8. Disconnect. Remind the person to uncheck the remote-debugging toggle
   (and close the terminal/tunnel in remote mode) — this is meant to be a
   short-lived, supervised session, not something left running.

---

## 6. Tech stack

The Python + PyQt6 prototype worked functionally but packaging/
distribution is the real complaint (bundling an interpreter + Qt via
PyInstaller across three OSes is genuinely painful and fragile). Given
this is meant to be handed out and reused, not just run from your own dev
machine, distribution simplicity matters as much as the GUI toolkit
itself.

| | Go + Fyne | Rust + egui | Python + PyQt6 (prior) |
|---|---|---|---|
| Packaging | Single static binary per OS/arch via `GOOS`/`GOARCH`; no bundled runtime | Single static binary per OS/arch; no bundled runtime | Bundled interpreter + Qt via PyInstaller/cx_Freeze — heavy, version-fragile |
| Websocket/CDP client | Mature (`nhooyr.io/websocket`, `gorilla/websocket`) | Mature (`tokio-tungstenite`) | Works (`websocket-client`), fine |
| SSH (for the tunnel/relay side) | `golang.org/x/crypto/ssh` — mature, widely deployed (Docker, k8s tooling), works as **both client and server** in pure Go | `russh` — workable but less battle-tested | Would shell out to system `ssh` |
| Dev velocity | Fast; simple concurrency model (goroutines/channels) maps naturally onto "wait for a command reply while also collecting background events" | Slower iteration, borrow-checker overhead for what's a fairly ordinary networked CRUD tool | Fast, but packaging cost eats the gain |
| GUI maturity for this shape (table + checkboxes + detail pane) | Fyne's widget set is more traditional/retained-mode; a checkbox-table is a bit of manual wiring but well-trodden | egui's immediate-mode model plus `egui_extras::TableBuilder` handles this shape well too | QTableWidget handled this natively, was the strongest part of the prior build |
| Cross-compiling the GUI itself | Needs CGO for native graphics on most platforms, so still generally built per-OS in CI rather than true cross-compiled — but CI matrix builds are simple and standard | Same caveat — also generally built per-OS in CI | N/A (interpreted, but that's the packaging problem) |

**Recommendation: Go + Fyne.** The deciding factors:
- It directly answers your packaging complaint — a `go build` per
  target OS/arch produces one file, no interpreter or Qt runtime to ship.
- `golang.org/x/crypto/ssh` covers *both* sides of the remote-mode
  transport (a Go relay-server component, and — for non-Chromebook
  targets where you might one day want to avoid asking the person to
  type an `ssh` command by hand — an embeddable tunnel client) in the same
  language as the rest of the tool, rather than shelling out to the
  system `ssh` binary.
- Your existing Kubernetes/Harbor/Istio homelab work is already
  Go-ecosystem-adjacent, so tooling and deployment patterns (static
  binaries, straightforward CI builds) should feel familiar.
- Rust + egui is a completely reasonable alternative if you'd rather
  build in Rust for its own sake or already have code you like there —
  the main practical cost versus Go is slower day-to-day iteration for
  what is, underneath the CDP specifics, a fairly ordinary networked
  tool, plus a slightly less mature SSH server story if you go the
  custom-relay route later.

---

## 7. Open questions before implementation

1. **Relay auth model.** Plain `sshd` with a locked-down forwarding-only
   account is enough for v1. A custom Go SSH server issuing short-lived,
   single-use tunnel tokens is a reasonable v2 if this gets used often
   enough that handing out durable SSH credentials feels wrong.
2. **Shadow-DOM scraper maintenance.** Since `chrome://settings/content/
   all`'s internal structure isn't a stable public API, worth deciding
   now whether to pin against a specific Chrome version's structure and
   fail loudly on mismatch, or degrade gracefully (skip permission
   scoring, keep cookie/service-worker scoring) — leaning toward the
   latter.
3. **Multi-browser support.** Brave/Edge/Vivaldi should work identically
   since they share the `chrome://inspect/#remote-debugging` surface —
   worth a quick manual check per browser rather than assuming.
4. **The person's exact runbook.** A short, plain-language "what you'll
   see and click" sheet for the family member/friend, separate from this
   design doc, so the same tool can be hand-tuned per person's comfort
   level without changing the tool itself.

---

## 8. Summary of what changed from the last two rounds

- Dropped the offline SQLite/Preferences-file scanner entirely — this
  design assumes no local filesystem access to the target machine at
  any point.
- Dropped the "launch Chrome against a copy of the profile" workaround —
  no longer needed given the `chrome://inspect/#remote-debugging` flow
  attaches to the real, already-running profile directly.
- Local mode needs no relay at all; remote mode's relay is an SSH reverse
  tunnel, not a raw `--remote-debugging-address=0.0.0.0` exposure.
- Permission-grant detection reads Chrome's own local settings page
  instead of visiting the candidate origin.
- Proposed rewrite target: Go + Fyne, replacing the Python/PyQt6
  prototype, primarily to solve packaging/distribution.
