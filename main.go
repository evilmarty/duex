package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dude/pkg/analyzer"

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
			Padding(0, 1)

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
			Padding(1, 2)
)

type item struct {
	analyzer.FileInfo
}

func (i item) Title() string       { return i.Name }
func (i item) Description() string { return formatSize(i.Size) }
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
	line := fmt.Sprintf("%s %-30s %s", cursor, truncate(str, 30), sizeStyle.Render(sizeStr))
	fmt.Fprint(w, style.Render(line))
}

type model struct {
	path         string
	selected     map[int]struct{}
	err          error
	loading      bool
	width        int
	height       int
	dirCache     map[string][]analyzer.FileInfo
	spinner      spinner.Model
	scannedPaths []string
	progressChan chan string
	list         list.Model
	cancel       context.CancelFunc
	history      []string
}



type analyzeMsg struct {
	path  string
	files []analyzer.FileInfo
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
		path:         path,
		selected:     make(map[int]struct{}),
		loading:      true,
		dirCache:     make(map[string][]analyzer.FileInfo),
		spinner:      s,
		list:         l,
	}
}

func (m model) Init() tea.Cmd {
	m.progressChan = make(chan string, 100)
	return tea.Batch(
		m.spinner.Tick,
		m.startScan(m.path),
		m.waitForProgress(m.progressChan),
	)
}

func (m *model) startScan(targetPath string) tea.Cmd {
	if m.cancel != nil {
		m.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	return func() tea.Msg {
		res, err := analyzer.Analyze(ctx, targetPath, m.progressChan)
		if err != nil {
			return err
		}
		return analyzeMsg{path: targetPath, files: res.Files}
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
		m.list.SetSize(msg.Width-40, msg.Height-10)
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
			delete(m.dirCache, m.path)
			return m, tea.Batch(m.startScan(m.path), m.waitForProgress(m.progressChan))

		case "enter":
			selectedItem := m.list.SelectedItem()
			if selectedItem != nil {
				selected := selectedItem.(item).FileInfo
				if selected.IsDir {
					m.history = append(m.history, m.path)
					newPath := selected.Path
					if cached, ok := m.dirCache[newPath]; ok {
						m.path = newPath
						m.setItems(cached)
						return m, nil
					}
					m.path = newPath
					m.setItems(nil)
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
				m.setItems(nil)
				m.loading = true
				m.scannedPaths = nil
				m.progressChan = make(chan string, 100)
				return m, tea.Batch(m.startScan(m.path), m.waitForProgress(m.progressChan))
			}
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
			m.setItems(msg.files)
			m.dirCache[m.path] = msg.files
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

func (m *model) setItems(files []analyzer.FileInfo) {
	sort.Slice(files, func(i, j int) bool {
		return files[i].Size > files[j].Size
	})

	var items []list.Item
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

	if m.loading {
		var s strings.Builder
		s.WriteString(fmt.Sprintf("\n  %s Scanning directory...\n\n", m.spinner.View()))
		for _, p := range m.scannedPaths {
			s.WriteString(faintStyle.Render("  " + truncate(p, m.width-4)) + "\n")
		}
		s.WriteString("\n (q: quit, esc: cancel)\n")
		return s.String()
	}

	var leftPane strings.Builder
	leftPane.WriteString(titleStyle.Render("dude - Disk Usage Explorer"))
	leftPane.WriteString("\n\n")
	leftPane.WriteString(fmt.Sprintf("Path: %s\n\n", m.path))
	leftPane.WriteString(m.list.View())
	leftPane.WriteString("\n (q: quit, r: refresh, enter: open, backspace: up, /: filter)\n")

	var rightPane strings.Builder
	selectedItem := m.list.SelectedItem()
	if selectedItem != nil {
		selected := selectedItem.(item).FileInfo
		rightPane.WriteString(lipgloss.NewStyle().Bold(true).Render("Details") + "\n\n")
		rightPane.WriteString(fmt.Sprintf("Name: %s\n", selected.Name))
		rightPane.WriteString(fmt.Sprintf("Size: %s\n", formatSize(selected.Size)))
		rightPane.WriteString(fmt.Sprintf("Type: %s\n", getType(selected)))

		if selected.IsDir {
			rightPane.WriteString("\n" + lipgloss.NewStyle().Bold(true).Render("Breakdown") + "\n")
			for i, b := range selected.Breakdown {
				if i > 10 {
					rightPane.WriteString("  ...\n")
					break
				}
				rightPane.WriteString(fmt.Sprintf("  %-10s %s\n", b.Extension, formatSize(b.Size)))
			}
		}
	}

	content := lipgloss.JoinHorizontal(lipgloss.Top, leftPane.String(), detailStyle.Render(rightPane.String()))
	return containerStyle.Render(content)
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
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

func main() {
	path := "."
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(initialModel(absPath))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v\n", err)
		os.Exit(1)
	}
}
