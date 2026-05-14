# mihomo-tui

mihomo-tui is a terminal UI configuration tool for [mihomo](https://github.com/MetaCubeX/mihomo) (Clash Meta). Built with [rivo/tview](https://github.com/rivo/tview), it is designed for Linux headless server environments and provides an intuitive keyboard-driven interface for managing proxy configurations.

[中文文档](README.zh.md)

## Features

- **Proxy Management** — Visual node list with latency sorting, multi-select speed testing, and automatic best-node selection
- **Subscription Management** — Add, remove, and update subscriptions with automatic download and config merging
- **Rule Management** — View active rules and support custom rule configuration
- **Connection Monitoring** — Real-time active connections, traffic statistics, and connection details
- **Latency Testing** — Batch node latency tests with visual result sorting
- **System Proxy** — One-click toggle for system proxy (HTTP/SOCKS5)
- **TUN Mode** — Support for TUN mode and routing configuration
- **Log Viewer** — Real-time scrolling logs with filtering and pause support
- **systemd Service** — Built-in service install/uninstall with auto-start support
- **Mouse Support** — Clickable buttons, page switching, and node selection via mouse
- **Dynamic Paging** — Automatically adapts form and proxy list pagination when terminal size changes
- **TUN Docker Compatibility** — Fixes Docker container packet routing under TUN mode so host ports remain accessible from containers

## Screenshots

| Proxy Page | Rules Page |
|:----------:|:----------:|
| ![Proxy Page](docs/proxy.png) | ![Rules Page](docs/rules.png) |

## System Requirements

- **Operating System**: Linux (amd64 / arm64 / armv7 / 386)
- **Runtime Mode**: Root privileges required for service installation; TUI client can run as a regular user

## Quick Install

Use the official one-click install script (recommended):

```bash
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/WangZhongDian/mihomo-tui/main/scripts/install.sh)"
```

Install a specific version:

```bash
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/WangZhongDian/mihomo-tui/main/scripts/install.sh)" -s -- -v v0.1.0
```

After installation, the systemd service will be registered and started automatically.

## Manual Installation

1. Go to the [Releases](https://github.com/WangZhongDian/mihomo-tui/releases) page and download the binary for your architecture
2. Place the executable in your system PATH (e.g., `/usr/local/bin`)
3. Install the systemd service:

```bash
sudo mihomo-tui install_service
```

## Usage

### Start the TUI Client

```bash
mihomo-tui
```

### Command Line Options

```
mihomo-tui — mihomo Terminal UI Configuration Tool

Usage:
  mihomo-tui [options]              Start the TUI client
  mihomo-tui server [options]       Start the background IPC service
  mihomo-tui install_service        Install as a systemd service (requires root)
  mihomo-tui uninstall              Uninstall the systemd service (requires root)
  mihomo-tui version                Show version information

Options:
  -d string    Specify the configuration directory
  -standalone  Start the embedded server (standalone mode)
```

### Common Commands

```bash
# Check service status
sudo systemctl status mihomo-tui

# Stop / Restart service
sudo systemctl stop mihomo-tui
sudo systemctl restart mihomo-tui

# Uninstall service
sudo mihomo-tui uninstall
```

## Interface Navigation

Once the TUI is running, use the following keyboard shortcuts:

| Shortcut | Description |
|----------|-------------|
| `Tab` | Switch focus |
| `↑/↓` | Move up/down |
| `Enter` | Confirm selection |
| `Esc` / `q` | Go back / exit |
| `PgUp` / `PgDn` | Page through forms / lists |
| `Space` | Check / uncheck checkbox |

## Project Structure

```
mihomo-tui/
├── cmd/              # Command line entry points
├── mihomotui/        # Core logic
│   ├── ui/           # TUI interface (tview)
│   ├── config.go     # Configuration management
│   ├── daemon*.go    # IPC service and handlers
│   └── ...
├── scripts/          # Install scripts
├── .github/workflows/# CI/CD
├── main.go           # Main entry point
└── go.mod
```

## Build

Requires Go 1.26+:

```bash
go build -ldflags="-s -w" -o mihomo-tui .
```

Cross-compilation example (ARM64):

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o mihomo-tui-linux-arm64 .
```

## Release Workflow

This project uses GitHub Actions for automated builds and releases:

1. Update the version number locally (`mihomotui/version.go`)
2. Commit and push the code
3. Tag to trigger the build: `git tag v0.x.x && git push origin v0.x.x`
4. GitHub Actions will automatically build multi-architecture binaries and publish them to the Release page

## Supported Architectures

| Architecture | Description |
|--------------|-------------|
| `amd64` | x86_64, mainstream servers / PCs |
| `arm64` | aarch64, ARM servers / Raspberry Pi 4 |
| `armv7` | ARMv7, Raspberry Pi 3/4 (32-bit) |
| `386` | i386, legacy device compatibility |

## License

[MIT](LICENSE)
