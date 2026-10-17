package analyzer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMacOSScanPolicy(t *testing.T) {
	origRead, origStat := readFirmlinks, statVolume
	t.Cleanup(func() { readFirmlinks, statVolume = origRead, origStat })
	volume, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	readFirmlinks = func(path string) ([]byte, error) {
		if path != "/usr/share/firmlinks" {
			t.Errorf("unexpected table path: %s", path)
		}
		return []byte("Users\tUsers\n"), nil
	}
	statVolume = func(path string) (os.FileInfo, error) {
		if path != "/System/Volumes/Data" {
			t.Errorf("unexpected volume path: %s", path)
		}
		return volume, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeRoot, err := filepath.Rel(cwd, "/")
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{"/", "/.", "/System/..", relativeRoot} {
		p, err := loadScanPolicy(root, true)
		if err != nil {
			t.Fatal(err)
		}
		if !p.excludes(filepath.Join(root, "System/Volumes/Data/Users")) || p.dataDev == nil || *p.dataDev != getFileStats(volume).Dev {
			t.Fatalf("unexpected root policy: %+v", p)
		}
	}
	p, err := loadScanPolicy("/", false)
	if err != nil || p.dataDev != nil || !p.excludes("/System/Volumes/Data/Users") {
		t.Fatalf("cross-mount scan should still exclude aliases: %+v, %v", p, err)
	}
	for _, root := range []string{"/System", "/System/Volumes"} {
		p, err := loadScanPolicy(root, true)
		if err != nil || p.dataDev == nil {
			t.Fatalf("System-volume navigation must still reach Data: %+v, %v", p, err)
		}
	}
	for _, root := range []string{"/Users", "/System/Volumes/Data", "/System/Volumes/Data/Users"} {
		p, err := loadScanPolicy(root, true)
		if err != nil || len(p.aliases) != 0 || p.dataDev != nil {
			t.Fatalf("ordinary scan should have no root policy: %+v, %v", p, err)
		}
	}
	denied := errors.New("access denied")
	statVolume = func(string) (os.FileInfo, error) { return nil, denied }
	if _, err := loadScanPolicy("/", true); !errors.Is(err, denied) {
		t.Fatalf("volume stat error = %v", err)
	}
	readFirmlinks = func(string) ([]byte, error) { return nil, denied }
	if _, err := loadScanPolicy("/", true); !errors.Is(err, denied) {
		t.Fatalf("table read error = %v", err)
	}
	readFirmlinks = func(string) ([]byte, error) { return nil, os.ErrNotExist }
	if p, err := loadScanPolicy("/", true); err != nil || len(p.aliases) != 0 {
		t.Fatalf("pre-Catalina scan policy: %+v, %v", p, err)
	}
	readFirmlinks = func(string) ([]byte, error) { return []byte("invalid"), nil }
	if _, err := loadScanPolicy("/", true); err == nil {
		t.Fatal("expected malformed table error")
	}
}
