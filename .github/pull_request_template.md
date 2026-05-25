## Description

Please describe the purpose of this pull request, the problem it solves, and how the changes were implemented.

## Checklist

Please verify that your contributions follow the development and architectural guidelines of `duex`:

### General
- [ ] All tests pass cleanly (`go test ./...`)
- [ ] Core packages (especially `pkg/analyzer`) maintain a minimum of **95%** code coverage (`go test -cover ./...`)
- [ ] No race conditions detected under the race detector (`go test -race ./...`)
- [ ] Updated `README.md` or other documentation, if necessary

### Architectural Conventions
- [ ] **Accurate Disk Usage Calculations**:
  - [ ] Uses physical block-based size (`stat.Blocks * 512`) instead of logical size (`info.Size()`) to handle sparse files and APFS clones/reflinks correctly.
  - [ ] Inodes are tracked and deduplicated for hard links when `Nlink > 1` (Device ID + Inode number).
- [ ] **Scanners & Concurrency Thread Safety**:
  - [ ] All shared data mutations across concurrent workers and the main thread are strictly synchronized via mutexes or atomic variables.
- [ ] **Deterministic Filesystem Mocking**:
  - [ ] Avoids using OS-level permissions (e.g., `chmod 000`) for testing restricted paths. Instead, uses decoupled injectable filesystem function variables.
- [ ] **Bubble Tea (TUI) Architecture & Styling**:
  - [ ] All model state mutations (especially dimensions) are performed within the `Update` loop, NOT the `View` function.
  - [ ] Output is constructed robustly starting with the header using `lipgloss.JoinVertical` or direct layouts to avoid UI jitter.
  - [ ] Emojis are accounted for as having a width of 3 visual columns (including trailing space) when calculating dynamic margins.
  - [ ] Standard keyboard shortcuts and help footers are managed dynamically via the help component.

## Verification / Demo (if applicable)

Please describe or paste screenshots/recordings of manual TUI behavior verification, or paste command output showing test results.

Thanks for contributing!
