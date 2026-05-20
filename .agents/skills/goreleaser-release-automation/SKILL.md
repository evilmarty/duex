---
name: goreleaser-release-automation
description: Best practices for managing GoReleaser configurations, executing dry-run tests, injecting version variables via ldflags, and releasing to custom Homebrew taps.
---

# GoReleaser Release Automation Skill

This skill provides directions for testing, maintaining, and automating the release pipeline of a Go command-line tool using GoReleaser.

## Local Configuration Validation

Before running a release pipeline in CI/CD (such as GitHub Actions), always validate the structure and content of `.goreleaser.yaml`.

- Run dry-runs to ensure syntax correctness:
  ```bash
  goreleaser check
  ```
- Run a snapshot build to compile binaries locally and verify that output archives conform to your schema without publishing:
  ```bash
  goreleaser release --snapshot --clean
  ```

## Versioning & Build-time Injection

To ensure accurate version reports from command-line flags (e.g., `duex --version`), inject metadata at build-time using Go linker flags (`-ldflags`).

### Configuration Pattern
In `.goreleaser.yaml`, set up the build block to inject variables into the `main` package:

```yaml
builds:
  - env:
      - CGO_ENABLED=0
    ldflags:
      - "-s -w -X main.Version={{ .Version }} -X main.Commit={{ .Commit }} -X main.BuildDate={{ .Date }}"
```

### Main Go File Declaration
Make sure these variables are defined globally as empty strings inside `main.go`:

```go
var (
	Version   = "development"
	Commit    = "unknown"
	BuildDate = "unknown"
)
```

## Custom Homebrew Tap Distribution

GoReleaser can automatically update a Homebrew Formula in a separate repository (a "tap").

### Formula Structure Conventions
- **Formula Name**: Keep the recipe name matched to your binary.
- **Repository Setup**: Set up standard `repository` mappings:
  ```yaml
  brews:
    - name: duex
      repository:
        owner: evilmarty
        name: homebrew-duex
        branch: main
  ```
- **Local Tap Testing**: When testing locally, you can load a compiled formula using `brew install --build-from-source ./Formula/duex.rb`.
- **Release Verification**: Once GoReleaser completes, verify the release with:
  ```bash
  brew update
  brew install evilmarty/duex/duex
  duex --version
  ```
