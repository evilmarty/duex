# duex - Disk Usage Utility

`duex` is an interactive terminal-based disk usage utility written in Go. It provides a visual breakdown of folders and files based on size and type using the Bubble Tea TUI framework.

## Project Overview
*   **Technologies:** Go, Bubble Tea, Bubbles, Lip Gloss.
*   **Architecture:** Elm Architecture (Model-View-Update).

## Building and Running
*   **Build:** `go build -o duex`
*   **Run:** `./duex [path]` (defaults to `.`)
*   **Test:** `go test ./...`
*   **Coverage:** `go test -cover ./...` (individual packages) or `go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out` (full project breakdown)

## Development Conventions
*   Follow standard Go idiomatic patterns.
*   Use `Lip Gloss` for all styling.
*   Maintain a minimum of 95% code coverage for core packages (e.g., `pkg/analyzer`).
*   Implement concurrent scanning for the analyzer.
