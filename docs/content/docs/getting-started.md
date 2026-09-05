---
title: Getting Started
weight: 1
---

## Prerequisites

- Chrome (or a Chromium-based browser: Brave, Edge, Vivaldi) version 144 or
  later.

## 1. Enable remote debugging

From inside the already-running browser, go to:

```
chrome://inspect/#remote-debugging
```

Check **"Allow remote debugging for this browser instance."**

This attaches to the real, already-open profile — no relaunch, no
`--user-data-dir` trick, no profile copying. Chrome shows a permission
dialog on each new connection request and a persistent "being controlled
by automated test software" banner while a session is active.

## 2. Connect

- **Local mode** (same machine, or you're already remoted in via
  screen-share): open Chrome Polish and connect to `127.0.0.1:<port>`.
- **Remote mode** (different machine/network, or ChromeOS): see
  [Remote Sessions]({{< relref "remote-sessions" >}}).

## 3. Scan, review, and clean up

Chrome Polish scans for candidate origins (cookies, service workers) and
their permission state, scores them, and presents a checklist. Select
what to remove and confirm.

## 4. Disconnect

Uncheck the remote-debugging toggle when you're done. In remote mode,
also close the SSH tunnel.
