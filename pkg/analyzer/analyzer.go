package analyzer

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Breakdown represents a breakdown of file sizes by extension.
type Breakdown struct {
	Extension string
	Size      int64
}

// FileInfo represents information about a file or directory.
type FileInfo struct {
	Name         string
	Path         string
	Size         int64
	IsDir        bool
	Breakdown    []Breakdown
	IsUnreadable bool  // Set to true if this file/directory could not be read
	ErrorsCount  int64 // Number of unreadable items inside this directory
}

// Result contains the analysis results for a directory.
type Result struct {
	Files       []FileInfo
	TotalSize   int64
	Breakdown   []Breakdown
	ErrorsCount int64
}

// Analyze scans the given path and returns the size of each entry and the total size.
func Analyze(ctx context.Context, root string, progress chan<- string, cache map[string]Result, oneFileSystem bool, errorsCount *int64) (Result, error) {
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
			if errorsCount != nil {
				atomic.AddInt64(errorsCount, 1)
			}
			result.ErrorsCount++
			result.Files = append(result.Files, FileInfo{
				Name:         entry.Name(),
				Path:         path,
				Size:         0,
				IsDir:        entry.IsDir(),
				IsUnreadable: true,
			})
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
					Name:         entry.Name(),
					Path:         path,
					Size:         cached.TotalSize,
					IsDir:        true,
					Breakdown:    cached.Breakdown,
					IsUnreadable: cached.TotalSize == 0 && cached.ErrorsCount > 0,
					ErrorsCount:  cached.ErrorsCount,
				})
				result.TotalSize += cached.TotalSize
				result.ErrorsCount += cached.ErrorsCount
				mu.Unlock()
				continue
			}

			wg.Add(1)
			go func(p string, n string) {
				defer wg.Done()
				size, breakdown, errs := DirSize(ctx, p, progress, &seen, cache, oneFileSystem, rootDev, errorsCount)
				mu.Lock()
				result.Files = append(result.Files, FileInfo{
					Name:         n,
					Path:         p,
					Size:         size,
					IsDir:        true,
					Breakdown:    breakdown,
					IsUnreadable: size == 0 && errs > 0,
					ErrorsCount:  errs,
				})
				result.TotalSize += size
				result.ErrorsCount += errs
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
func DirSize(ctx context.Context, path string, progress chan<- string, seen *sync.Map, cache map[string]Result, oneFileSystem bool, rootDev uint64, errorsCount *int64) (int64, []Breakdown, int64) {
	var size int64
	var errs int64
	extensions := make(map[string]int64)

	filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			errs++
			if errorsCount != nil {
				atomic.AddInt64(errorsCount, 1)
			}
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
					errs++
					if errorsCount != nil {
						atomic.AddInt64(errorsCount, 1)
					}
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
					errs += cached.ErrorsCount
					if errorsCount != nil {
						atomic.AddInt64(errorsCount, cached.ErrorsCount)
					}
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
			} else {
				errs++
				if errorsCount != nil {
					atomic.AddInt64(errorsCount, 1)
				}
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

	return size, breakdown, errs
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
