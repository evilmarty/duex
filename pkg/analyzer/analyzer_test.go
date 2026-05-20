package analyzer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)


func TestAnalyze(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "duex-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create some files
	file1 := filepath.Join(tmpDir, "file1.txt")
	if err := os.WriteFile(file1, []byte("hello"), 0644); err != nil {
		t.Fatalf("Failed to create file1: %v", err)
	}

	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	file2 := filepath.Join(subDir, "file2.txt")
	if err := os.WriteFile(file2, []byte("world!"), 0644); err != nil {
		t.Fatalf("Failed to create file2: %v", err)
	}

	result, err := Analyze(context.Background(), tmpDir, nil, nil, false, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if len(result.Files) != 2 {
		t.Errorf("Expected 2 files, got %d", len(result.Files))
	}

	// Calculate expected physical size
	var expectedTotal int64
	for _, path := range []string{file1, file2} {
		info, _ := os.Stat(path)
		expectedTotal += getFileStats(info).Size
	}

	if result.TotalSize != expectedTotal {
		t.Errorf("Expected total size %d, got %d", expectedTotal, result.TotalSize)
	}

	// Verify breakdown for subdir
	foundSubdir := false
	for _, f := range result.Files {
		if f.Name == "subdir" {
			foundSubdir = true
			if len(f.Breakdown) == 0 {
				t.Error("Expected breakdown for subdir, got empty")
			}
			foundTxt := false
			for _, b := range f.Breakdown {
				if b.Extension == ".txt" {
					foundTxt = true
					info, _ := os.Stat(file2)
					expected := getFileStats(info).Size
					if b.Size != expected {
						t.Errorf("Expected .txt size %d for subdir, got %d", expected, b.Size)
					}
				}
			}
			if !foundTxt {
				t.Error("Expected .txt in subdir breakdown")
			}
		}
	}
	if !foundSubdir {
		t.Error("Expected to find 'subdir' in results")
	}

	// Verify aggregate breakdown
	if len(result.Breakdown) == 0 {
		t.Error("Expected root aggregate breakdown, got empty")
	}
	foundTxtAtRoot := false
	for _, b := range result.Breakdown {
		if b.Extension == ".txt" {
			foundTxtAtRoot = true
			if b.Size != expectedTotal {
				t.Errorf("Expected root breakdown .txt size %d, got %d", expectedTotal, b.Size)
			}
		}
	}
	if !foundTxtAtRoot {
		t.Error("Expected .txt in root aggregate breakdown")
	}
}

func TestAnalyzeHardLinks(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "duex-hardlink-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a file
	file1 := filepath.Join(tmpDir, "file1.txt")
	content := []byte("shared content")
	if err := os.WriteFile(file1, content, 0644); err != nil {
		t.Fatalf("Failed to create file1: %v", err)
	}

	// Create a hard link
	file2 := filepath.Join(tmpDir, "file1_link.txt")
	if err := os.Link(file1, file2); err != nil {
		t.Skip("Hard links not supported on this filesystem")
	}

	result, err := Analyze(context.Background(), tmpDir, nil, nil, false, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// It should find 2 entries
	if len(result.Files) != 2 {
		t.Errorf("Expected 2 files, got %d", len(result.Files))
	}

	// But the total size should only be the physical size of ONE file
	info, _ := os.Stat(file1)
	expectedSize := getFileStats(info).Size

	if result.TotalSize != expectedSize {
		t.Errorf("Expected total size %d (counted once), got %d", expectedSize, result.TotalSize)
	}
}

func TestAnalyzeLongExtension(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "duex-longext-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a file with an excessively long extension
	longExt := ".a0~__CX9j0QIqNd7exyZ5zPlE5EeM6jzt86awZCKR-eN68wV7qfj5P60gacfUh7oVojv9yXCYXkP7JcIuyx3AdRXg=="
	fileName := "data" + longExt
	filePath := filepath.Join(tmpDir, fileName)
	content := []byte("some content")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("Failed to create file with long extension: %v", err)
	}

	result, err := Analyze(context.Background(), tmpDir, nil, nil, false, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// Verify breakdown grouping
	// The file should be grouped under "Other"
	foundOther := false

	// Revised approach: check breakdown of a subdirectory containing the long extension file
	subDir := filepath.Join(tmpDir, "sub")
	os.Mkdir(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "file"+longExt), content, 0644)

	result, _ = Analyze(context.Background(), tmpDir, nil, nil, false, nil)
	for _, f := range result.Files {
		if f.Name == "sub" {
			for _, b := range f.Breakdown {
				if b.Extension == "Other" {
					foundOther = true
				}
				if b.Extension == longExt {
					t.Errorf("Should NOT find long extension %s in breakdown", longExt)
				}
			}
		}
	}

	if !foundOther {
		t.Error("Expected to find 'Other' in breakdown for file with long extension")
	}
}

func TestAnalyzeCancellation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "duex-cancel-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("data"), 0644)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = Analyze(ctx, tmpDir, nil, nil, false, nil)
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}
}

func TestAnalyzeProgress(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "duex-progress-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("data"), 0644)

	progress := make(chan string, 10)
	_, err = Analyze(context.Background(), tmpDir, progress, nil, false, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	select {
	case <-progress:
		// Success
	default:
		t.Error("Expected progress update, but channel was empty")
	}
}

func TestAnalyzeErrors(t *testing.T) {
	// Create non-existent directory
	_, err := Analyze(context.Background(), "/non/existent/path", nil, nil, false, nil)
	if err == nil {
		t.Error("Expected error for non-existent directory, got nil")
	}
}

type mockFileInfo struct {
	os.FileInfo
}

func (m mockFileInfo) Sys() interface{} { return nil }
func (m mockFileInfo) Size() int64      { return 0 }

func TestGetFileStatsFallback(t *testing.T) {
	// Test the case where Sys() is nil
	info := mockFileInfo{}
	stats := getFileStats(info)
	if stats.Multi != false {
		t.Error("Expected Multi to be false for nil Sys()")
	}
}

func TestAnalyzeWithCache(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "duex-cache-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	subDir := filepath.Join(tmpDir, "cached-subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	// Put subDir in cache directly
	cache := map[string]Result{
		subDir: {
			TotalSize: 50000,
			Breakdown: []Breakdown{
				{Extension: ".dat", Size: 50000},
			},
		},
	}

	// This should hit the cache instead of scanning subDir
	result, err := Analyze(context.Background(), tmpDir, nil, cache, false, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if result.TotalSize != 50000 {
		t.Errorf("Expected total size 50000 from cache, got %d", result.TotalSize)
	}
}

func TestDirSizeWithCache(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "duex-dirsize-cache-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create subDirs
	parentDir := filepath.Join(tmpDir, "parent")
	childDir := filepath.Join(parentDir, "child")
	if err := os.MkdirAll(childDir, 0755); err != nil {
		t.Fatalf("Failed to create dirs: %v", err)
	}

	// Put childDir in cache directly
	cache := map[string]Result{
		childDir: {
			TotalSize: 12345,
			Breakdown: []Breakdown{
				{Extension: ".log", Size: 12345},
			},
		},
	}

	// Walk parentDir. It should encounter childDir and use cache.
	size, breakdown, _ := DirSize(context.Background(), parentDir, nil, nil, cache, false, 0, nil)
	if size != 12345 {
		t.Errorf("Expected size 12345 from cache, got %d", size)
	}
	if len(breakdown) != 1 || breakdown[0].Extension != ".log" || breakdown[0].Size != 12345 {
		t.Errorf("Expected log breakdown of size 12345, got %v", breakdown)
	}
}

func TestDirSizeCancellation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "duex-dirsize-cancel")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	size, _, _ := DirSize(ctx, tmpDir, nil, nil, nil, false, 0, nil)
	if size != 0 {
		t.Errorf("Expected 0 size on canceled context, got %d", size)
	}
}

func TestAnalyzeOneFileSystem(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "duex-one-fs-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	subDir := filepath.Join(tmpDir, "sub")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	file1 := filepath.Join(subDir, "file1.txt")
	if err := os.WriteFile(file1, []byte("data"), 0644); err != nil {
		t.Fatalf("Failed to create file1: %v", err)
	}

	result, err := Analyze(context.Background(), tmpDir, nil, nil, true, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	foundSub := false
	for _, f := range result.Files {
		if f.Name == "sub" {
			foundSub = true
			if f.Size <= 0 {
				t.Errorf("Expected positive physical size for subdir, got %d", f.Size)
			}
		}
	}
	if !foundSub {
		t.Error("Expected to find and traverse 'sub'")
	}
}

func TestAnalyzeOneFileSystemBoundary(t *testing.T) {
	origGetFileStats := getFileStats
	defer func() { getFileStats = origGetFileStats }()

	tmpDir, err := os.MkdirTemp("", "duex-one-fs-boundary")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	subDir := filepath.Join(tmpDir, "sub")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	file1 := filepath.Join(subDir, "file1.txt")
	if err := os.WriteFile(file1, []byte("data"), 0644); err != nil {
		t.Fatalf("Failed to create file1: %v", err)
	}

	getFileStats = func(info os.FileInfo) fileStats {
		stats := origGetFileStats(info)
		if info.Name() == filepath.Base(tmpDir) {
			stats.Dev = 1
		} else {
			stats.Dev = 2
		}
		return stats
	}

	result, err := Analyze(context.Background(), tmpDir, nil, nil, true, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	for _, f := range result.Files {
		if f.Name == "sub" {
			t.Error("Expected 'sub' to be skipped due to different device ID")
		}
	}
}

func TestDirSizeOneFileSystemBoundary(t *testing.T) {
	origGetFileStats := getFileStats
	defer func() { getFileStats = origGetFileStats }()

	tmpDir, err := os.MkdirTemp("", "duex-dirsize-boundary")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	subDir := filepath.Join(tmpDir, "sub")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	grandchildDir := filepath.Join(subDir, "grandchild")
	if err := os.Mkdir(grandchildDir, 0755); err != nil {
		t.Fatalf("Failed to create grandchild: %v", err)
	}

	file1 := filepath.Join(grandchildDir, "file1.txt")
	if err := os.WriteFile(file1, []byte("data"), 0644); err != nil {
		t.Fatalf("Failed to create file1: %v", err)
	}

	getFileStats = func(info os.FileInfo) fileStats {
		stats := origGetFileStats(info)
		if info.Name() == filepath.Base(tmpDir) || info.Name() == "sub" {
			stats.Dev = 1
		} else {
			stats.Dev = 2
		}
		return stats
	}

	result, err := Analyze(context.Background(), tmpDir, nil, nil, true, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	foundSub := false
	for _, f := range result.Files {
		if f.Name == "sub" {
			foundSub = true
			if f.Size != 0 {
				t.Errorf("Expected 'sub' size to be 0 (grandchild skipped), got %d", f.Size)
			}
		}
	}
	if !foundSub {
		t.Error("Expected to find 'sub'")
	}
}

func TestAnalyzeWithErrorsCount(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "duex-err-count")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	subDir := filepath.Join(tmpDir, "noperm")
	if err := os.Mkdir(subDir, 0000); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}
	// Restore permissions on defer so cleanup doesn't fail
	defer os.Chmod(subDir, 0755)

	res, err := Analyze(context.Background(), tmpDir, nil, nil, false, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if res.ErrorsCount < 1 {
		t.Errorf("Expected at least 1 error due to permission denied directory, got %d", res.ErrorsCount)
	}

	foundNoPerm := false
	for _, f := range res.Files {
		if f.Name == "noperm" {
			foundNoPerm = true
			if !f.IsUnreadable {
				t.Errorf("Expected 'noperm' subdirectory to be marked as IsUnreadable")
			}
			if f.Size != 0 {
				t.Errorf("Expected unreadable directory size to be 0, got %d", f.Size)
			}
		}
	}
	if !foundNoPerm {
		t.Errorf("Expected 'noperm' subdirectory to be present in Files list")
	}
}

func TestAnalyzeWithErrorsCountRealTime(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "duex-err-count-rt")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	subDir := filepath.Join(tmpDir, "noperm")
	if err := os.Mkdir(subDir, 0000); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}
	defer os.Chmod(subDir, 0755)

	var errorsCount int64
	_, err = Analyze(context.Background(), tmpDir, nil, nil, false, &errorsCount)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if errorsCount < 1 {
		t.Errorf("Expected errorsCount pointer to be at least 1, got %d", errorsCount)
	}
}

func TestAnalyzeAdvancedEdgeCases(t *testing.T) {
	// 1. entry.Info() failure in Analyze
	tmpDir1, err := os.MkdirTemp("", "duex-edge1")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		os.Chmod(tmpDir1, 0755)
		os.RemoveAll(tmpDir1)
	}()

	// Create 50 files
	for i := 0; i < 50; i++ {
		filePath := filepath.Join(tmpDir1, fmt.Sprintf("file%d.txt", i))
		os.WriteFile(filePath, []byte("data"), 0644)
	}
	// Add a second file with a different extension so sort.Slice is covered
	filePathLog := filepath.Join(tmpDir1, "extra.log")
	os.WriteFile(filePathLog, []byte("logdata"), 0644)

	progress1 := make(chan string, 100)
	var errorsCount1 int64

	type analyzeResult struct {
		res Result
		err error
	}
	ch1 := make(chan analyzeResult, 1)

	go func() {
		res, err := Analyze(context.Background(), tmpDir1, progress1, nil, false, &errorsCount1)
		close(progress1)
		ch1 <- analyzeResult{res, err}
	}()

	first := true
	for p := range progress1 {
		t.Logf("[Test 1] Received progress path: %s", p)
		if first {
			err := os.Chmod(tmpDir1, 0000)
			t.Logf("[Test 1] os.Chmod(0000) error: %v", err)
			first = false
		}
	}
	res1Wrap := <-ch1
	
	// Restore permissions so cleanup defer works
	os.Chmod(tmpDir1, 0755)

	if res1Wrap.err != nil {
		t.Fatalf("Analyze failed: %v", res1Wrap.err)
	}

	if errorsCount1 < 1 {
		t.Errorf("Expected at least 1 error count from unreadable directory files, got %d", errorsCount1)
	}
	if res1Wrap.res.ErrorsCount < 1 {
		t.Errorf("Expected res1.ErrorsCount to be at least 1, got %d", res1Wrap.res.ErrorsCount)
	}

	// 2. d.Info() failure for subdirectory inside DirSize under oneFileSystem=true
	tmpDir2, err := os.MkdirTemp("", "duex-edge2")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		os.Chmod(tmpDir2, 0755)
		os.RemoveAll(tmpDir2)
	}()

	subDir := filepath.Join(tmpDir2, "sub")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	// Create 50 nested subdirectories
	for i := 0; i < 50; i++ {
		dirPath := filepath.Join(subDir, fmt.Sprintf("child%d", i))
		os.Mkdir(dirPath, 0755)
	}

	progress2 := make(chan string, 100)
	var errorsCount2 int64

	type dirSizeResult struct {
		size      int64
		breakdown []Breakdown
		errs      int64
	}
	ch2 := make(chan dirSizeResult, 1)

	go func() {
		size, breakdown, errs := DirSize(context.Background(), subDir, progress2, nil, nil, true, 0, &errorsCount2)
		close(progress2)
		ch2 <- dirSizeResult{size, breakdown, errs}
	}()

	first2 := true
	for p := range progress2 {
		t.Logf("[Test 2] Received progress path: %s", p)
		if first2 {
			err := os.Chmod(subDir, 0000)
			t.Logf("[Test 2] os.Chmod(0000) error: %v", err)
			first2 = false
		}
	}
	res2 := <-ch2
	os.Chmod(subDir, 0755)

	if errorsCount2 < 1 {
		t.Errorf("Expected at least 1 error count for unreadable subdirectory in DirSize, got %d", errorsCount2)
	}
	if res2.errs < 1 {
		t.Errorf("Expected errs2 to be at least 1, got %d", res2.errs)
	}

	// 3. d.Info() failure for file inside DirSize
	tmpDir3, err := os.MkdirTemp("", "duex-edge3")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		os.Chmod(tmpDir3, 0755)
		os.RemoveAll(tmpDir3)
	}()

	// Create 50 files
	for i := 0; i < 50; i++ {
		filePath := filepath.Join(tmpDir3, fmt.Sprintf("file%d.txt", i))
		os.WriteFile(filePath, []byte("data"), 0644)
	}

	progress3 := make(chan string, 100)
	var errorsCount3 int64
	ch3 := make(chan dirSizeResult, 1)

	go func() {
		size, breakdown, errs := DirSize(context.Background(), tmpDir3, progress3, nil, nil, false, 0, &errorsCount3)
		close(progress3)
		ch3 <- dirSizeResult{size, breakdown, errs}
	}()

	first3 := true
	for p := range progress3 {
		t.Logf("[Test 3] Received progress path: %s", p)
		if first3 {
			err := os.Chmod(tmpDir3, 0000)
			t.Logf("[Test 3] os.Chmod(0000) error: %v", err)
			first3 = false
		}
	}
	res3 := <-ch3
	os.Chmod(tmpDir3, 0755)

	if errorsCount3 < 1 {
		t.Errorf("Expected at least 1 error count for unreadable file in DirSize, got %d", errorsCount3)
	}
	if res3.errs < 1 {
		t.Errorf("Expected errs3 to be at least 1, got %d", res3.errs)
	}

	// 4. Cache hit with errors count in DirSize
	tmpDir4, err := os.MkdirTemp("", "duex-edge4")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir4)

	subDir4 := filepath.Join(tmpDir4, "sub")
	if err := os.Mkdir(subDir4, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}
	childDir4 := filepath.Join(subDir4, "child")
	if err := os.Mkdir(childDir4, 0755); err != nil {
		t.Fatalf("Failed to create childdir: %v", err)
	}

	cache := map[string]Result{
		childDir4: {
			TotalSize:   100,
			ErrorsCount: 5,
		},
	}
	var errorsCount4 int64
	_, _, errs4 := DirSize(context.Background(), subDir4, nil, nil, cache, false, 0, &errorsCount4)
	if errorsCount4 != 5 {
		t.Errorf("Expected cached errors count 5 to be accumulated, got %d", errorsCount4)
	}
	if errs4 != 5 {
		t.Errorf("Expected errs4 to be 5, got %d", errs4)
	}
}



