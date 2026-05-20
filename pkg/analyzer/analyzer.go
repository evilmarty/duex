package analyzer

import (
	"context"
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
	Files     []FileInfo
	TotalSize int64
	Breakdown []Breakdown
}

// Analyze scans the given path and returns the size of each entry and the total size.
func Analyze(ctx context.Context, root string, progress chan<- string, cache map[string]Result, oneFileSystem bool) (Result, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return Result{}, err
	}

	var rootDev uint64
	if oneFileSystem {
		rootInfo, err := os.Lstat(root)
		if err == nil {
			rootDev = getFileStats(rootInfo).Dev
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var result Result
	var seen sync.Map

	for _, entry := range entries {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}

		path := filepath.Join(root, entry.Name())
		sendProgress(progress, path)
		info, err := entry.Info()
		if err != nil {
			continue
		}

		entryStats := getFileStats(info)

		if entry.IsDir() {
			if oneFileSystem && entryStats.Dev != rootDev {
				continue
			}

			if cached, ok := cache[path]; ok {
				mu.Lock()
				result.Files = append(result.Files, FileInfo{
					Name:      entry.Name(),
					Path:      path,
					Size:      cached.TotalSize,
					IsDir:     true,
					Breakdown: cached.Breakdown,
				})
				result.TotalSize += cached.TotalSize
				mu.Unlock()
				continue
			}

			wg.Add(1)
			go func(p string, n string) {
				defer wg.Done()
				size, breakdown := DirSize(ctx, p, progress, &seen, cache, oneFileSystem, rootDev)
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
			ext := filepath.Ext(entry.Name())
			if ext == "" || len(ext) > 15 {
				ext = "Other"
			} else {
				ext = strings.ToLower(ext)
			}
			result.Files = append(result.Files, FileInfo{
				Name:  entry.Name(),
				Path:  path,
				Size:  size,
				IsDir: false,
				Breakdown: []Breakdown{
					{Extension: ext, Size: size},
				},
			})
			result.TotalSize += size
		}
	}

	wg.Wait()

	// Aggregate root breakdown
	extMap := make(map[string]int64)
	for _, f := range result.Files {
		for _, b := range f.Breakdown {
			extMap[b.Extension] += b.Size
		}
	}
	for ext, s := range extMap {
		result.Breakdown = append(result.Breakdown, Breakdown{Extension: ext, Size: s})
	}
	sort.Slice(result.Breakdown, func(i, j int) bool {
		return result.Breakdown[i].Size > result.Breakdown[j].Size
	})

	return result, nil
}

// DirSize calculates the total size of a directory recursively and its breakdown.
func DirSize(ctx context.Context, path string, progress chan<- string, seen *sync.Map, cache map[string]Result, oneFileSystem bool, rootDev uint64) (int64, []Breakdown) {
	var size int64
	extensions := make(map[string]int64)

	filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		sendProgress(progress, p)

		if d.IsDir() {
			if oneFileSystem && p != path {
				info, err := d.Info()
				if err != nil {
					return filepath.SkipDir
				}
				dirStats := getFileStats(info)
				if dirStats.Dev != rootDev {
					return filepath.SkipDir
				}
			}

			if p != path {
				if cached, ok := cache[p]; ok {
					size += cached.TotalSize
					for _, b := range cached.Breakdown {
						extensions[b.Extension] += b.Size
					}
					return filepath.SkipDir
				}
			}
		}

		if !d.IsDir() {
			info, err := d.Info()
			if err == nil {
				s := getPhysicalSize(info, seen)
				size += s

				ext := filepath.Ext(d.Name())
				if ext == "" || len(ext) > 15 {
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
