package analyzer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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
// It uses a bounded goroutine worker pool: one goroutine is launched per subdirectory,
// but at most runtime.NumCPU() goroutines perform filesystem I/O concurrently via a
// semaphore. This eliminates the sequential bottleneck of filepath.WalkDir while
// preventing resource exhaustion on deep or wide directory trees.
func DirSize(ctx context.Context, path string, progress chan<- string, seen *sync.Map, cache map[string]Result, oneFileSystem bool, rootDev uint64, errorsCount *int64) (int64, []Breakdown, int64) {
	var (
		totalSize  int64
		totalErrs  int64
		extMu      sync.Mutex
		extensions = make(map[string]int64)
		wg         sync.WaitGroup
	)

	// sem is a counting semaphore that limits concurrent filesystem I/O to
	// runtime.NumCPU() goroutines. Goroutines waiting to acquire a slot are
	// parked by the scheduler and consume minimal resources, so spawning one
	// goroutine per subdirectory is safe even for very large trees.
	sem := make(chan struct{}, runtime.NumCPU())

	var processDir func(dirPath string)
	processDir = func(dirPath string) {
		defer wg.Done()

		// Block here until a semaphore slot is available. This is the only
		// point at which a goroutine waits to acquire a resource it does not
		// already hold, so no deadlock is possible.
		sem <- struct{}{}
		defer func() { <-sem }()

		if ctx.Err() != nil {
			return
		}

		// For non-root directories check the cache to avoid redundant traversal.
		if dirPath != path {
			if cached, ok := cache[dirPath]; ok {
				atomic.AddInt64(&totalSize, cached.TotalSize)
				atomic.AddInt64(&totalErrs, cached.ErrorsCount)
				if errorsCount != nil {
					atomic.AddInt64(errorsCount, cached.ErrorsCount)
				}
				extMu.Lock()
				for _, b := range cached.Breakdown {
					extensions[b.Extension] += b.Size
				}
				extMu.Unlock()
				return
			}
		}

		sendProgress(progress, dirPath)

		entries, err := os.ReadDir(dirPath)
		if err != nil {
			atomic.AddInt64(&totalErrs, 1)
			if errorsCount != nil {
				atomic.AddInt64(errorsCount, 1)
			}
			return
		}

		for _, entry := range entries {
			if ctx.Err() != nil {
				return
			}

			entryPath := filepath.Join(dirPath, entry.Name())

			if entry.IsDir() {
				if oneFileSystem {
					info, err := entry.Info()
					if err != nil {
						atomic.AddInt64(&totalErrs, 1)
						if errorsCount != nil {
							atomic.AddInt64(errorsCount, 1)
						}
						continue
					}
					if getFileStats(info).Dev != rootDev {
						continue
					}
				}
				// Launch a goroutine per subdirectory. It will wait for a semaphore
				// slot before doing any I/O, so the parent is never blocked here.
				wg.Add(1)
				go processDir(entryPath)
			} else {
				sendProgress(progress, entryPath)
				info, err := entry.Info()
				if err != nil {
					atomic.AddInt64(&totalErrs, 1)
					if errorsCount != nil {
						atomic.AddInt64(errorsCount, 1)
					}
					continue
				}
				s := getPhysicalSize(info, seen)
				atomic.AddInt64(&totalSize, s)

				ext := filepath.Ext(entry.Name())
				if ext == "" || len(ext) > 15 {
					ext = "Other"
				} else {
					ext = strings.ToLower(ext)
				}
				extMu.Lock()
				extensions[ext] += s
				extMu.Unlock()
			}
		}
	}

	wg.Add(1)
	go processDir(path)
	wg.Wait()

	var breakdown []Breakdown
	for ext, s := range extensions {
		breakdown = append(breakdown, Breakdown{Extension: ext, Size: s})
	}
	sort.Slice(breakdown, func(i, j int) bool {
		return breakdown[i].Size > breakdown[j].Size
	})

	return totalSize, breakdown, totalErrs
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
