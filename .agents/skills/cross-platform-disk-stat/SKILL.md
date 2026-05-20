---
name: cross-platform-disk-stat
description: Best practices and OS-specific patterns for calculating physical disk usage, deduplicating hard links, and handling sparse files across UNIX and Windows systems in Go.
---

# Cross-Platform Disk Stat Skill

This skill provides guidelines and patterns for performing high-performance, accurate cross-platform disk calculations in Go, particularly addressing the differences between UNIX-like and Windows filesystems.

## Sizing Calculations: Logical vs. Physical Size

To accurately assess disk space consumption, you must distinguish between logical size and physical size.

1. **Logical Size**: What the application or file system lists as the length of the file's data.
2. **Physical Size**: The actual amount of disk space occupied by the blocks representing the file.
   - **Sparse Files**: May have a very large logical size but consume zero or few physical blocks.
   - **APFS Clones / Reflinks**: May share physical blocks, resulting in smaller actual usage.

### UNIX Implementation
On Unix-like platforms (macOS, Linux, BSD), query the underlying `syscall.Stat_t` structure.

```go
// +build !windows

package analyzer

import (
	"os"
	"syscall"
)

func getPhysicalSize(info os.FileInfo) int64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		// stat.Blocks is the number of 512-byte blocks allocated
		return stat.Blocks * 512
	}
	return info.Size()
}
```

### Windows Implementation
Windows does not use block-based allocations in the same way. You must fall back to the logical size or query specific NTFS attributes:

```go
// +build windows

package analyzer

import "os"

func getPhysicalSize(info os.FileInfo) int64 {
	// Fall back to logical size or use GetCompressedFileSize API
	return info.Size()
}
```

## Inode Tracking & Hard Link Deduplication

Hard links allow multiple directory entries to point to the same physical data. To prevent double-counting, track files by their unique system identifier.

### Performance Optimization: Check `Nlink`
To avoid performance overhead from maintaining massive tracking maps, only query and store inode keys if a file's link count is greater than 1.

```go
type InodeKey struct {
	Dev uint64 // Device ID
	Ino uint64 // Inode number
}

// In your analyzer loop:
if stat, ok := info.Sys().(*syscall.Stat_t); ok {
	if stat.Nlink > 1 {
		key := InodeKey{Dev: uint64(stat.Dev), Ino: uint64(stat.Ino)}
		if seenInodes[key] {
			// Already counted, skip physical size calculation
			return 0
		}
		seenInodes[key] = true
	}
}
```

## Testing Cross-Platform Logic

When writing unit tests for analyzer packages:
- Use build tags (`_unix_test.go`, `_windows_test.go`) or platform runtime checks to execute specific tests.
- For hard link tests, use `os.Link()` inside a temporary directory to verify that your deduplication registry functions as expected.
- For sparse files, write small tests that check block size on supported filesystems (e.g., ext4, APFS).
