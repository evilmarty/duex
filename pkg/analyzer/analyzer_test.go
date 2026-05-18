package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyze(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "dude-test")
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

	result, err := Analyze(tmpDir, nil)
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
}

func TestAnalyzeHardLinks(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "dude-hardlink-test")
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

	result, err := Analyze(tmpDir, nil)
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
