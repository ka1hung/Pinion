<p align="center"><img src="build/appicon.png" width="128" alt="Pinion"></p>

<h1 align="center">Pinion</h1>

<p align="center">
  A cross-platform terminal for system integrators and field engineers: SSH, Serial, Telnet, SFTP, tunnels and quick file servers in one window.<br>
  Windows · macOS · Linux ｜ MIT License
</p>

<p align="center"><a href="README.md">繁體中文</a> ｜ <b>English</b></p>

> The user interface is currently in Traditional Chinese; labels quoted below are translated.

<!-- Screenshots: TODO -->

---

## Contents

- [Features](#features)
- [Download and install](#download-and-install)
- [Getting started](#getting-started)
- [Keyboard shortcuts](#keyboard-shortcuts)
- [Feature guide](#feature-guide)
- [Where data is stored](#where-data-is-stored)
- [Security](#security)
- [Building from source](#building-from-source)
- [Architecture](#architecture)
- [License](#license)

## Features

| Area | What you get |
| --- | --- |
| SSH | Tabs and split view; password / key / SSH agent; jump hosts (ProxyJump, multi-hop); keepalive and reconnect |
| Serial | Port list with USB device names and chips (CH340, CP210x, FTDI, PL2303); HEX view, timestamps, TX/RX lights; DTR / RTS / BREAK |
| Telnet | Full IAC negotiation; works with old switches and building controllers |
| TCP / UDP | Client and server modes; send UTF-8 text, JSON or HEX; incoming JSON is pretty-printed and colorized |
| Local shell | PowerShell / cmd on Windows (ConPTY), your default shell on macOS / Linux |
| Sessions | Grouped by site folders; search, drag to reorganize, duplicate, import / export (without passwords) |
| SFTP | Remote files appear in the sidebar once SSH connects; follows the shell's directory; upload / download (drag and drop); remote copy / move; permissions dialog; full keyboard control |
| Editor | Built-in VS Code editor core (Monaco, works offline); UTF-8 / Big5, LF / CRLF; detects remote changes before saving and shows a diff; save with sudo when permission is denied; or use an external editor such as VS Code (saves are uploaded automatically) |
| SSH tunnels | Local / Remote / Dynamic (SOCKS5); one-click start/stop; optional auto-start with the session |
| Quick servers | One-click TFTP, HTTP and FTP with a chosen folder and interface; read-only by default, stops when idle (firmware flashing) |
| More | MultiExec (send to several tabs), session logs (file names include site and time), dark / light themes, session file import (.ini / .mxtsessions) |

## Download and install

Get the files from [Releases](https://github.com/ka1hung/pinion/releases):

| Platform | File | First launch |
| --- | --- | --- |
| Windows 10 / 11 (x64) | `Pinion-<version>-windows-x64.zip` (portable) or `…-setup.exe` | Not code-signed yet; SmartScreen warns: click "More info" → "Run anyway" |
| macOS (Apple Silicon) | `Pinion-<version>-macos-arm64.zip` | Not notarized yet: right-click Pinion.app in Finder → "Open", or run the command below |
| Linux (x64, Ubuntu 24.04+) | `Pinion-<version>-linux-x64.tar.gz` or `.deb` | Needs GTK4 and WebKitGTK 6.0 |

Verify downloads with `SHA256SUMS.txt`:

```bash
sha256sum -c SHA256SUMS.txt --ignore-missing
```

Windows (PowerShell):

```bash
Get-FileHash .\Pinion-<version>-windows-x64.zip
```

If Windows reports that WebView2 is missing (Windows 11 ships with it):

```bash
winget install Microsoft.EdgeWebView2Runtime
```

macOS — remove the quarantine flag:

```bash
xattr -dr com.apple.quarantine /Applications/Pinion.app
```

Linux runtime packages:

```bash
sudo apt install libgtk-4-1 libwebkitgtk-6.0-4
```

Linux serial ports require the `dialout` group (log in again afterwards):

```bash
sudo usermod -aG dialout "$USER"
```

TFTP on port 69 needs a capability on Linux (the `.deb` sets it automatically):

```bash
sudo setcap cap_net_bind_service=+ep /usr/local/bin/Pinion
```

## Getting started

1. **Connect**: type `user@host:port` into "Quick connect" (top right) and press Enter. `telnet://host`, `tcp://host:port`, `udp://host:port`, `COM3` and `/dev/ttyUSB0` also work.
2. **Save a session**: click "+ Session" in the toolbar, fill it in and choose a folder (e.g. `Plant A/Server room`). Passwords go to the system keychain.
3. **Files**: after an SSH session connects, the sidebar switches to SFTP. Double-click a file to edit it, Ctrl+S saves it back to the server.
4. **Bring your sessions**: toolbar "Import", then pick a session file (.ini / .mxtsessions); folders are preserved.

## Keyboard shortcuts

On macOS, replace **Ctrl+Shift** below with **⌘** (e.g. Ctrl+Shift+T → ⌘T).

### Global

| Keys | Action |
| --- | --- |
| Ctrl+Shift+T | New local terminal |
| Ctrl+Shift+N | New session |
| Ctrl+Shift+K | Quick connect (search sessions or type an address) |
| Ctrl+Shift+W | Close tab |
| Ctrl+Tab / Ctrl+Shift+Tab | Next / previous tab |
| Ctrl+Shift+1 … 9 | Go to tab 1–9 |
| Ctrl+Shift+B | Toggle sidebar |
| Ctrl+Shift+E | Focus the SFTP file list |
| Ctrl+Shift+, | Settings |
| Middle-click a tab | Close it |

### Terminal

| Keys | Action |
| --- | --- |
| Ctrl+C | Copy when text is selected; otherwise interrupt (^C) |
| Ctrl+V | Paste |
| Ctrl+Shift+C / Ctrl+Shift+V | Copy / paste |
| Ctrl+Insert / Shift+Insert | Copy / paste |
| Middle click | Paste |
| Right click | Menu (copy, paste, select all, find, clear); can be switched to "copy if selected, otherwise paste" in Settings |
| Ctrl+Shift+F | Find (Enter next, Shift+Enter previous, Esc close) |
| R | Reconnect after the connection dropped |

macOS: ⌘C / ⌘V copy and paste, ⌘F find; Ctrl+C always interrupts.

### SFTP file list

Click the list first, or press Ctrl+Shift+E.

| Keys | Action |
| --- | --- |
| ↑ ↓ / Home End / PgUp PgDn | Move |
| Shift+↑ ↓, Ctrl+click, Shift+click | Multi-select |
| Ctrl+A | Select all |
| Enter / double-click | Open folder or file |
| Backspace, Alt+↑, `..` | Parent folder |
| Ctrl+C / Ctrl+X / Ctrl+V | Copy / cut / paste (copied or moved on the server) |
| Drag onto a folder or `..` | Move (hold Ctrl to copy) |
| F2 | Rename |
| Delete | Delete |
| F5 or Ctrl+R | Refresh |
| F7 or Ctrl+Shift+N | New folder |
| Ctrl+D / Ctrl+U | Download / upload |
| Ctrl+Shift+C | Copy path |
| Type letters | Jump to a matching file name |
| Esc | Back to the terminal |

### Editor

Same as VS Code; the most used ones:

| Keys | Action |
| --- | --- |
| Ctrl+S | Save (to the server or local disk) |
| Ctrl+F / Ctrl+H | Find / replace (regex supported) |
| Ctrl+D | Select next occurrence (multi-cursor) |
| Alt+click | Add cursor |
| Ctrl+/ | Toggle comment |
| Ctrl+Z / Ctrl+Y | Undo / redo |
| Ctrl+G | Go to line |

## Feature guide

### Sessions and folders

- The "Sessions" sidebar groups sessions by folder (site). Drag sessions between folders; right-click a folder to add subfolders, rename, connect to everything inside, or export.
- Search matches names, IPs, folders and notes.
- Per session: tab color, terminal encoding (Big5 for older devices), automatic log, startup command.

### SFTP and the editor

- When an SSH tab connects, the sidebar shows its remote files; switching to another tab brings the session list back (can be turned off in Settings).
- "Follow terminal folder": the file list follows `cd` in the terminal (works with `user@host:~/dir$`-style prompts or shells that report their directory).
- Drag files from your file manager into the list to upload.
- Right-click → "Permissions…" shows owner / group / others × read / write / execute checkboxes and an octal field (e.g. 755).
- Before saving, the editor checks whether the remote file changed and shows a diff so you can overwrite or reload; if you lack write permission you can save with sudo.
- Files above the size limit (10 MB by default) can be opened read-only.

### Serial and TCP / UDP

- Toolbar toggles for HEX view and timestamps; TCP / UDP also have JSON formatting.
- The send box at the bottom supports UTF-8 / HEX / JSON and a line ending (CR / LF / CRLF); ↑↓ recalls history.
- Serial: toggle DTR / RTS and send BREAK; if a port fails to open you get platform-specific hints (drivers, permissions).

### SSH tunnels

Add rules in an SSH session's "Tunnels" tab, then start / stop them from the sidebar's "Tunnels" view. Tick "Auto" to start them with the session.

| Type | Use it to | Example |
| --- | --- | --- |
| L (local) | Reach a device's service from your PC | `127.0.0.1:8080` → `192.168.10.5:80`, then open `http://127.0.0.1:8080` |
| R (remote) | Let the remote host reach a service on your PC | remote `127.0.0.1:9000` → local `127.0.0.1:8080` |
| D (dynamic) | SOCKS5 proxy into the whole network | bind `127.0.0.1:1080`, set your browser's SOCKS5 proxy to `127.0.0.1:1080` |

Keep the bind address at `127.0.0.1` so nobody else can reach the customer network through your PC.

### Quick servers

The "Servers" sidebar starts and stops TFTP (69), HTTP (8080) and FTP (2121) with a chosen folder and network interface. They are read-only by default, stop when idle, and list transfers below. Your firewall may ask the first time.

### MultiExec

Turn on "MultiExec" in the toolbar to get checkboxes on tabs. Type a command in the bar at the bottom to send it to every checked tab, or enable "Type in all" to mirror your typing.

### Importing session files

Toolbar "Import", then choose a session file from another terminal tool (`.ini` or an exported `.mxtsessions`). SSH (including jump hosts, key paths and startup commands), Telnet, Serial and local shells are imported with their folder structure; the encoding (UTF-8 / Big5) is detected automatically, and unsupported types are listed and skipped. Passwords in those files are encrypted and are not imported; enter them on first connect and tick "Remember".

## Where data is stored

| Platform | Location |
| --- | --- |
| Windows | `%APPDATA%\Pinion` |
| macOS | `~/Library/Application Support/Pinion` |
| Linux | `~/.config/Pinion` |

It contains `sessions.json` (no passwords), `settings.json` and `known_hosts`. Session logs go to `~/Pinion/logs` and quick servers share `~/Pinion/share` by default; both can be changed in Settings.

## Security

- **Passwords and key passphrases** live in the OS credential store (Windows Credential Manager, macOS Keychain, Linux Secret Service). On Linux without Secret Service they go to a file encrypted with a master password (Argon2id + AES-256-GCM).
- **Host keys** are always verified: the first connection shows the fingerprint, accepted keys are written to an OpenSSH-compatible `known_hosts`, and changed keys are refused (remove old entries in Settings → Known hosts).
- **Terminal data** travels over a local WebSocket bound to `127.0.0.1` on a random port with a random per-launch token.
- **Session logs and quick servers are off by default**; servers are read-only and cannot serve files outside their folder.
- **Exported session files never contain passwords.**

## Building from source

Requirements: Go 1.26, Node.js 22, Wails CLI v3.1.0; on Linux also `libgtk-4-dev libwebkitgtk-6.0-dev pkg-config`.

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
```

Development mode with hot reload:

```bash
wails3 dev
```

Build for the current platform (output in `bin/`):

```bash
wails3 task build
```

Run the tests:

```bash
go test ./...
```

Releases: push a `v*` tag, or run the "Release" workflow in GitHub Actions with a version number. It builds Windows x64, macOS arm64 and Linux x64 and uploads them with `SHA256SUMS.txt` to a GitHub Release.

## Architecture

```
React + TypeScript + Tailwind (xterm.js, Monaco)
   │  control calls: Wails bindings
   │  terminal data: local WebSocket (binary, output coalesced every 16 ms)
Go core
   ├─ Transport interface: Open / Read / Write / Resize / Close
   │    SSH · Serial · Telnet · local shell (PTY / ConPTY) · TCP / UDP
   ├─ Tabs: reconnect, session logs, MultiExec, Big5 conversion
   ├─ SSH connection pool: terminal, SFTP and sudo-save share one connection
   └─ SFTP · editor · tunnels · TFTP / HTTP / FTP servers · session import
Local storage: sessions.json · settings.json · known_hosts · OS keychain
```

Every protocol implements the same `Transport` interface (`internal/transport`), so tabs, logging, MultiExec and reconnect are written once; adding a protocol means adding one implementation.

## License

[MIT](LICENSE). Bundled third-party licenses are listed in [THIRD_PARTY_NOTICES.txt](THIRD_PARTY_NOTICES.txt) (regenerate with `python3 build/notices/gen_notices.py` after updating dependencies).
