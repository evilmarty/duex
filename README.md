# duex - Disk Usage Explorer

`duex` is a fast, interactive terminal-based disk usage utility written in Go. It helps you understand what is consuming space on your drives by providing a navigable, visual breakdown of folders and files.

## Features

*   **Interactive TUI**: Built with the Charm ecosystem (Bubble Tea, Bubbles, Lip Gloss) for a modern, responsive terminal experience.
*   **Accurate Sizing**: Calculates actual physical disk usage, properly handling hard links (no double-counting) and sparse files.
*   **Fast & Concurrent**: Uses Go routines for rapid, non-blocking directory traversal.
*   **Real-time Progress**: Visual feedback with an animated spinner and scrolling file list during heavy scans.
*   **Instant Breakdowns**: Automatically computes and displays a file extension breakdown for directories.
*   **Filter & Search**: Quickly find specific files in large directories.
*   **Safe Navigation**: Cancel long-running scans instantly with `esc`.

## Building and Installation

Make sure you have [Go](https://golang.org/) installed.

Clone the repository and build the executable:

```bash
git clone https://github.com/evilmarty/duex
cd duex
go build -ldflags="-X main.Version=v1.0.0" -o duex
```

You can then move the `duex` binary to a location in your `$PATH`.

## Usage

Run `duex` in your current directory:
```bash
./duex
```

Or provide a specific path to scan:
```bash
./duex /path/to/scan
```

### CLI Flags

| Flag | Description |
| :--- | :--- |
| `-h`, `--help` | Show usage instructions |
| `-v`, `--version` | Show application version |

### Keyboard Shortcuts

| Key | Action |
| :--- | :--- |
| `↑` / `k` | Move cursor up |
| `↓` / `j` | Move cursor down |
| `enter` | Open selected directory |
| `backspace` | Go up to parent directory |
| `esc` | Cancel active scan / Go back |
| `r` | Refresh (rescan current directory) |
| `/` | Filter files in current directory |
| `q` / `ctrl+c`| Quit application |

## Testing and Coverage

The project maintains a high standard of test coverage (95%+ for core packages).

To run the test suite:
```bash
go test ./...
```

To generate and view the detailed code coverage profile:
```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```
