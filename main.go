package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dude/pkg/analyzer"

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

	detailStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(0, 2).
			MarginLeft(2).
			Width(40)

	containerStyle = lipgloss.NewStyle().
			Padding(1, 2)
)

type model struct {
	path           string
	files          []analyzer.FileInfo
	cursor         int
	selected       map[int]struct{}
	err            error
	loading        bool
	width          int
	height         int
	dirCache       map[string][]analyzer.FileInfo
	breakdownCache map[string][]analyzer.Breakdown
	spinner        spinner.Model
}

type analyzeMsg struct {
	path  string
	files []analyzer.FileInfo
}

type breakdownMsg struct {
	path      string
	breakdown []analyzer.Breakdown
}

func initialModel(path string) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))

	return model{
		path:           path,
		selected:       make(map[int]struct{}),
		loading:        true,
		dirCache:       make(map[string][]analyzer.FileInfo),
		breakdownCache: make(map[string][]analyzer.Breakdown),
		spinner:        s,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.analyze(m.path))
}

func (m model) analyze(targetPath string) tea.Cmd {
	return func() tea.Msg {
		res, err := analyzer.Analyze(targetPath)
		if err != nil {
			return err
		}
		return analyzeMsg{path: targetPath, files: res.Files}
	}
}

func (m model) triggerBreakdown() tea.Cmd {
	if len(m.files) == 0 {
		return nil
	}
	selected := m.files[m.cursor]
	if selected.IsDir {
		if _, ok := m.breakdownCache[selected.Path]; !ok {
			return func() tea.Msg {
				return breakdownMsg{path: selected.Path, breakdown: analyzer.GetBreakdown(selected.Path)}
			}
		}
	}
	return nil
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

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				return m, m.triggerBreakdown()
			}

		case "down", "j":
			if m.cursor < len(m.files)-1 {
				m.cursor++
				return m, m.triggerBreakdown()
			}

		case "r":
			m.loading = true
			delete(m.dirCache, m.path)
			// Also clear breakdowns for children to ensure a deep refresh if requested
			for k := range m.breakdownCache {
				if strings.HasPrefix(k, m.path) {
					delete(m.breakdownCache, k)
				}
			}
			return m, m.analyze(m.path)

		case "enter":
			if len(m.files) > 0 && m.files[m.cursor].IsDir {
				newPath := m.files[m.cursor].Path
				if cached, ok := m.dirCache[newPath]; ok {
					m.path = newPath
					m.files = cached
					m.cursor = 0
					return m, m.triggerBreakdown()
				}
				m.path = newPath
				m.files = nil
				m.cursor = 0
				m.loading = true
				return m, m.analyze(m.path)
			}

		case "backspace", "esc":
			parent := filepath.Dir(m.path)
			if parent != m.path {
				if cached, ok := m.dirCache[parent]; ok {
					m.path = parent
					m.files = cached
					m.cursor = 0
					return m, m.triggerBreakdown()
				}
				m.path = parent
				m.files = nil
				m.cursor = 0
				m.loading = true
				return m, m.analyze(m.path)
			}
		}

	case analyzeMsg:
		if msg.path == m.path {
			m.loading = false
			m.files = msg.files
			sort.Slice(m.files, func(i, j int) bool {
				return m.files[i].Size > m.files[j].Size
			})
			m.dirCache[m.path] = m.files
			return m, m.triggerBreakdown()
		}

	case breakdownMsg:
		m.breakdownCache[msg.path] = msg.breakdown

	case error:
		m.err = msg
		m.loading = false
	}

	return m, nil
}

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v", m.err)
	}

	if m.loading {
		return fmt.Sprintf("\n  %s Scanning directory...", m.spinner.View())
	}

	var leftPane strings.Builder
	leftPane.WriteString(titleStyle.Render("dude - Disk Usage Explorer"))
	leftPane.WriteString("\n\n")
	leftPane.WriteString(fmt.Sprintf("Path: %s\n\n", m.path))

	for i, file := range m.files {
		cursor := " "
		style := itemStyle
		if m.cursor == i {
			cursor = ">"
			style = selectedItemStyle
		}

		name := file.Name
		if file.IsDir {
			name += "/"
		}

		sizeStr := formatSize(file.Size)
		line := fmt.Sprintf("%s %-30s %s", cursor, truncate(name, 30), sizeStyle.Render(sizeStr))
		leftPane.WriteString(style.Render(line) + "\n")
	}

	leftPane.WriteString("\n (q: quit, r: refresh, enter: open, backspace: up)\n")

	var rightPane strings.Builder
	if len(m.files) > 0 {
		selected := m.files[m.cursor]
		rightPane.WriteString(lipgloss.NewStyle().Bold(true).Render("Details") + "\n\n")
		rightPane.WriteString(fmt.Sprintf("Name: %s\n", selected.Name))
		rightPane.WriteString(fmt.Sprintf("Size: %s\n", formatSize(selected.Size)))
		rightPane.WriteString(fmt.Sprintf("Type: %s\n", getType(selected)))

		if selected.IsDir {
			rightPane.WriteString("\n" + lipgloss.NewStyle().Bold(true).Render("Breakdown") + "\n")
			if breakdown, ok := m.breakdownCache[selected.Path]; ok {
				for i, b := range breakdown {
					if i > 10 {
						rightPane.WriteString("  ...\n")
						break
					}
					rightPane.WriteString(fmt.Sprintf("  %-10s %s\n", b.Extension, formatSize(b.Size)))
				}
			} else {
				rightPane.WriteString(fmt.Sprintf("  %s Scanning...\n", m.spinner.View()))
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
	if ext == "" {
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
