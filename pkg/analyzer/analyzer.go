package analyzer

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Breakdown represents a breakdown of file sizes by extension.
type Breakdown struct {
	Extension string
	Size      int64
}

// FileInfo represents information about a file or directory.
type FileInfo struct {
	Name      string
	Path      string
	Size      int64
	IsDir     bool
	Breakdown []Breakdown
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
	var seen sync.Map

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
				size, breakdown := DirSize(p, progress, &seen)
				mu.Lock()
				result.Files = append(result.Files, FileInfo{
					Name:      n,
					Path:      p,
					Size:      size,
					IsDir:     true,
					Breakdown: breakdown,
				})
				result.TotalSize += size
				mu.Unlock()
			}(path, entry.Name())
		} else {
			size := getPhysicalSize(info, &seen)
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

// DirSize calculates the total size of a directory recursively and its breakdown.
func DirSize(path string, progress chan<- string, seen *sync.Map) (int64, []Breakdown) {
	var size int64
	extensions := make(map[string]int64)

	filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		sendProgress(progress, p)
		if !d.IsDir() {
			info, err := d.Info()
			if err == nil {
				s := getPhysicalSize(info, seen)
				size += s

				ext := filepath.Ext(d.Name())
				if ext == "" {
					ext = "Other"
				} else {
					ext = strings.ToLower(ext)
				}
				extensions[ext] += s
			}
		}
		return nil
	})

	var breakdown []Breakdown
	for ext, s := range extensions {
		breakdown = append(breakdown, Breakdown{Extension: ext, Size: s})
	}

	sort.Slice(breakdown, func(i, j int) bool {
		return breakdown[i].Size > breakdown[j].Size
	})

	return size, breakdown
}

func getPhysicalSize(info os.FileInfo, seen *sync.Map) int64 {
	stats := getFileStats(info)
	if stats.Multi && seen != nil {
		if _, loaded := seen.LoadOrStore(stats.ID, struct{}{}); loaded {
			return 0
		}
	}
	return stats.Size
}

func sendProgress(progress chan<- string, path string) {
	if progress != nil {
		select {
		case progress <- path:
		default:
		}
	}
}
