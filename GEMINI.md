# dude - Disk Usage Utility

`dude` is an interactive terminal-based disk usage utility written in Go. It provides a visual breakdown of folders and files based on size and type using the Bubble Tea TUI framework.

## Project Overview
*   **Technologies:** Go, Bubble Tea, Bubbles, Lip Gloss.
*   **Architecture:** Elm Architecture (Model-View-Update).

## Building and Running
*   **Build:** `go build -o dude`
*   **Run:** `./dude [path]` (defaults to `.`)
*   **Test:** `go test ./...`

## Development Conventions
*   Follow standard Go idiomatic patterns.
*   Use `Lip Gloss` for all styling.
*   Implement concurrent scanning for the analyzer.
