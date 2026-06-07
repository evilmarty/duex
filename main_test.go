package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
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
	chanBefore := m.progressChan
	cmd := m.Init()

	if cmd == nil {
		t.Fatal("expected Init() to return a batch command")
	}
	if m.progressChan == nil {
		t.Error("expected m.progressChan to be non-nil after initialization")
	}
	if m.progressChan != chanBefore {
		t.Error("expected m.progressChan reference to remain identical after calling Init()")
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
	if m2.topList.Width() != 54 {
		t.Errorf("expected topList width of 54, got %d", m2.topList.Width())
	}
	if m2.list.Height() != 40 { // 50 - 10 = 40
		t.Errorf("expected list height of 40, got %d", m2.list.Height())
	}
	if m2.topList.Height() != 40 {
		t.Errorf("expected topList height of 40, got %d", m2.topList.Height())
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

func TestInvalidateCacheCascading(t *testing.T) {
	m := initialModel("/my/test/path")
	m.dirCache["/my"] = analyzer.Result{}
	m.dirCache["/my/test"] = analyzer.Result{}
	m.dirCache["/my/test/path"] = analyzer.Result{}
	m.dirCache["/my/test/path/child"] = analyzer.Result{}
	m.dirCache["/my/test/path/child/grandchild"] = analyzer.Result{}
	m.dirCache["/my/test/sibling"] = analyzer.Result{}
	m.dirCache["/unrelated"] = analyzer.Result{}

	m.invalidateCache("/my/test/path")

	// Ancestors and descendants should be invalidated
	pathsToCheckDeleted := []string{
		"/my",
		"/my/test",
		"/my/test/path",
		"/my/test/path/child",
		"/my/test/path/child/grandchild",
	}
	for _, p := range pathsToCheckDeleted {
		if _, cached := m.dirCache[p]; cached {
			t.Errorf("expected path %q to be invalidated, but it was not", p)
		}
	}

	// Unrelated or sibling paths should NOT be invalidated
	pathsToCheckPreserved := []string{
		"/my/test/sibling",
		"/unrelated",
	}
	for _, p := range pathsToCheckPreserved {
		if _, cached := m.dirCache[p]; !cached {
			t.Errorf("expected path %q to be preserved, but it was deleted", p)
		}
	}
}

func TestErrorClearingOnScan(t *testing.T) {
	m := initialModel("/my/test/path")
	m.err = errors.New("some error")

	// Triggering startScan should clear err
	m.startScan("/my/test/path")
	if m.err != nil {
		t.Errorf("expected error to be cleared on startScan, but got: %v", m.err)
	}

	// Triggering setItems should clear err
	m.err = errors.New("another error")
	m.setItems(analyzer.Result{})
	if m.err != nil {
		t.Errorf("expected error to be cleared on setItems, but got: %v", m.err)
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
	if !strings.Contains(viewStrLoading, "my / test / path") {
		t.Errorf("expected view to contain breadcrumb path, got:\n%s", viewStrLoading)
	}
	if !strings.Contains(viewStrLoading, "⣾") {
		t.Errorf("expected view to contain spinner, got:\n%s", viewStrLoading)
	}
	if !strings.Contains(viewStrLoading, "  /my/test/path/dir1") {
		t.Errorf("expected scanned paths to render, got:\n%s", viewStrLoading)
	}

	// 2b. Loading state rendering with real-time warnings
	m2b := initialModel("/my/test/path")
	m2b.width = 80
	m2b.height = 24
	m2b.scannedPaths = []string{"/my/test/path/dir1"}
	atomic.StoreInt64(m2b.errorsPtr, 3)
	viewStrLoadingWarn := stripAnsi(m2b.View())
	if !strings.Contains(viewStrLoadingWarn, "Warning: 3 directories/files skipped so far.") {
		t.Errorf("expected view to indicate 3 skipped directories/files, got:\n%s", viewStrLoadingWarn)
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
	// The breadcrumb replaces the old "Path: ..." line; the final segment of
	// the path (/my/test/path) is "path", which should appear in the output.
	if !strings.Contains(viewStrBrowsing, "path") {
		t.Errorf("expected breadcrumb to render current dir segment, got:\n%s", viewStrBrowsing)
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

	// 5. Warning state rendering
	m5 := initialModel("/my/test/path")
	m5.width = 100
	m5.height = 30
	m5.loading = false
	mockResultErr := analyzer.Result{
		Files: []analyzer.FileInfo{
			{Name: "data.csv", Size: 4096, IsDir: false},
		},
		TotalSize:   4096,
		ErrorsCount: 5,
	}
	m5.setItems(mockResultErr)
	viewStrWarn := stripAnsi(m5.View())
	if !strings.Contains(viewStrWarn, "Warning: 5 files/directories were skipped") {
		t.Errorf("expected warning view to contain permission error notification, got:\n%s", viewStrWarn)
	}

	// 6. Height alignment validation
	if h := lipgloss.Height(m2.View()); h != m2.height {
		t.Errorf("expected loading view height to be %d, got %d", m2.height, h)
	}
	if h := lipgloss.Height(m3.View()); h != m3.height {
		t.Errorf("expected browsing view height to be %d, got %d", m3.height, h)
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

func TestKeyMapHelp(t *testing.T) {
	k := keys
	if len(k.ShortHelp()) == 0 {
		t.Error("Expected short help bindings")
	}
	if len(k.FullHelp()) == 0 {
		t.Error("Expected full help bindings")
	}
	if len(k.BrowsingHelp()) == 0 {
		t.Error("Expected browsing help bindings")
	}
	if len(k.ScanningHelp(true)) == 0 {
		t.Error("Expected scanning help with history")
	}
	if len(k.ScanningHelp(false)) == 0 {
		t.Error("Expected scanning help without history")
	}
}

func TestItemMethods(t *testing.T) {
	fi := analyzer.FileInfo{Name: "myfile.txt", Size: 4096, IsDir: false}
	it := item{fi}

	if it.Title() != "myfile.txt" {
		t.Errorf("Title() = %q, want %q", it.Title(), "myfile.txt")
	}
	if it.Description() != "4.0 KB" {
		t.Errorf("Description() = %q, want %q", it.Description(), "4.0 KB")
	}
	if it.FilterValue() != "myfile.txt" {
		t.Errorf("FilterValue() = %q, want %q", it.FilterValue(), "myfile.txt")
	}
}

func TestItemDelegateRender(t *testing.T) {
	fi := analyzer.FileInfo{Name: "nested_folder", Size: 2048, IsDir: true}
	it := item{fi}

	// Make sure we have a width set in list
	l := list.New([]list.Item{it}, itemDelegate{}, 100, 20)
	w := &strings.Builder{}
	
	// Test rendering delegate (should not panic)
	itemDelegate{}.Render(w, l, 0, it)
	if w.Len() == 0 {
		t.Error("Expected render output to not be empty")
	}

	// Test unreadable item
	fiUnreadable := analyzer.FileInfo{Name: "locked_folder", Size: 0, IsDir: true, IsUnreadable: true}
	itUnreadable := item{fiUnreadable}
	if itUnreadable.Description() != "⚠️" {
		t.Errorf("Expected description to be ⚠️, got %q", itUnreadable.Description())
	}

	wUnreadable := &strings.Builder{}
	itemDelegate{}.Render(wUnreadable, l, 0, itUnreadable)
	if !strings.Contains(wUnreadable.String(), "⚠️") {
		t.Errorf("Expected render output for unreadable item to contain ⚠️, got: %s", wUnreadable.String())
	}

	// Test directory with partial errors (contains unreadable children but itself is readable)
	fiPartial := analyzer.FileInfo{Name: "partial_folder", Size: 4096, IsDir: true, ErrorsCount: 2}
	itPartial := item{fiPartial}
	desc := itPartial.Description()
	if !strings.Contains(desc, "⚠️") {
		t.Errorf("Expected description for partial-error dir to contain ⚠️, got %q", desc)
	}
	if !strings.Contains(desc, "4.0 KB") {
		t.Errorf("Expected description for partial-error dir to contain size, got %q", desc)
	}

	wPartial := &strings.Builder{}
	itemDelegate{}.Render(wPartial, l, 0, itPartial)
	if !strings.Contains(wPartial.String(), "⚠️") {
		t.Errorf("Expected render output for partial-error dir to contain ⚠️, got: %s", wPartial.String())
	}
}

func TestUpdateEdgeCases(t *testing.T) {
	// 1. Loading with no history, press Esc -> nothing should change
	m := initialModel("/my/test/path")
	msgEsc := tea.KeyMsg{Type: tea.KeyEsc}
	newModel1, cmd1 := m.Update(msgEsc)
	if cmd1 != nil {
		t.Error("expected no command when escaping without history")
	}
	if newModel1.(model).path != "/my/test/path" {
		t.Error("expected path to remain unchanged")
	}

	// 2. Browsing state, press Enter with no items or non-directory items
	m2 := initialModel("/my/test/path")
	m2.loading = false
	// selection is nil initially
	msgEnter := tea.KeyMsg{Type: tea.KeyEnter}
	_, cmd2 := m2.Update(msgEnter)
	if cmd2 != nil {
		t.Error("expected no command when pressing Enter on empty list")
	}

	// item is a file (non-directory)
	m2.setItems(analyzer.Result{
		Files: []analyzer.FileInfo{
			{Name: "file.txt", Size: 100, IsDir: false},
		},
		TotalSize: 100,
	})
	m2.list.Select(1) // select "file.txt" (index 1 since "." is 0)
	_, cmd3 := m2.Update(msgEnter)
	if cmd3 != nil {
		t.Error("expected no command when pressing Enter on a file")
	}

	// selection is "."
	m2.list.Select(0) // select "."
	_, cmd4 := m2.Update(msgEnter)
	if cmd4 != nil {
		t.Error("expected no command when pressing Enter on '.'")
	}

	// 3. Browsing state, press Backspace when path is root (parent == path)
	m5 := initialModel("/")
	m5.loading = false
	msgBack := tea.KeyMsg{Type: tea.KeyBackspace}
	_, cmd5 := m5.Update(msgBack)
	if cmd5 != nil {
		t.Error("expected no command when navigating back from root")
	}

	// 4. progressMsg when not loading
	m6 := initialModel("/my/test/path")
	m6.loading = false
	_, cmd6 := m6.Update(progressMsg("/my/test/path/foo"))
	if cmd6 != nil {
		t.Error("expected progressMsg to be ignored when not loading")
	}

	// 5. analyzeMsg with non-matching path
	m7 := initialModel("/my/test/path")
	newModel7, cmd7 := m7.Update(analyzeMsg{path: "/different/path", result: analyzer.Result{}})
	// Should not turn off loading since paths don't match
	if !newModel7.(model).loading {
		t.Error("expected loading to remain true when path doesn't match")
	}
	if cmd7 != nil {
		t.Error("expected no command when paths don't match")
	}

	// 6. Unknown key in browsing state
	m8 := initialModel("/my/test/path")
	m8.loading = false
	msgUnk := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	_, _ = m8.Update(msgUnk)
}

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name              string
		args              []string
		wantPath          string
		wantOneFileSystem bool
		wantShowHelp      bool
		wantShowVersion   bool
		wantMinSize       int64
		wantErr           bool
	}{
		{
			name:              "default args",
			args:              []string{},
			wantPath:          ".",
			wantOneFileSystem: true,
			wantShowHelp:      false,
			wantShowVersion:   false,
			wantErr:           false,
		},
		{
			name:              "path only",
			args:              []string{"/some/path"},
			wantPath:          "/some/path",
			wantOneFileSystem: true,
			wantShowHelp:      false,
			wantShowVersion:   false,
			wantErr:           false,
		},
		{
			name:              "cross mounts short flag",
			args:              []string{"-c"},
			wantPath:          ".",
			wantOneFileSystem: false,
			wantShowHelp:      false,
			wantShowVersion:   false,
			wantErr:           false,
		},
		{
			name:              "cross mounts long flag",
			args:              []string{"--cross-mounts"},
			wantPath:          ".",
			wantOneFileSystem: false,
			wantShowHelp:      false,
			wantShowVersion:   false,
			wantErr:           false,
		},
		{
			name:              "help short flag",
			args:              []string{"-h"},
			wantPath:          ".",
			wantOneFileSystem: true,
			wantShowHelp:      true,
			wantShowVersion:   false,
			wantErr:           false,
		},
		{
			name:              "help long flag",
			args:              []string{"--help"},
			wantPath:          ".",
			wantOneFileSystem: true,
			wantShowHelp:      true,
			wantShowVersion:   false,
			wantErr:           false,
		},
		{
			name:              "version short flag",
			args:              []string{"-v"},
			wantPath:          ".",
			wantOneFileSystem: true,
			wantShowHelp:      false,
			wantShowVersion:   true,
			wantErr:           false,
		},
		{
			name:              "version long flag",
			args:              []string{"--version"},
			wantPath:          ".",
			wantOneFileSystem: true,
			wantShowHelp:      false,
			wantShowVersion:   true,
			wantErr:           false,
		},
		{
			name:    "unknown flag",
			args:    []string{"-unknown"},
			wantErr: true,
		},
		{
			name:    "too many arguments",
			args:    []string{"path1", "path2"},
			wantErr: true,
		},
		{
			name:              "min size short flag",
			args:              []string{"-m", "50mb"},
			wantPath:          ".",
			wantOneFileSystem: true,
			wantMinSize:       50 * 1024 * 1024,
			wantShowHelp:      false,
			wantShowVersion:   false,
			wantErr:           false,
		},
		{
			name:              "min size long flag",
			args:              []string{"--min-size", "2gb"},
			wantPath:          ".",
			wantOneFileSystem: true,
			wantMinSize:       2 * 1024 * 1024 * 1024,
			wantShowHelp:      false,
			wantShowVersion:   false,
			wantErr:           false,
		},
		{
			name:    "min size invalid",
			args:    []string{"-m", "invalid"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			path, oneFS, minSize, help, version, err := parseFlags(&buf, tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseFlags() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			expectedMinSize := tt.wantMinSize
			if expectedMinSize == 0 {
				expectedMinSize = 100 * 1024 * 1024
			}
			if minSize != expectedMinSize {
				t.Errorf("minSize = %d, want %d", minSize, expectedMinSize)
			}
			if path != tt.wantPath {
				t.Errorf("path = %q, want %q", path, tt.wantPath)
			}
			if oneFS != tt.wantOneFileSystem {
				t.Errorf("oneFileSystem = %v, want %v", oneFS, tt.wantOneFileSystem)
			}
			if help != tt.wantShowHelp {
				t.Errorf("showHelp = %v, want %v", help, tt.wantShowHelp)
			}
			if version != tt.wantShowVersion {
				t.Errorf("showVersion = %v, want %v", version, tt.wantShowVersion)
			}
		})
	}
}

func TestShowHelpAndMain(t *testing.T) {
	// Call showHelp (which prints to stdout) to cover statements
	showHelp()

	// Backup os.Args and restore it
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// Test main() with -h / --help
	os.Args = []string{"duex", "-h"}
	main()

	// Test main() with -v / --version
	os.Args = []string{"duex", "-v"}
	main()

	// Test main() with -c / --cross-mounts and -h
	os.Args = []string{"duex", "-c", "-h"}
	main()

	os.Args = []string{"duex", "--cross-mounts", "-h"}
	main()
}

func TestStartScanAndProgressCommands(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "duex-cmd-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m := initialModel(tmpDir)
	m.progressChan = make(chan string, 10)

	// Call startScan to get the tea.Cmd
	cmd := m.startScan(tmpDir)
	if cmd == nil {
		t.Fatal("expected startScan to return a command")
	}

	// Execute startScan command synchronously
	msg := cmd()
	if _, ok := msg.(analyzeMsg); !ok {
		t.Errorf("expected analyzeMsg from startScan command, got %T", msg)
	}

	// Send progress path
	m.progressChan <- "somepath"
	
	// Call waitForProgress to get command
	progressCmd := m.waitForProgress(m.progressChan)
	progressMsgVal := progressCmd()
	if progressMsgVal.(progressMsg) != "somepath" {
		t.Errorf("expected progressMsg 'somepath', got %v", progressMsgVal)
	}

	// Close progress channel to cover !ok branch
	close(m.progressChan)
	nilProgressMsg := progressCmd()
	if nilProgressMsg != nil {
		t.Errorf("expected nil progressMsg when channel is closed, got %v", nilProgressMsg)
	}

	// Test startScan error path
	cmdErr := m.startScan("/non/existent/path")
	msgErr := cmdErr()
	if _, ok := msgErr.(error); !ok {
		t.Errorf("expected error from startScan command on non-existent path, got %T", msgErr)
	}
}

func TestMainUnknownFlag(t *testing.T) {
	if os.Getenv("BE_CRASHER") == "1" {
		os.Args = []string{"duex", "-unknown"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMainUnknownFlag")
	cmd.Env = append(os.Environ(), "BE_CRASHER=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Fatalf("process ran with err %v, want exit status 1", err)
}

func TestMainTooManyArgs(t *testing.T) {
	if os.Getenv("BE_CRASHER") == "2" {
		os.Args = []string{"duex", "path1", "path2"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMainTooManyArgs")
	cmd.Env = append(os.Environ(), "BE_CRASHER=2")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Fatalf("process ran with err %v, want exit status 1", err)
}

func TestRenderBreadcrumb(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		name     string
		path     string
		width    int
		wantSegs []string // substrings that must appear in plain-text output
		wantNot  []string // substrings that must NOT appear
	}{
		{
			name:     "home directory collapsed to tilde",
			path:     home,
			width:    80,
			wantSegs: []string{"~"},
		},
		{
			name:     "path under home uses tilde prefix",
			path:     home + "/Code/duex",
			width:    80,
			wantSegs: []string{"~", "Code", "duex"},
		},
		{
			name:     "root path renders as slash segment",
			path:     "/",
			width:    80,
			wantSegs: []string{"/"},
		},
		{
			name:     "absolute path outside home shows all segments",
			path:     "/usr/local/bin",
			width:    80,
			wantSegs: []string{"usr", "local", "bin"},
		},
		{
			name:     "narrow width truncates from the left with ellipsis",
			path:     "/very/deeply/nested/directory/structure/here",
			width:    10,
			wantSegs: []string{"here"},
			wantNot:  []string{"very"},
		},
		{
			name:  "final segment always present regardless of width",
			path:  "/a/b/c/d/e/f/g",
			width: 1,
			// The loop never drops the last segment; even at width=1 'g' will appear
			// (possibly prefixed by '… / ').
			wantSegs: []string{"g"},
		},
		{
			name:     "separator appears between segments",
			path:     "/foo/bar/baz",
			width:    80,
			wantSegs: []string{"foo", "/", "bar", "/", "baz"},
		},
		{
			name:     "single segment path renders correctly",
			path:     "/singledir",
			width:    80,
			wantSegs: []string{"singledir"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := renderBreadcrumb(tc.path, tc.width)
			plain := stripANSI(result)

			for _, want := range tc.wantSegs {
				if !strings.Contains(plain, want) {
					t.Errorf("renderBreadcrumb(%q, %d) = %q; want substring %q", tc.path, tc.width, plain, want)
				}
			}
			for _, bad := range tc.wantNot {
				if strings.Contains(plain, bad) {
					t.Errorf("renderBreadcrumb(%q, %d) = %q; must NOT contain %q", tc.path, tc.width, plain, bad)
				}
			}
		})
	}
}

// stripANSI removes ANSI escape sequences from s for plain-text comparison.
func stripANSI(s string) string {
	var out strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func TestDeleteFile(t *testing.T) {
	// Mock removeAll
	var deletedPath string
	var deleteErr error
	oldRemoveAll := removeAll
	defer func() { removeAll = oldRemoveAll }()
	removeAll = func(path string) error {
		deletedPath = path
		return deleteErr
	}

	// 1. Initial setup of the model with items
	m := initialModel("/my/test/path")
	m.loading = false
	m.width = 80
	m.height = 24

	// Populate list items: "." and a file "file.txt"
	m.list.SetItems([]list.Item{
		item{analyzer.FileInfo{Name: ".", Path: "/my/test/path", IsDir: true}},
		item{analyzer.FileInfo{Name: "file.txt", Path: "/my/test/path/file.txt", Size: 100}},
	})
	m.list.ResetSelected() // selects "." initially

	// 2. Pressing "d" on "." should be ignored (since we block deleting ".")
	msgD := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}
	newModel, cmd := m.Update(msgD)
	m2 := newModel.(model)
	if m2.deleteTarget != nil {
		t.Error("expected deleteTarget to be nil when pressing d on '.'")
	}
	if cmd != nil {
		t.Error("expected no command when trying to delete '.'")
	}

	// 3. Move cursor down to "file.txt" and press "d"
	m.list.Select(1) // select "file.txt"
	newModel, cmd = m.Update(msgD)
	m2 = newModel.(model)
	if m2.deleteTarget == nil {
		t.Fatal("expected deleteTarget to be set after pressing d on 'file.txt'")
	}
	if m2.deleteTarget.Name != "file.txt" {
		t.Errorf("expected deleteTarget name to be 'file.txt', got %s", m2.deleteTarget.Name)
	}
	if m2.deleting {
		t.Error("expected deleting to be false initially")
	}
	if m2.confirmDeleteSelected {
		t.Error("expected confirmDeleteSelected to default to false (Cancel)")
	}
	if cmd != nil {
		t.Error("expected no command immediately after pressing d")
	}

	// Check View in confirmation state
	viewStr := m2.View()
	plainView := stripANSI(viewStr)
	if !strings.Contains(plainView, "Confirm Deletion") {
		t.Error("expected view to contain 'Confirm Deletion'")
	}
	if !strings.Contains(plainView, "/my/test/path/file.txt") {
		t.Error("expected view to contain deleted file path")
	}
	if !strings.Contains(plainView, "Confirm") || !strings.Contains(plainView, "Cancel") {
		t.Error("expected view to contain Confirm and Cancel buttons")
	}
	if !strings.Contains(plainView, "toggle") || !strings.Contains(plainView, "select") {
		t.Error("expected help views for toggle and select keys")
	}

	// 4. Test keyboard toggling (left/right/tab)
	msgTab := tea.KeyMsg{Type: tea.KeyTab}
	newModelToggle, cmdToggle := m2.Update(msgTab)
	mToggle := newModelToggle.(model)
	if !mToggle.confirmDeleteSelected {
		t.Error("expected confirmDeleteSelected to toggle to true (Confirm)")
	}
	if cmdToggle != nil {
		t.Error("expected no command on toggle")
	}

	// Toggle back to Cancel
	newModelToggle2, _ := mToggle.Update(msgTab)
	mToggle2 := newModelToggle2.(model)
	if mToggle2.confirmDeleteSelected {
		t.Error("expected confirmDeleteSelected to toggle back to false (Cancel)")
	}

	// 5. Pressing enter on Cancel defaults to canceling deletion
	msgEnter := tea.KeyMsg{Type: tea.KeyEnter}
	newModelEnterCancel, cmdEnterCancel := mToggle2.Update(msgEnter)
	mEnterCancel := newModelEnterCancel.(model)
	if mEnterCancel.deleteTarget != nil {
		t.Error("expected deleteTarget to be cleared on Cancel enter")
	}
	if cmdEnterCancel != nil {
		t.Error("expected no command when canceling deletion via enter")
	}

	// 6. Confirm deletion (press 'y' shortcut)
	msgY := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}
	newModelConfirm, cmdConfirm := m2.Update(msgY)
	mConfirm := newModelConfirm.(model)
	if !mConfirm.deleting {
		t.Error("expected deleting state to be true after confirmation")
	}
	if cmdConfirm == nil {
		t.Fatal("expected a delete command to be returned")
	}

	// Check View in active deletion state
	viewStrConfirm := mConfirm.View()
	plainViewConfirm := stripANSI(viewStrConfirm)
	if !strings.Contains(plainViewConfirm, "Deleting...") {
		t.Error("expected view to contain 'Deleting...'")
	}

	// Execute command to simulate async deletion
	msgResult := cmdConfirm()
	delRes, ok := msgResult.(deleteResultMsg)
	if !ok {
		t.Fatalf("expected deleteResultMsg, got %T", msgResult)
	}
	if delRes.err != nil {
		t.Errorf("expected no error in deleteResultMsg, got %v", delRes.err)
	}

	// Feed deleteResultMsg back into model
	newModelAfterDel, cmdAfterDel := mConfirm.Update(delRes)
	mAfterDel := newModelAfterDel.(model)
	if mAfterDel.deleting {
		t.Error("expected deleting to be false after result handled")
	}
	if mAfterDel.deleteTarget != nil {
		t.Error("expected deleteTarget to be nil after result handled")
	}
	if !mAfterDel.loading {
		t.Error("expected model to trigger scan/loading after successful deletion")
	}
	if cmdAfterDel == nil {
		t.Fatal("expected batch scan commands after deletion")
	}
	if deletedPath != "/my/test/path/file.txt" {
		t.Errorf("expected removeAll to be called with '/my/test/path/file.txt', got %q", deletedPath)
	}

	// 7. Toggle to Confirm and press Enter to confirm deletion
	mToggle.deleting = false
	newModelEnterConfirm, cmdEnterConfirm := mToggle.Update(msgEnter)
	mEnterConfirm := newModelEnterConfirm.(model)
	if !mEnterConfirm.deleting {
		t.Error("expected deleting state to be true after confirming via Enter key")
	}
	if cmdEnterConfirm == nil {
		t.Fatal("expected a delete command to be returned on Confirm enter")
	}

	// 8. Test deletion failure
	deleteErr = errors.New("permission denied")
	newModelConfirmErr, cmdConfirmErr := m2.Update(msgY)
	mConfirmErr := newModelConfirmErr.(model)
	msgResultErr := cmdConfirmErr()
	newModelAfterDelErr, cmdAfterDelErr := mConfirmErr.Update(msgResultErr)
	mAfterDelErr := newModelAfterDelErr.(model)
	if mAfterDelErr.err != deleteErr {
		t.Errorf("expected model error to be set to %v, got %v", deleteErr, mAfterDelErr.err)
	}
	if mAfterDelErr.deleteTarget != nil {
		t.Error("expected deleteTarget to be cleared on deletion error")
	}
	if cmdAfterDelErr != nil {
		t.Error("expected no command returned on error")
	}
}

func TestTabsFeature(t *testing.T) {
	// 1. Initial State
	m := initialModel("/my/test/path")
	m.loading = false
	if m.activeTab != 0 {
		t.Errorf("expected activeTab to be 0, got %d", m.activeTab)
	}

	// 2. Tab Key Switch
	msgTab := tea.KeyMsg{Type: tea.KeyTab}
	newModel, cmd := m.Update(msgTab)
	m2 := newModel.(model)
	if cmd != nil {
		t.Error("expected no command on tab switch")
	}
	if m2.activeTab != 1 {
		t.Errorf("expected activeTab to switch to 1, got %d", m2.activeTab)
	}

	// Switch back
	newModelBack, _ := m2.Update(msgTab)
	mBack := newModelBack.(model)
	if mBack.activeTab != 0 {
		t.Errorf("expected activeTab to switch back to 0, got %d", mBack.activeTab)
	}

	// 3. setItems populates both list and topList
	mockResult := analyzer.Result{
		Files: []analyzer.FileInfo{
			{Name: "data.csv", Path: "/my/test/path/data.csv", Size: 4096, IsDir: false},
		},
		TotalSize: 4096,
		TopFiles: []analyzer.FileInfo{
			{Name: "sub1.txt", Path: "/my/test/path/sub/sub1.txt", Size: 10000, IsDir: false},
		},
	}
	m.setItems(mockResult)
	if len(m.list.Items()) != 2 { // "." and "data.csv"
		t.Errorf("expected 2 items in list, got %d", len(m.list.Items()))
	}
	if len(m.topList.Items()) != 1 { // "sub1.txt"
		t.Errorf("expected 1 item in topList, got %d", len(m.topList.Items()))
	}

	// Verify relPath in topItem
	topItem0 := m.topList.Items()[0].(topItem)
	if topItem0.relPath != "sub/sub1.txt" {
		t.Errorf("expected relPath 'sub/sub1.txt', got %q", topItem0.relPath)
	}

	// 4. Pressing Enter on a topItem navigates to its parent
	m2.setItems(mockResult) // activeTab = 1
	m2.topList.Select(0)    // select the topItem

	msgEnter := tea.KeyMsg{Type: tea.KeyEnter}
	newModelEnter, cmdEnter := m2.Update(msgEnter)
	if cmdEnter == nil {
		t.Error("expected non-nil command returned to trigger scanning parent directory")
	}
	m3 := newModelEnter.(model)

	if m3.activeTab != 0 {
		t.Error("expected activeTab to switch back to 0 (Directory view) after enter")
	}
	if m3.path != "/my/test/path/sub" {
		t.Errorf("expected parent path '/my/test/path/sub', got %q", m3.path)
	}
	if m3.targetFileToSelect != "/my/test/path/sub/sub1.txt" {
		t.Errorf("expected targetFileToSelect to be the file path, got %q", m3.targetFileToSelect)
	}

	// 5. Test selection and highlighting
	mockResultSub := analyzer.Result{
		Files: []analyzer.FileInfo{
			{Name: "sub1.txt", Path: "/my/test/path/sub/sub1.txt", Size: 10000, IsDir: false},
		},
		TotalSize: 10000,
	}
	m3.setItems(mockResultSub)
	// sub1.txt should be selected. Since "." is index 0, "sub1.txt" is index 1.
	if m3.list.Index() != 1 {
		t.Errorf("expected list index to be 1 (selected sub1.txt), got %d", m3.list.Index())
	}
}

func TestEmptyTopFilesViewRenderingWidth(t *testing.T) {
	m := initialModel("/my/test/path")
	m.width = 100
	m.height = 30
	m.loading = false
	m.activeTab = 1 // Top Files tab
	m.updateListSizes()

	// Ensure there are no top files
	mockResult := analyzer.Result{
		Files:     []analyzer.FileInfo{},
		TotalSize: 0,
		TopFiles:  []analyzer.FileInfo{},
	}
	m.setItems(mockResult)

	viewStr := m.View()
	// Split view into lines
	lines := strings.Split(viewStr, "\n")

	foundNoItems := false
	for _, line := range lines {
		plain := stripAnsi(line)
		if strings.Contains(plain, "No items") {
			foundNoItems = true
			// The list view is styled to leftWidth = m.width - 40 - 6 = 54.
			// When padded with space, the plain text line should have at least 54 chars.
			if len(plain) < 54 {
				t.Errorf("expected line containing 'No items' to be at least 54 chars wide, got %d: %q", len(plain), plain)
			}
		}
	}
	if !foundNoItems {
		t.Error("expected to find 'No items' in the empty Top Files view")
	}
}
