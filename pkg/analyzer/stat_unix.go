//go:build !windows

package analyzer

import (
	"os"
	"syscall"
)

// fileStats contains physical size and a unique ID for inode tracking.
type fileStats struct {
	Size  int64
	ID    uint64
	Multi bool // True if the file has multiple hard links
}

// getFileStats returns the physical size and unique ID of a file.
func getFileStats(info os.FileInfo) fileStats {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileStats{Size: info.Size()}
	}

	// Physical size is blocks * 512 bytes on most Unix systems.
	// This correctly handles sparse files and clones on APFS.
	size := stat.Blocks * 512

	return fileStats{
		Size:  size,
		ID:    stat.Ino ^ (uint64(stat.Dev) << 32),
		Multi: stat.Nlink > 1,
	}
}
