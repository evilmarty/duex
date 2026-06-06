package analyzer

// This file contains mock-based tests that cover code paths which are
// impossible to trigger via real filesystem operations on a healthy macOS/Linux
// system — specifically the entry.Info() error paths, which require a DirEntry
// whose underlying inode disappears between ReadDir and Info().
//
// We inject a custom readDir implementation that returns a mix of normal entries
// and errDirEntry instances (which always fail Info()) to exercise these branches.

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// errDirEntry is a fake fs.DirEntry whose Info() always returns an error,
// simulating an inode that was removed after the ReadDir call.
type errDirEntry struct {
	name  string
	isDir bool
}

func (e errDirEntry) Name() string               { return e.name }
func (e errDirEntry) IsDir() bool                { return e.isDir }
func (e errDirEntry) Type() fs.FileMode          { return 0 }
func (e errDirEntry) Info() (fs.FileInfo, error) { return nil, errors.New("mock Info() error") }

// withErrReadDir overrides the package readDir to inject errDirEntry values
// at the given root path, then restores the original after the test.
func withErrReadDir(t *testing.T, injectAt string, extra []fs.DirEntry, fn func()) {
	t.Helper()
	orig := readDir
	t.Cleanup(func() { readDir = orig })

	readDir = func(name string) ([]fs.DirEntry, error) {
		entries, err := orig(name)
		if err != nil {
			return nil, err
		}
		if name == injectAt {
			entries = append(entries, extra...)
		}
		return entries, nil
	}
	fn()
}

// TestAnalyzeEntryInfoError covers lines 67-79 of analyzer.go:
// the entry.Info() != nil branch inside Analyze's entry loop.
func TestAnalyzeEntryInfoError(t *testing.T) {
	root, err := os.MkdirTemp("", "duex-mock1")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(root)

	// Put a readable file so Analyze has something to process.
	os.WriteFile(filepath.Join(root, "real.txt"), []byte("data"), 0644)

	var errCount int64
	withErrReadDir(t, root, []fs.DirEntry{
		errDirEntry{name: "ghost.txt", isDir: false},
	}, func() {
		res, err := Analyze(context.Background(), root, nil, nil, false, &errCount)
		if err != nil {
			t.Fatalf("Analyze returned unexpected error: %v", err)
		}
		if res.ErrorsCount < 1 {
			t.Errorf("expected ErrorsCount >= 1, got %d", res.ErrorsCount)
		}
		if errCount < 1 {
			t.Errorf("expected errorsCount pointer >= 1, got %d", errCount)
		}
		// Verify the ghost file appears as an unreadable entry.
		found := false
		for _, f := range res.Files {
			if f.Name == "ghost.txt" && f.IsUnreadable {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected ghost.txt to appear as unreadable FileInfo")
		}
	})
}

// TestDirSizeFileEntryInfoError covers lines 254-259 of analyzer.go:
// the entry.Info() != nil branch for regular files inside DirSize.
func TestDirSizeFileEntryInfoError(t *testing.T) {
	root, err := os.MkdirTemp("", "duex-mock2")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(root)

	os.WriteFile(filepath.Join(root, "real.txt"), []byte("data"), 0644)

	var errCount int64
	withErrReadDir(t, root, []fs.DirEntry{
		errDirEntry{name: "ghost.log", isDir: false},
	}, func() {
		var seen sync.Map
		_, _, errs, _ := DirSize(context.Background(), root, nil, &seen, nil, false, 0, &errCount)
		if errs < 1 {
			t.Errorf("expected errs >= 1, got %d", errs)
		}
		if errCount < 1 {
			t.Errorf("expected errCount >= 1, got %d", errCount)
		}
	})
}

// TestDirSizeDirEntryInfoError covers lines 236-242 of analyzer.go:
// the entry.Info() != nil branch for subdirectories inside the oneFileSystem block.
func TestDirSizeDirEntryInfoError(t *testing.T) {
	root, err := os.MkdirTemp("", "duex-mock3")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(root)

	var errCount int64
	withErrReadDir(t, root, []fs.DirEntry{
		errDirEntry{name: "ghostdir", isDir: true},
	}, func() {
		var seen sync.Map
		// Use oneFileSystem=true with rootDev=0 so the device-ID check is skipped
		// and we proceed to entry.Info() for the directory entry.
		_, _, errs, _ := DirSize(context.Background(), root, nil, &seen, nil, true, 0, &errCount)
		if errs < 1 {
			t.Errorf("expected errs >= 1 from errDirEntry dir, got %d", errs)
		}
	})
}
