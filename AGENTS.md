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
*   **Thread Safety in Scanners**: When using concurrent workers, all shared data mutations (including those in the main thread/goroutine) must be strictly synchronized under a mutex or with atomic variables to prevent data races.
*   **Deterministic Filesystem Mocking**: Avoid using OS-level permissions (e.g. `chmod 000`) for testing restricted files/folders. These are fragile and fail in root environments. Decouple filesystem operations via injectable function variables (e.g., `var listDir = os.ReadDir`) to allow deterministic unit tests of error paths.


## Architectural Conventions

### Accurate Disk Usage Calculations
*   **Physical Size:** Always use physical block-based size (`stat.Blocks * 512`) instead of logical size (`info.Size()`) to correctly handle sparse files and APFS clones.
*   **Inode Tracking:** Use OS-specific stat identifiers (Device ID + Inode number) to deduplicate hard links. To maintain performance, only perform inode lookups when `Nlink > 1`.

### Bubble Tea Architecture & Layout
*   **Update Loop Mutation:** Perform all model state mutations (especially dimension updates like `SetSize` or `SetHeight`) within the `Update` loop. Modifications inside `View` are lost because the function receives a copy of the model.
*   **Robust Rendering:** Construct a unified output string in `View` starting with the header. Use `lipgloss.JoinVertical` or direct concatenation with fixed margins/newlines to prevent UI jitter during rapid asynchronous updates (e.g., spinner ticks).
*   **Emoji Layout & Width Alignment:** When rendering emojis (e.g., `⚠️ `) in structured columns (such as size indicators), explicitly account for the visual cell width of the emoji and its trailing space (typically 3 visual columns) when calculating dynamic column margins to prevent layout alignment shift or jitter.


### Charm Ecosystem Idioms
*   **Custom List Delegates:** Prefer `bubbles/list` for scrollable interfaces. Implement a custom `list.ItemDelegate` to maintain custom aesthetics while retaining built-in list features like filtering and pagination.
*   **Help Component:** Manage keyboard shortcuts using `bubbles/help` and `bubbles/key` for dynamic, context-aware help footers that adapt to the application state (e.g., loading vs. browsing).

## Build and Versioning
*   **Build-time Versioning:** Use Go's `-ldflags` to set the version string at build-time. This allows the `--version` flag to display accurate release information.
    ```bash
    go build -ldflags="-X main.Version=v1.0.0" -o duex
    ```
