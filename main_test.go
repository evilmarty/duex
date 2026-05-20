package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"duex/pkg/analyzer"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Helper to strip ANSI Escape sequences (colors/borders) to easily assert text contents.
func stripAnsi(str string) string {
	var sb strings.Builder
	inAnsi := false
	for i := 0; i < len(str); i++ {
		if str[i] == '\x1b' {
			inAnsi = true
			continue
		}
		if inAnsi {
			if (str[i] >= 'a' && str[i] <= 'z') || (str[i] >= 'A' && str[i] <= 'Z') {
				inAnsi = false
			}
			continue
		}
		sb.WriteByte(str[i])
	}
	return sb.String()
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1099511627776, "1.0 TB"},
	}

	for _, tt := range tests {
		got := formatSize(tt.bytes)
		if got != tt.want {
			t.Errorf("formatSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestGetType(t *testing.T) {
	tests := []struct {
		info analyzer.FileInfo
		want string
	}{
		{analyzer.FileInfo{IsDir: true, Name: "docs"}, "Directory"},
		{analyzer.FileInfo{IsDir: false, Name: "data.json"}, "JSON File"},
		{analyzer.FileInfo{IsDir: false, Name: "main.go"}, "GO File"},
		{analyzer.FileInfo{IsDir: false, Name: "config"}, "File"},
		{analyzer.FileInfo{IsDir: false, Name: "extremely_long_extension.longextensionname"}, "File"},
	}

	for _, tt := range tests {
		got := getType(tt.info)
		if got != tt.want {
			t.Errorf("getType(%+v) = %q, want %q", tt.info, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		str  string
		max  int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 8, "hello..."},
		{"hello world", 3, "..."},
		{"abc", 2, "..."},
	}

	for _, tt := range tests {
		got := truncate(tt.str, tt.max)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.str, tt.max, got, tt.want)
		}
	}
}

func TestInitialModel(t *testing.T) {
	m := initialModel("/my/test/path")

	if m.path != "/my/test/path" {
		t.Errorf("expected path to be '/my/test/path', got %q", m.path)
	}
	if !m.loading {
		t.Error("expected loading state to be true initially")
	}
	if m.dirCache == nil {
		t.Error("expected dirCache to be initialized")
	}
	if m.selected == nil {
		t.Error("expected selected map to be initialized")
	}
}

func TestModelInit(t *testing.T) {
	m := initialModel("/my/test/path")
	cmd := m.Init()

	if cmd == nil {
		t.Fatal("expected Init() to return a batch command")
	}
}

func TestUpdateSpinnerTick(t *testing.T) {
	m := initialModel("/my/test/path")
	tick := spinner.TickMsg{}
	newModel, _ := m.Update(tick)
	m2 := newModel.(model)

	// Simple check that it returns the updated model without panic
	if m2.path != m.path {
		t.Errorf("expected path to remain '%s', got '%s'", m.path, m2.path)
	}
}

func TestUpdateWindowSize(t *testing.T) {
	m := initialModel("/my/test/path")
	m.loading = false

	msg := tea.WindowSizeMsg{Width: 100, Height: 50}
	newModel, cmd := m.Update(msg)
	m2 := newModel.(model)

	if cmd != nil {
		t.Error("expected no command on window resize")
	}
	if m2.width != 100 || m2.height != 50 {
		t.Errorf("expected model dims 100x50, got %dx%d", m2.width, m2.height)
	}
	// Verify list dimensions are successfully updated
	if m2.list.Width() != 54 { // 100 - 40 - 6 = 54
		t.Errorf("expected list width of 54, got %d", m2.list.Width())
	}
	if m2.list.Height() != 40 { // 50 - 10 = 40
		t.Errorf("expected list height of 40, got %d", m2.list.Height())
	}
}

func TestUpdateProgressMsg(t *testing.T) {
	m := initialModel("/my/test/path")
	m.height = 10
	m.progressChan = make(chan string, 10)

	msg := progressMsg("/my/test/path/file.txt")
	newModel, cmd := m.Update(msg)
	m2 := newModel.(model)

	if cmd == nil {
		t.Error("expected progressMsg handler to return a tea.Cmd for next progress element")
	}
	if len(m2.scannedPaths) != 1 {
		t.Errorf("expected 1 scanned path, got %d", len(m2.scannedPaths))
	}
	if m2.scannedPaths[0] != "/my/test/path/file.txt" {
		t.Errorf("expected scanned path to match, got %q", m2.scannedPaths[0])
	}

	// Verify scannedPaths queue truncation based on height
	// height is 10, maxItems is 10 - 7 = 3.
	m2.scannedPaths = []string{"a", "b", "c"}
	newModel3, _ := m2.Update(progressMsg("d"))
	m3 := newModel3.(model)
	if len(m3.scannedPaths) != 3 {
		t.Errorf("expected scannedPaths to be truncated to max 3 items, got %d", len(m3.scannedPaths))
	}
	if m3.scannedPaths[2] != "d" || m3.scannedPaths[0] != "b" {
		t.Errorf("expected b, c, d, got %v", m3.scannedPaths)
	}
}

func TestUpdateAnalyzeMsg(t *testing.T) {
	m := initialModel("/my/test/path")

	mockResult := analyzer.Result{
		Files: []analyzer.FileInfo{
			{Name: "file.txt", Size: 100, IsDir: false},
		},
		TotalSize: 100,
	}

	msg := analyzeMsg{path: "/my/test/path", result: mockResult}
	newModel, cmd := m.Update(msg)
	m2 := newModel.(model)

	if cmd != nil {
		t.Error("expected no command on analyze completion")
	}
	if m2.loading {
		t.Error("expected loading state to turn false")
	}
	if len(m2.scannedPaths) != 0 {
		t.Error("expected scannedPaths to be cleared")
	}
	if len(m2.list.Items()) != 2 { // "." and "file.txt"
		t.Errorf("expected 2 items, got %d", len(m2.list.Items()))
	}
	if m2.dirCache["/my/test/path"].TotalSize != 100 {
		t.Error("expected result to be stored in cache")
	}
}

func TestUpdateError(t *testing.T) {
	// Test cancellation error (should be ignored)
	m := initialModel("/my/test/path")
	newModel1, cmd1 := m.Update(context.Canceled)
	m1 := newModel1.(model)
	if m1.err != nil {
		t.Errorf("expected cancellation error to be ignored, got %v", m1.err)
	}
	if cmd1 != nil {
		t.Error("expected no command on cancel error")
	}

	// Test regular error
	err := errors.New("failed scan")
	newModel2, _ := m.Update(err)
	m2 := newModel2.(model)
	if m2.err != err {
		t.Errorf("expected error to be recorded, got %v", m2.err)
	}
	if m2.loading {
		t.Error("expected loading to be false on error")
	}
}

func TestUpdateKeyQuit(t *testing.T) {
	keysToTest := []string{"q", "ctrl+c"}

	for _, k := range keysToTest {
		m := initialModel("/my/test/path")
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		if k == "ctrl+c" {
			msg = tea.KeyMsg{Type: tea.KeyCtrlC}
		}

		_, cmd := m.Update(msg)
		if cmd == nil {
			t.Fatalf("expected command for quit key %q", k)
		}
		resolvedMsg := cmd()
		if _, ok := resolvedMsg.(tea.QuitMsg); !ok {
			t.Errorf("expected tea.QuitMsg for key %q, got %T", k, resolvedMsg)
		}
	}
}

func TestUpdateKeyEsc(t *testing.T) {
	// 1. Loading with history
	m := initialModel("/my/test/path/child")
	m.history = []string{"/my/test/path"}
	m.dirCache["/my/test/path"] = analyzer.Result{TotalSize: 10}

	msg := tea.KeyMsg{Type: tea.KeyEsc}
	newModel, cmd := m.Update(msg)
	m2 := newModel.(model)

	if cmd != nil {
		t.Error("expected no command when escaping back to a cached path")
	}
	if m2.path != "/my/test/path" {
		t.Errorf("expected path to revert to history, got %s", m2.path)
	}
	if m2.loading {
		t.Error("expected loading state to turn false since target path is cached")
	}

	// 2. Loading with history, target not cached
	m = initialModel("/my/test/path/child")
	m.history = []string{"/my/test/path"}
	newModelUncached, cmdUncached := m.Update(msg)
	mUncached := newModelUncached.(model)

	if cmdUncached == nil {
		t.Error("expected batch command for scan restart since history path is not cached")
	}
	if mUncached.path != "/my/test/path" {
		t.Errorf("expected path to revert, got %s", mUncached.path)
	}
	if !mUncached.loading {
		t.Error("expected loading to remain true to rescan path")
	}

	// 3. Browsing state (esc should be ignored to prevent list default action)
	m = initialModel("/my/test/path")
	m.loading = false
	newModelBrowsing, cmdBrowsing := m.Update(msg)
	if cmdBrowsing != nil {
		t.Error("expected esc key to be ignored in browsing mode")
	}
	mBrowsing := newModelBrowsing.(model)
	if mBrowsing.loading {
		t.Error("expected loading state to remain false")
	}
}

func TestUpdateKeyRefresh(t *testing.T) {
	m := initialModel("/my/test/path")
	m.loading = false
	m.dirCache["/my/test/path"] = analyzer.Result{TotalSize: 10}

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}}
	newModel, cmd := m.Update(msg)
	m2 := newModel.(model)

	if cmd == nil {
		t.Error("expected refresh command to be returned")
	}
	if !m2.loading {
		t.Error("expected loading to become true on refresh")
	}
	if _, cached := m2.dirCache["/my/test/path"]; cached {
		t.Error("expected cache for path to be invalidated")
	}
}

func TestUpdateKeyEnter(t *testing.T) {
	// Enter directory -> trigger scan
	m := initialModel("/my/test/path")
	m.loading = false

	mockResult := analyzer.Result{
		Files: []analyzer.FileInfo{
			{Name: "subfolder", Path: "/my/test/path/subfolder", Size: 20, IsDir: true},
			{Name: "file.txt", Path: "/my/test/path/file.txt", Size: 10, IsDir: false},
		},
		TotalSize: 30,
	}
	m.setItems(mockResult)

	// Ensure selection is on "subfolder"
	m.list.Select(1) // 0 is ".", 1 is "subfolder", 2 is "file.txt"

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	newModel, cmd := m.Update(msg)
	m2 := newModel.(model)

	if cmd == nil {
		t.Error("expected enter key to return scan command for new directory")
	}
	if m2.path != "/my/test/path/subfolder" {
		t.Errorf("expected path to change to subfolder, got %s", m2.path)
	}
	if len(m2.history) != 1 || m2.history[0] != "/my/test/path" {
		t.Errorf("expected history to capture previous path, got %v", m2.history)
	}
	if !m2.loading {
		t.Error("expected loading to become true")
	}

	// Try cached target
	m3 := initialModel("/my/test/path")
	m3.loading = false
	m3.dirCache["/my/test/path/subfolder"] = analyzer.Result{TotalSize: 50}
	m3.setItems(mockResult)
	m3.list.Select(1) // subfolder

	newModelCached, cmdCached := m3.Update(msg)
	m4 := newModelCached.(model)
	if cmdCached != nil {
		t.Error("expected no command for cached target")
	}
	if m4.loading {
		t.Error("expected loading to remain false for cached target")
	}
	if m4.path != "/my/test/path/subfolder" {
		t.Errorf("expected path to update, got %s", m4.path)
	}
}

func TestUpdateKeyBackspace(t *testing.T) {
	m := initialModel("/my/test/path/subfolder")
	m.loading = false

	msg := tea.KeyMsg{Type: tea.KeyBackspace}
	newModel, cmd := m.Update(msg)
	m2 := newModel.(model)

	if cmd == nil {
		t.Error("expected backspace command to be returned to scan parent path")
	}
	if m2.path != "/my/test/path" {
		t.Errorf("expected path to change to parent, got %s", m2.path)
	}
	if len(m2.history) != 1 || m2.history[0] != "/my/test/path/subfolder" {
		t.Errorf("expected parent navigation to update history, got %v", m2.history)
	}
}

func TestViewRenderingOutputs(t *testing.T) {
	lipgloss.SetHasDarkBackground(true)

	// 1. Error state rendering
	m := initialModel("/my/test/path")
	m.err = errors.New("disk failure")
	m.loading = false
	viewStr := stripAnsi(m.View())
	if !strings.Contains(viewStr, "Error: disk failure") {
		t.Errorf("expected view to contain error message, got:\n%s", viewStr)
	}

	// 2. Loading state rendering
	m2 := initialModel("/my/test/path")
	m2.width = 80
	m2.height = 24
	m2.scannedPaths = []string{"/my/test/path/dir1", "/my/test/path/dir2"}
	viewStrLoading := stripAnsi(m2.View())
	if !strings.Contains(viewStrLoading, "Scanning directory...") {
		t.Errorf("expected view to indicate scanning, got:\n%s", viewStrLoading)
	}
	if !strings.Contains(viewStrLoading, "  /my/test/path/dir1") {
		t.Errorf("expected scanned paths to render, got:\n%s", viewStrLoading)
	}

	// 3. Browsing state rendering
	m3 := initialModel("/my/test/path")
	m3.width = 100
	m3.height = 30
	m3.loading = false
	mockResult := analyzer.Result{
		Files: []analyzer.FileInfo{
			{Name: "data.csv", Size: 4096, IsDir: false},
		},
		TotalSize: 4096,
		Breakdown: []analyzer.Breakdown{
			{Extension: "csv", Size: 4096},
		},
	}
	m3.setItems(mockResult)
	m3.list.Select(1) // select data.csv (index 1 since "." is 0)

	viewStrBrowsing := stripAnsi(m3.View())
	if !strings.Contains(viewStrBrowsing, "Path: /my/test/path") {
		t.Errorf("expected path header to render, got:\n%s", viewStrBrowsing)
	}
	if !strings.Contains(viewStrBrowsing, "data.csv") {
		t.Errorf("expected scanned files in list, got:\n%s", viewStrBrowsing)
	}
	if !strings.Contains(viewStrBrowsing, "Details") {
		t.Errorf("expected detail panel title, got:\n%s", viewStrBrowsing)
	}
	if !strings.Contains(viewStrBrowsing, "Type: CSV File") {
		t.Errorf("expected file details to be formatted, got:\n%s", viewStrBrowsing)
	}

	// 4. Browsing state with directory selected
	m4 := initialModel("/my/test/path")
	m4.width = 100
	m4.height = 30
	m4.loading = false
	mockResultDir := analyzer.Result{
		Files: []analyzer.FileInfo{
			{
				Name:  "nested_dir",
				Size:  9000,
				IsDir: true,
				Breakdown: []analyzer.Breakdown{
					{Extension: "txt", Size: 9000},
				},
			},
		},
		TotalSize: 9000,
	}
	m4.list.SetHeight(20) // set height > 7 to enable breakdown panel rendering
	m4.setItems(mockResultDir)
	// select nested_dir (index 1 since "." is 0)
	m4.list.Select(1)

	viewStrDirSelected := stripAnsi(m4.View())
	if !strings.Contains(viewStrDirSelected, "Breakdown") {
		t.Errorf("expected directory details to contain file breakdown heading, got:\n%s", viewStrDirSelected)
	}
	if !strings.Contains(viewStrDirSelected, "  txt        8.8 KB") {
		t.Errorf("expected file breakdown items to render, got:\n%s", viewStrDirSelected)
	}
}

func TestListFilteringState(t *testing.T) {
	m := initialModel("/my/test/path")
	m.loading = false

	mockResult := analyzer.Result{
		Files: []analyzer.FileInfo{
			{Name: "abc.txt", Size: 100, IsDir: false},
			{Name: "xyz.txt", Size: 200, IsDir: false},
		},
		TotalSize: 300,
	}
	m.setItems(mockResult)

	// Simulate hitting the filter key "/"
	msgFilter := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}
	newModel, cmd := m.Update(msgFilter)
	m2 := newModel.(model)

	if cmd == nil {
		t.Fatal("expected list component to return command on filter activation")
	}
	if m2.list.FilterState() != list.Filtering {
		t.Errorf("expected list component state to be Filtering, got %v", m2.list.FilterState())
	}
}
