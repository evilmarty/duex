package analyzer

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// FileInfo represents information about a file or directory.
type FileInfo struct {
	Name  string
	Path  string
	Size  int64
	IsDir bool
}

// Result contains the analysis results for a directory.
type Result struct {
	Files []FileInfo
	TotalSize int64
}

// Analyze scans the given path and returns the size of each entry and the total size.
func Analyze(root string, progress chan<- string) (Result, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return Result{}, err
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var result Result

	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		sendProgress(progress, path)
		info, err := entry.Info()
		if err != nil {
			continue
		}

		if entry.IsDir() {
			wg.Add(1)
			go func(p string, n string) {
				defer wg.Done()
				size := DirSize(p, progress)
				mu.Lock()
				result.Files = append(result.Files, FileInfo{
					Name:  n,
					Path:  p,
					Size:  size,
					IsDir: true,
				})
				result.TotalSize += size
				mu.Unlock()
			}(path, entry.Name())
		} else {
			size := info.Size()
			result.Files = append(result.Files, FileInfo{
				Name:  entry.Name(),
				Path:  path,
				Size:  size,
				IsDir: false,
			})
			result.TotalSize += size
		}
	}

	wg.Wait()
	return result, nil
}

// DirSize calculates the total size of a directory recursively.
func DirSize(path string, progress chan<- string) int64 {
	var size int64
	filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		sendProgress(progress, p)
		if !d.IsDir() {
			info, err := d.Info()
			if err == nil {
				size += info.Size()
			}
		}
		return nil
	})
	return size
}

func sendProgress(progress chan<- string, path string) {
	if progress != nil {
		select {
		case progress <- path:
		default:
		}
	}
}

// Breakdown represents a breakdown of file sizes by extension.
type Breakdown struct {
	Extension string
	Size      int64
}

// GetBreakdown calculates the size breakdown of a directory by file extension.
func GetBreakdown(path string, progress chan<- string) []Breakdown {
	extensions := make(map[string]int64)
	filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		sendProgress(progress, p)
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(d.Name())
		if ext == "" {
			ext = "Other"
		} else {
			ext = strings.ToLower(ext)
		}
		info, err := d.Info()
		if err == nil {
			extensions[ext] += info.Size()
		}
		return nil
	})

	var result []Breakdown
	for ext, size := range extensions {
		result = append(result, Breakdown{Extension: ext, Size: size})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Size > result[j].Size
	})

	return result
}
