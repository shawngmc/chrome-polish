---
title: Remote Sessions
weight: 2
---

Used when you can't get a full remote-desktop session onto the target
machine — including the ChromeOS case, where you can't run your own
executable at all.

## How it works

The debug port stays bound to `127.0.0.1` on the target machine.
Reachability comes from an SSH reverse tunnel initiated *from* the target
machine *to* a relay host:

```
ssh -R <relay_port>:127.0.0.1:<local_debug_port> user@your-relay-host
```

- **Windows / macOS / Linux:** the OS-bundled OpenSSH client runs this
  command as-is.
- **ChromeOS:** the built-in **Secure Shell** app (a Chrome Web Store
  extension) is a full SSH client and runs the same command.

Chrome Polish then connects to `relay-host:<relay_port>` instead of
`127.0.0.1:<port>` — everything downstream is identical to local mode.

## Relay host

The relay is optional infrastructure, not a hard dependency. A plain
`sshd` with `AllowTcpForwarding yes` and a restricted, forwarding-only
account is enough.
