package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"duex/pkg/analyzer"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1).
			MarginBottom(1)

	itemStyle = lipgloss.NewStyle().PaddingLeft(2)

	selectedItemStyle = lipgloss.NewStyle().
				PaddingLeft(2).
				Foreground(lipgloss.Color("#7D56F4")).
				Bold(true)

	sizeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	faintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))

	detailStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(0, 2).
			MarginLeft(2).
			Width(40)

	containerStyle = lipgloss.NewStyle().
			Padding(0, 2)

	// Breadcrumb styles
	crumbStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#6C6C6C"))
	crumbSep      = lipgloss.NewStyle().Foreground(lipgloss.Color("#6C6C6C")).Render(" / ")
	crumbActive   = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)
)

type item struct {
	analyzer.FileInfo
}

type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Enter   key.Binding
	Back    key.Binding
	Cancel  key.Binding
	Refresh key.Binding
	Filter  key.Binding
	Quit    key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter, k.Back},
		{k.Refresh, k.Filter, k.Quit},
	}
}

func (k keyMap) BrowsingHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Back, k.Refresh, k.Filter, k.Quit}
}

func (k keyMap) ScanningHelp(hasHistory bool) []key.Binding {
	if hasHistory {
		return []key.Binding{k.Cancel, k.Quit}
	}
	return []key.Binding{k.Quit}
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "open"),
	),
	Back: key.NewBinding(
		key.WithKeys("backspace"),
		key.WithHelp("backspace", "up"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh"),
	),
	Filter: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "filter"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}

func (i item) Title() string       { return i.Name }
func (i item) Description() string {
	if i.IsUnreadable {
		return "⚠️"
	}
	if i.IsDir && i.ErrorsCount > 0 {
		return "⚠️ " + formatSize(i.Size)
	}
	return formatSize(i.Size)
}
func (i item) FilterValue() string { return i.Name }

type itemDelegate struct{}

func (d itemDelegate) Height() int                               { return 1 }
func (d itemDelegate) Spacing() int                              { return 0 }
func (d itemDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil }
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(item)
	if !ok {
		return
	}

	str := i.Name
	if i.IsDir {
		str += "/"
	}

	cursor := " "
	style := itemStyle
	if index == m.Index() {
		cursor = ">"
		style = selectedItemStyle
	}

	sizeStr := formatSize(i.Size)
	sizeLen := len(sizeStr)
	// Build the size style: when selected, size should also be bold.
	effectiveSizeStyle := sizeStyle
	if index == m.Index() {
		effectiveSizeStyle = sizeStyle.Bold(true)
	}

	var sizeRendered string
	if i.IsUnreadable {
		sizeStr = "⚠️ "
		sizeLen = 3
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#D1A100")).Bold(true)
		sizeRendered = warnStyle.Render("⚠️ ")
	} else if i.IsDir && i.ErrorsCount > 0 {
		sizeStr = "⚠️ " + sizeStr
		sizeLen = 3 + len(formatSize(i.Size))
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#D1A100")).Bold(true)
		sizeRendered = warnStyle.Render("⚠️ ") + effectiveSizeStyle.Render(formatSize(i.Size))
	} else {
		sizeRendered = effectiveSizeStyle.Render(sizeStr)
	}

	// Calculate dynamic width. Subtract cursor(2), size padding(2), and size length.
	// Ensure we have a minimum width for the name.
	nameWidth := m.Width() - sizeLen - 5
	if nameWidth < 10 {
		nameWidth = 10
	}

	namePart := fmt.Sprintf("%s %-*s", cursor, nameWidth, truncate(str, nameWidth))
	fmt.Fprint(w, style.Render(namePart)+" "+sizeRendered)
}

type model struct {
	path          string
	selected      map[int]struct{}
	err           error
	loading       bool
	width         int
	height        int
	dirCache      map[string]analyzer.Result
	spinner       spinner.Model
	scannedPaths  []string
	progressChan  chan string
	list          list.Model
	cancel        context.CancelFunc
	history       []string
	help          help.Model
	oneFileSystem bool
	errorsCount   int64 // Track permission/access errors in currently viewed path
	errorsPtr     *int64 // Pointer to the errors count passed to the analyzer
}

type analyzeMsg struct {
	path   string
	result analyzer.Result
}


type progressMsg string

func initialModel(path string) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))

	l := list.New([]list.Item{}, itemDelegate{}, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.Styles.PaginationStyle = lipgloss.NewStyle().PaddingLeft(2)

	return model{
		path:          path,
		selected:      make(map[int]struct{}),
		loading:       true,
		dirCache:      make(map[string]analyzer.Result),
		spinner:       s,
		list:          l,
		help:          help.New(),
		oneFileSystem: true,
		errorsPtr:     new(int64),
		progressChan:  make(chan string, 100),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.startScan(m.path),
		m.waitForProgress(m.progressChan),
	)
}

func (m *model) startScan(targetPath string) tea.Cmd {
	m.err = nil
	if m.cancel != nil {
		m.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	if m.errorsPtr == nil {
		m.errorsPtr = new(int64)
	} else {
		atomic.StoreInt64(m.errorsPtr, 0)
	}

	return func() tea.Msg {
		res, err := analyzer.Analyze(ctx, targetPath, m.progressChan, m.dirCache, m.oneFileSystem, m.errorsPtr)
		if err != nil {
			return err
		}
		return analyzeMsg{path: targetPath, result: res}
	}
}

func (m model) waitForProgress(sub chan string) tea.Cmd {
	return func() tea.Msg {
		path, ok := <-sub
		if !ok {
			return nil
		}
		return progressMsg(path)
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Calculate dynamic dimensions
		rightWidth := 40
		leftWidth := m.width - rightWidth - 6
		if leftWidth < 20 {
			leftWidth = 20
		}

		// Overhead: Header(3), Path(2), Footer(2), Padding(2) = 9
		listHeight := m.height - 10
		if listHeight < 5 {
			listHeight = 5
		}

		m.list.SetSize(leftWidth, listHeight)
		return m, nil

	case tea.KeyMsg:
		// Global keys
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}

		if m.loading {
			switch msg.String() {
			case "esc":
				if m.cancel != nil {
					m.cancel()
				}
				if len(m.history) > 0 {
					// Return to previous directory
					prev := m.history[len(m.history)-1]
					m.history = m.history[:len(m.history)-1]
					m.path = prev
					if cached, ok := m.dirCache[prev]; ok {
						m.setItems(cached)
						m.loading = false
						return m, nil
					}
					// Re-scan previous if not cached
					m.loading = true
					m.scannedPaths = nil
					m.progressChan = make(chan string, 100)
					return m, tea.Batch(m.startScan(m.path), m.waitForProgress(m.progressChan))
				}
				// Initial scan with no history - do nothing
			}
			return m, nil
		}

		// Not loading - browsing state
		if m.list.FilterState() == list.Filtering {
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "up", "k", "down", "j":
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			return m, cmd

		case "r":
			m.loading = true
			m.scannedPaths = nil
			m.progressChan = make(chan string, 100)
			m.invalidateCache(m.path)
			return m, tea.Batch(m.startScan(m.path), m.waitForProgress(m.progressChan))

		case "enter":
			selectedItem := m.list.SelectedItem()
			if selectedItem != nil {
				selected := selectedItem.(item).FileInfo
				if selected.IsDir && selected.Name != "." {
					m.history = append(m.history, m.path)
					newPath := selected.Path
					if cached, ok := m.dirCache[newPath]; ok {
						m.path = newPath
						m.setItems(cached)
						return m, nil
					}
					m.path = newPath
					m.setItems(analyzer.Result{})
					m.loading = true
					m.scannedPaths = nil
					m.progressChan = make(chan string, 100)
					return m, tea.Batch(m.startScan(m.path), m.waitForProgress(m.progressChan))
				}
			}

		case "backspace":
			// Go Up (Parent)
			parent := filepath.Dir(m.path)
			if parent != m.path {
				m.history = append(m.history, m.path)
				if cached, ok := m.dirCache[parent]; ok {
					m.path = parent
					m.setItems(cached)
					return m, nil
				}
				m.path = parent
				m.setItems(analyzer.Result{})
				m.loading = true
				m.scannedPaths = nil
				m.progressChan = make(chan string, 100)
				return m, tea.Batch(m.startScan(m.path), m.waitForProgress(m.progressChan))
			}

		case "esc":
			return m, nil // Explicitly ignore esc to prevent list component from quitting
		}

	case progressMsg:
		if m.loading {
			m.scannedPaths = append(m.scannedPaths, string(msg))
			maxItems := m.height - 7
			if maxItems < 1 {
				maxItems = 1
			}
			if len(m.scannedPaths) > maxItems {
				m.scannedPaths = m.scannedPaths[len(m.scannedPaths)-maxItems:]
			}
			return m, m.waitForProgress(m.progressChan)
		}

	case analyzeMsg:
		if msg.path == m.path {
			m.loading = false
			m.scannedPaths = nil
			m.setItems(msg.result)
			m.dirCache[m.path] = msg.result
			return m, nil
		}

	case error:
		if msg == context.Canceled {
			return m, nil // Ignore cancellation errors
		}
		m.err = msg
		m.loading = false
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *model) invalidateCache(targetPath string) {
	for cachedPath := range m.dirCache {
		if isDescendantOrEqual(targetPath, cachedPath) || isDescendantOrEqual(cachedPath, targetPath) {
			delete(m.dirCache, cachedPath)
		}
	}
}

func isDescendantOrEqual(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (m *model) setItems(res analyzer.Result) {
	m.err = nil
	m.errorsCount = res.ErrorsCount
	files := res.Files
	sort.Slice(files, func(i, j int) bool {
		return files[i].Size > files[j].Size
	})

	var items []list.Item

	// Inject current directory item if we have data
	if res.TotalSize > 0 || len(res.Breakdown) > 0 {
		items = append(items, item{analyzer.FileInfo{
			Name:        ".",
			Path:        m.path,
			Size:        res.TotalSize,
			IsDir:       true,
			Breakdown:   res.Breakdown,
			ErrorsCount: res.ErrorsCount,
		}})
	}

	for _, f := range files {
		items = append(items, item{f})
	}
	m.list.SetItems(items)
	m.list.ResetSelected()
	m.list.ResetFilter()
}

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v", m.err)
	}

	header := titleStyle.Render("duex - Disk Usage Explorer")
	headerHeight := lipgloss.Height(header)

	var body string
	var footer string

	if m.loading {
		var s strings.Builder
		breadcrumbWidth := m.width - 6
		if breadcrumbWidth < 20 {
			breadcrumbWidth = 20
		}
		s.WriteString(fmt.Sprintf("%s %s\n\n", renderBreadcrumb(m.path, breadcrumbWidth), m.spinner.View()))
		for _, p := range m.scannedPaths {
			s.WriteString(faintStyle.Render("  " + truncate(p, m.width-4)) + "\n")
		}

		var footerBuilder strings.Builder
		errs := atomic.LoadInt64(m.errorsPtr)
		if errs > 0 {
			warningStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#D1A100")).
				Bold(true)
			footerBuilder.WriteString("\n" + warningStyle.Render(fmt.Sprintf("⚠️  Warning: %d directories/files skipped so far.", errs)) + "\n")
		}
		footerBuilder.WriteString("\n" + m.help.ShortHelpView(keys.ScanningHelp(len(m.history) > 0)))
		footer = footerBuilder.String()

		footerHeight := lipgloss.Height(footer)
		bodyHeight := m.height - headerHeight - footerHeight
		if bodyHeight < 1 {
			bodyHeight = 1
		}
		body = lipgloss.NewStyle().Height(bodyHeight).Render(s.String())
	} else {
		leftWidth := m.width - 40 - 6
		if leftWidth < 20 {
			leftWidth = 20
		}
		var leftPane strings.Builder
		leftPane.WriteString(renderBreadcrumb(m.path, leftWidth) + "\n\n")
		leftPane.WriteString(m.list.View())

		var rightPane strings.Builder
		selectedItem := m.list.SelectedItem()
		if selectedItem != nil {
			selected := selectedItem.(item).FileInfo
			rightPane.WriteString(lipgloss.NewStyle().Bold(true).Render("Details") + "\n\n")
			rightPane.WriteString(fmt.Sprintf("Name: %s\n", selected.Name))
			rightPane.WriteString(fmt.Sprintf("Size: %s\n", formatSize(selected.Size)))
			rightPane.WriteString(fmt.Sprintf("Type: %s\n", getType(selected)))

			if selected.IsDir {
				availableHeight := m.list.Height() - 7
				if availableHeight > 0 {
					rightPane.WriteString("\n" + lipgloss.NewStyle().Bold(true).Render("Breakdown") + "\n")
					for i, b := range selected.Breakdown {
						if i >= availableHeight-1 && i < len(selected.Breakdown)-1 {
							rightPane.WriteString("  ...\n")
							break
						}
						rightPane.WriteString(fmt.Sprintf("  %-10s %s\n", b.Extension, formatSize(b.Size)))
					}
				}
			}
		}

		var footerBuilder strings.Builder
		if m.errorsCount > 0 {
			warningStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#D1A100")).
				Bold(true)
			footerBuilder.WriteString("\n" + warningStyle.Render(fmt.Sprintf("⚠️  Warning: %d files/directories were skipped due to permission errors.", m.errorsCount)))
		}
		footerBuilder.WriteString("\n" + m.help.ShortHelpView(keys.BrowsingHelp()))
		footer = footerBuilder.String()

		footerHeight := lipgloss.Height(footer)
		bodyHeight := m.height - headerHeight - footerHeight
		if bodyHeight < 1 {
			bodyHeight = 1
		}

		mainContent := lipgloss.JoinHorizontal(
			lipgloss.Top,
			leftPane.String(),
			detailStyle.Height(bodyHeight).Render(rightPane.String()),
		)
		body = lipgloss.NewStyle().Height(bodyHeight).Render(mainContent)
	}

	return containerStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, body, footer))
}

func truncate(s string, max int) string {
	if max < 4 {
		return "..."
	}
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}

// renderBreadcrumb renders m.path as a styled breadcrumb trail that fits within
// width columns of plain-text. The home directory prefix is replaced with ~.
// Non-current segments are muted; the current (final) segment is bold purple.
// When the trail is too wide, leftmost segments are replaced with a … token.
func renderBreadcrumb(path string, width int) string {
	// Collapse home directory.
	if home, err := os.UserHomeDir(); err == nil {
		if strings.HasPrefix(path, home) {
			path = "~" + path[len(home):]
		}
	}

	// Split into segments, filtering empty strings caused by leading separator.
	raw := strings.Split(path, string(filepath.Separator))
	var segments []string
	for _, s := range raw {
		if s != "" {
			segments = append(segments, s)
		}
	}
	// Root "/" produces no segments after filtering; represent it as "/".
	if len(segments) == 0 {
		segments = []string{"/"}
	}

	// Separator costs 3 plain-text chars (" / ").
	const sepLen = 3

	// Measure total plain-text width of all segments joined.
	totalLen := func(segs []string) int {
		n := 0
		for i, s := range segs {
			n += len(s)
			if i < len(segs)-1 {
				n += sepLen
			}
		}
		return n
	}

	// Left-truncate: while the total plain-text width exceeds the available space,
	// drop the oldest data segment. Once we've started truncating, ensure the
	// ellipsis placeholder sits at position 0 (added once, never duplicated).
	for len(segments) > 1 && totalLen(segments) > width {
		if segments[0] == "…" {
			// Already have ellipsis — only drop the next segment if there are
			// still more segments after it (i.e. the final segment is preserved).
			if len(segments) <= 2 {
				break
			}
			segments = append(segments[:1], segments[2:]...)
		} else {
			// First truncation — replace the leftmost segment with ellipsis.
			segments = append([]string{"…"}, segments[1:]...)
		}
	}

	// Render styled output.
	var parts []string
	for i, seg := range segments {
		if i == len(segments)-1 {
			parts = append(parts, crumbActive.Render(seg))
		} else {
			parts = append(parts, crumbStyle.Render(seg))
		}
	}
	return strings.Join(parts, crumbSep)
}


func getType(f analyzer.FileInfo) string {
	if f.IsDir {
		return "Directory"
	}
	ext := filepath.Ext(f.Name)
	if ext == "" || len(ext) > 15 {
		return "File"
	}
	return strings.ToUpper(ext[1:]) + " File"
}

var Version = "dev"

func formatSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func showHelp() {
	showHelpWriter(os.Stdout)
}

func showHelpWriter(w io.Writer) {
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FAFAFA")).
		Background(lipgloss.Color("#7D56F4")).
		Padding(0, 1)

	sectionStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7D56F4"))

	flagStyle := lipgloss.NewStyle()

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888"))

	printLine := func(name, desc string) {
		padding := 20 - len(name)
		if padding < 0 {
			padding = 0
		}
		fmt.Fprintf(w, "  %s%s %s\n", flagStyle.Render(name), strings.Repeat(" ", padding), descStyle.Render(desc))
	}

	fmt.Fprintln(w, headerStyle.Render("duex - Disk Usage Explorer"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, sectionStyle.Render("Usage:"))
	fmt.Fprintln(w, "  duex [flags] [path]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, sectionStyle.Render("Flags:"))
	printLine("-h, --help", "Show this help message")
	printLine("-v, --version", "Show application version")
	printLine("-c, --cross-mounts", "Allow crossing filesystem boundaries")
	fmt.Fprintln(w)
	fmt.Fprintln(w, sectionStyle.Render("Arguments:"))
	printLine("path", "The directory to scan (defaults to current directory)")
}

func parseFlags(output io.Writer, args []string) (path string, oneFileSystem bool, showHelp bool, showVersion bool, err error) {
	fs := flag.NewFlagSet("duex", flag.ContinueOnError)
	fs.SetOutput(output)

	var crossMounts, c bool
	var version, v bool
	var help, h bool

	fs.BoolVar(&crossMounts, "cross-mounts", false, "Allow crossing filesystem boundaries")
	fs.BoolVar(&c, "c", false, "Allow crossing filesystem boundaries (alias)")
	fs.BoolVar(&version, "version", false, "Show application version")
	fs.BoolVar(&v, "v", false, "Show application version (alias)")
	fs.BoolVar(&help, "help", false, "Show this help message")
	fs.BoolVar(&h, "h", false, "Show help message (alias)")

	fs.Usage = func() {
		showHelpWriter(output)
	}

	err = fs.Parse(args)
	if err != nil {
		return "", false, false, false, err
	}

	showHelp = help || h
	showVersion = version || v
	oneFileSystem = !(crossMounts || c)

	parsedArgs := fs.Args()
	if len(parsedArgs) > 1 {
		return "", false, false, false, fmt.Errorf("too many arguments provided")
	}

	path = "."
	if len(parsedArgs) == 1 {
		path = parsedArgs[0]
	}

	return path, oneFileSystem, showHelp, showVersion, nil
}

func main() {
	path, oneFileSystem, showHelpFlag, showVersionFlag, err := parseFlags(os.Stderr, os.Args[1:])
	if err != nil {
		showHelpWriter(os.Stderr)
		os.Exit(1)
	}

	if showHelpFlag {
		showHelp()
		return
	}

	if showVersionFlag {
		fmt.Printf("duex version %s\n", Version)
		return
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	m := initialModel(absPath)
	m.oneFileSystem = oneFileSystem
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v\n", err)
		os.Exit(1)
	}
}
