# Contributing to duex

First and foremost, thank you! We appreciate that you want to contribute to duex, your time is valuable, and your contributions mean a lot to us.

## Important

By contributing to this project, you:

* Agree that you have authored 100% of the content
* Agree that you have the necessary rights to the content
* Agree that you have received the necessary permissions from your employer to make the contributions (if applicable)
* Agree that the content you contribute may be provided under the Project license(s)
* Agree that, if you did not author 100% of the content, the appropriate licenses and copyrights have been added along with any other necessary attribution.

## Getting started

**What does "contributing" mean?**

Creating an issue is the simplest form of contributing to a project. But there are many ways to contribute, including the following:

* Updating or correcting documentation
* Feature requests
* Bug reports

**Showing support for duex**

Please keep in mind that open source software is built by people like you, who spend their free time creating things the rest of the community can use.

Don't have time to contribute? No worries, here are some other ways to show your support for duex:

* Star the [project](https://github.com/evilmarty/duex)
* Share your support for duex with others

## Issues

Please only create issues for bug reports or feature requests. Issues discussing any other topics may be closed by the project's maintainers without further explanation.

Do not create issues about bumping dependencies unless a bug has been identified and you can demonstrate that it affects this utility.

**Help us to help you**

Remember that we’re here to help, but not to make guesses about what you need help with:

* Whatever bug or issue you're experiencing, assume that it will not be as obvious to the maintainers as it is to you.
* Spell it out completely. Explain how you're using the utility and what filesystem context triggered the issue so that maintainers can diagnose it.

_It can't be understated how frustrating and draining it can be to maintainers to have to ask clarifying questions on the most basic things, before it's even possible to start debugging. Please try to make the best use of everyone's time involved, including yourself, by providing this information up front._

### Before creating an issue

Search for existing issues (open or closed) that address the issue and might have even resolved it already.

### Creating an issue

Please be as descriptive as possible when creating an issue. Give us the information we need to successfully answer your question or address your issue by answering the following in your issue:

* **description**: (required) What is the bug/behavior you're experiencing?
* **OS**: (required) what operating system are you on?
* **version**: (required) please note the version of duex are you using (e.g. output of `./duex --version`)
* **error messages**: (required) please paste any error messages or panic logs
* **filesystem environment**: (if applicable) what filesystem type are you scanning (e.g. APFS, ext4, NTFS) and are there sparse files, nested directories, symlinks, or hard links?

### Closing issues

The original poster or the maintainers of duex may close an issue at any time. Typically, but not exclusively, issues are closed when:

* The issue is resolved
* The project's maintainers have determined the issue is out of scope
* An issue is clearly a duplicate of another issue
* A discussion has clearly run its course

## Development Workflow

### Building and Running
* **Build**: `go build -o duex`
* **Run**: `./duex [path]` (defaults to `.`)
* **Test**: `go test ./...`
* **Coverage**: `go test -cover ./...` (individual packages) or `go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out` (full project breakdown)

### Build-time Versioning
We use Go's `-ldflags` to set the version string at build-time. This allows the `--version` flag to display accurate release information:
```bash
go build -ldflags="-X main.Version=v1.0.0" -o duex
```

## Architecture & Package Design

The project is structured with a clear separation of concerns to maintain testability, performance, and cross-platform compatibility:

1. **`pkg/analyzer`**: The core directory scanner. It traverses the directory tree concurrently to calculate file sizes and accumulate breakdown stats.
   * **Physical Size**: Always use physical block-based size (`stat.Blocks * 512` on UNIX) instead of logical size (`info.Size()`) to correctly handle sparse files and APFS clones/reflinks.
   * **Inode Tracking**: Use OS-specific stat identifiers (Device ID + Inode number) to deduplicate hard links. To maintain performance, only perform inode lookups when `Nlink > 1`.
   * **Thread Safety**: When using concurrent workers, all shared data mutations (including those in the main thread/goroutine) must be strictly synchronized under a mutex or with atomic variables to prevent data races.
2. **Root Package (`main.go`)**: The TUI application runner built on the Elm Architecture (Model-View-Update) using the Charm TUI ecosystem.
   * **Update Loop Mutation**: Perform all model state mutations (especially dimension updates like `SetSize` or `SetHeight`) within the `Update` loop. Modifications inside `View` are lost because the function receives a copy of the model.
   * **Robust Rendering**: Construct a unified output string in `View` starting with the header. Use `lipgloss.JoinVertical` or direct concatenation with fixed margins/newlines to prevent UI jitter during rapid asynchronous updates (e.g., spinner ticks).
   * **Emoji Layout & Width Alignment**: Emojis (e.g. `⚠️ `) occupy a specific number of visual cells in the terminal (typically 3 visual columns including a trailing space). Explicitly account for this visual cell width when calculating dynamic column margins to prevent layout alignment shift or jitter.
   * **Custom List Delegates**: Prefer `bubbles/list` for scrollable interfaces. Implement a custom `list.ItemDelegate` to maintain custom aesthetics while retaining built-in list features like filtering and pagination.
   * **Help Component**: Manage keyboard shortcuts using `bubbles/help` and `bubbles/key` for dynamic, context-aware help footers that adapt to the application state (e.g., loading vs. browsing).

## Testing Conventions

We maintain a strict testing culture to prevent regressions:

* **Coverage**: Core packages (e.g., `pkg/analyzer`) must maintain a minimum of **95%** code coverage.
* **Race Detector**: All concurrent code must run cleanly with the Go race detector. Run tests using:
  ```bash
  go test -race ./...
  ```
* **Deterministic Filesystem Mocking**: Avoid using OS-level permissions (e.g., `chmod 000`) for testing restricted files/folders. These are fragile and fail in root environments. Instead, decouple filesystem operations via injectable function variables (e.g., `var listDir = os.ReadDir`) to allow deterministic unit tests of error paths.
* **TUI Regression Testing**:
  * **Keyboard Interaction**: Test keyboard events programmatically by feeding `tea.KeyMsg` directly into your model's `Update` function and asserting the returned model state.
  * **View Output Assertions**: Verify the rendering of TUI layouts by calling the model's `View()` function and asserting substring containment on the returned layout. You can strip ANSI escape sequences using a regular expression helper to test raw text content cleanly.

[so]: http://stackoverflow.com/questions/tagged/duex
