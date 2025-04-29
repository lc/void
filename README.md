# void

**Put distracting sites into the void. Works on macOS.**

Void is a lightweight, high-performance domain blocker that operates at the network level using macOS's built-in `pf` (Packet Filter). It blocks distracting websites by domain, resolving them to IPs and enforcing firewall rules—permanently or temporarily.

---

## Features

- Block domains with one command
- Temporary or permanent rules
- Uses macOS-native `pf` firewall (no kernel extensions)
- Automatically re-resolves blocked domains, expires old rules, etc.

---

## Install

```bash
go install github.com/lc/void/cmd/void@latest
sudo go install github.com/lc/void/cmd/voidd@latest
```

> Requires Go 1.20+ and root access to run the daemon (`voidd`).

---

## Usage

Start the daemon (must run as root):

```bash
sudo voidd
```

In a separate terminal, use the CLI:

```bash
void block facebook.com        # Permanently block
void block twitter.com 2h      # Temporarily block for 2 hours
void list                      # View all current blocks
```

---

## Config

By default, configuration is stored in `~/.void/config.yaml`:

```yaml
socket:
  path: /var/run/voidd.socket
rules:
  dns_refresh_interval: 1h
  dns_timeout: 5s
```

Defaults are sensible if no config file is found.

---

## Architecture
- `cmd/void`: User-facing CLI
- `cmd/voidd`: Background daemon
- `internal/`: Core engine, DNS resolver, rule management, pf integration
- `pkg/api`: Minimal HTTP-over-UNIX socket API
- `pkg/client`: CLI-to-daemon client

---
