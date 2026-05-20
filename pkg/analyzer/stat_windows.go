//go:build windows

package analyzer

import (
	"os"
)

// fileStats contains physical size and a unique ID for inode tracking.
type fileStats struct {
	Size  int64
	ID    uint64
	Multi bool
	Dev   uint64
}

// getFileStats returns the logical size for Windows as a fallback.
var getFileStats = func(info os.FileInfo) fileStats {
	return fileStats{
		Size:  info.Size(),
		Multi: false,
		Dev:   0,
	}
}
