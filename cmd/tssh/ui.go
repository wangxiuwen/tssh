package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// getTermWidth returns the terminal width, defaulting to 100
func getTermWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 100
	}
	return w
}

// --- Bubbletea-based Fuzzy Selector ---

type selectorModel struct {
	instances []Instance
	filtered  []int // indices into instances
	query     string
	cursor    int // position in filtered list
	selected  int // final selected index in instances, -1 if none
	width     int
	height    int
	quit      bool
}

func newSelectorModel(instances []Instance, initialQuery string) selectorModel {
	w, h, _ := term.GetSize(int(os.Stdout.Fd()))
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	m := selectorModel{
		instances: instances,
		query:     initialQuery,
		selected:  -1,
		width:     w,
		height:    h,
	}
	m.filter()
	return m
}

func (m *selectorModel) filter() {
	m.filtered = m.filtered[:0]
	query := strings.ToLower(strings.TrimSpace(m.query))
	keywords := strings.Fields(query)

	for i, inst := range m.instances {
		if len(keywords) == 0 {
			m.filtered = append(m.filtered, i)
			continue
		}

		// Always search text (name, IP, ID, and index number)
		searchStr := strings.ToLower(fmt.Sprintf("%d %s %s %s %s %s", i+1, inst.Name, inst.PrivateIP, inst.PublicIP, inst.EIP, inst.ID))
		allMatch := true
		for _, kw := range keywords {
			if !strings.Contains(searchStr, kw) {
				allMatch = false
				break
			}
		}
		if allMatch {
			m.filtered = append(m.filtered, i)
		}
	}

	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m selectorModel) Init() tea.Cmd {
	return nil
}

func (m selectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quit = true
			return m, tea.Quit

		case tea.KeyEnter:
			if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				m.selected = m.filtered[m.cursor]
			}
			return m, tea.Quit

		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil

		case tea.KeyDown:
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			return m, nil

		case tea.KeyBackspace:
			if len(m.query) > 0 {
				m.query = m.query[:len(m.query)-1]
				m.filter()
			}
			return m, nil

		case tea.KeyRunes:
			m.query += string(msg.Runes)
			m.filter()
			return m, nil
		}
	}
	return m, nil
}

func (m selectorModel) View() string {
	var sb strings.Builder

	// Styles
	dimStyle := lipgloss.NewStyle().Faint(true)
	activeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("36")).Bold(true)
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	_ = statusStyle

	// Calculate available lines for list (reserve 3 for header+input+status)
	listHeight := m.height - 3
	if listHeight < 3 {
		listHeight = 3
	}

	// Adaptive columns
	nameW := 28
	showIP := true
	showPubIP := false
	showID := false

	if m.width >= 100 {
		showID = true
		showPubIP = true
	} else if m.width >= 75 {
		showPubIP = true
		nameW = m.width - 50
		if nameW < 18 {
			nameW = 18
		}
	} else {
		nameW = m.width - 28
		if nameW < 12 {
			nameW = 12
		}
	}

	// Calculate scroll window
	start := 0
	end := len(m.filtered)
	if end > listHeight {
		// Keep cursor in view
		half := listHeight / 2
		start = m.cursor - half
		if start < 0 {
			start = 0
		}
		end = start + listHeight
		if end > len(m.filtered) {
			end = len(m.filtered)
			start = end - listHeight
			if start < 0 {
				start = 0
			}
		}
	}

	// Render list items (top section)
	for i := start; i < end; i++ {
		idx := m.filtered[i]
		inst := m.instances[idx]

		name := shortenName(inst.Name, nameW)
		status := "✅"
		if inst.Status != "Running" {
			status = "⛔"
		}

		var line string
		if i == m.cursor {
			// Active row
			prefix := " 👉 "
			if showID {
				line = fmt.Sprintf("%s%-3d %s %-*s %-16s %-16s %s",
					prefix, idx+1, status, nameW, name, inst.PrivateIP, getPublicIP(inst), inst.ID)
			} else if showPubIP || showIP {
				line = fmt.Sprintf("%s%-3d %s %-*s %-16s",
					prefix, idx+1, status, nameW, name, inst.PrivateIP)
			} else {
				line = fmt.Sprintf("%s%-3d %s %s",
					prefix, idx+1, status, name)
			}
			// Truncate to terminal width
			if len(line) > m.width {
				line = line[:m.width-1]
			}
			sb.WriteString(activeStyle.Render(line))
		} else {
			prefix := "    "
			if showID {
				line = fmt.Sprintf("%s%-3d %s %-*s %-16s %-16s %s",
					prefix, idx+1, status, nameW, name, inst.PrivateIP, getPublicIP(inst), inst.ID)
			} else if showPubIP || showIP {
				line = fmt.Sprintf("%s%-3d %s %-*s %-16s",
					prefix, idx+1, status, nameW, name, inst.PrivateIP)
			} else {
				line = fmt.Sprintf("%s%-3d %s %s",
					prefix, idx+1, status, name)
			}
			if len(line) > m.width {
				line = line[:m.width-1]
			}
			sb.WriteString(dimStyle.Render(line))
		}
		sb.WriteString("\n")
	}

	// Pad remaining lines
	rendered := end - start
	for i := rendered; i < listHeight; i++ {
		sb.WriteString("\n")
	}

	// Status line
	statusLine := fmt.Sprintf(" %d/%d 台  ↑↓选择 Enter确认 Esc退出", len(m.filtered), len(m.instances))
	if len(statusLine) > m.width {
		statusLine = statusLine[:m.width]
	}
	sb.WriteString(dimStyle.Render(statusLine))
	sb.WriteString("\n")

	// Input line (bottom)
	inputLine := fmt.Sprintf(" 🔍 %s▌", m.query)
	sb.WriteString(inputLine)

	return sb.String()
}

// FuzzySelect provides an interactive fuzzy search selector using bubbletea
func FuzzySelect(instances []Instance, initialQuery string) (*Instance, error) {
	if len(instances) == 0 {
		return nil, fmt.Errorf("no instances found")
	}

	m := newSelectorModel(instances, initialQuery)
	p := tea.NewProgram(m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		return nil, err
	}

	// bubbletea normally returns our model, but if upstream ever changes
	// the contract (or Run exits before drawing) the bare cast below would
	// panic. Fall back to "cancelled" instead.
	final, ok := result.(selectorModel)
	if !ok || final.quit || final.selected < 0 {
		return nil, fmt.Errorf("cancelled")
	}
	if final.selected >= len(instances) {
		return nil, fmt.Errorf("bad selection index %d/%d", final.selected, len(instances))
	}
	return &instances[final.selected], nil
}

// PrintInstanceList prints a formatted table, adapting to terminal width
func PrintInstanceList(instances []Instance) {
	running := 0
	for _, inst := range instances {
		if inst.Status == "Running" {
			running++
		}
	}

	tw := getTermWidth()

	fmt.Println()
	fmt.Printf("  ECS Instances (%d total, %d running)\n", len(instances), running)

	if tw >= 100 {
		nameW := 28
		sep := strings.Repeat("─", 100)
		fmt.Printf("  %s\n", sep)
		fmt.Printf("  %-4s %-2s %-*s %-16s %-16s %s\n", "#", "ST", nameW, "NAME", "PRIVATE-IP", "PUBLIC-IP", "INSTANCE-ID")
		fmt.Printf("  %s\n", sep)

		for i, inst := range instances {
			fmt.Printf("  %-4d %s %-*s %-16s %-16s %s\n",
				i+1, statusIcon(inst), nameW, shortenName(inst.Name, nameW),
				inst.PrivateIP, getPublicIP(inst), inst.ID)
		}
	} else if tw >= 75 {
		nameW := tw - 50
		if nameW < 18 {
			nameW = 18
		}
		if nameW > 30 {
			nameW = 30
		}
		sep := strings.Repeat("─", tw-2)
		fmt.Printf("  %s\n", sep)
		fmt.Printf("  %-3s %-2s %-*s %-16s %s\n", "#", "ST", nameW, "NAME", "PRIVATE-IP", "PUBLIC-IP")
		fmt.Printf("  %s\n", sep)

		for i, inst := range instances {
			fmt.Printf("  %-3d %s %-*s %-16s %s\n",
				i+1, statusIcon(inst), nameW, shortenName(inst.Name, nameW),
				inst.PrivateIP, getPublicIP(inst))
		}
	} else {
		nameW := tw - 28
		if nameW < 12 {
			nameW = 12
		}
		sep := strings.Repeat("─", tw-2)
		fmt.Printf("  %s\n", sep)
		fmt.Printf("  %-3s %-2s %-*s %s\n", "#", "ST", nameW, "NAME", "PRIVATE-IP")
		fmt.Printf("  %s\n", sep)

		for i, inst := range instances {
			fmt.Printf("  %-3d %s %-*s %s\n",
				i+1, statusIcon(inst), nameW, shortenName(inst.Name, nameW),
				inst.PrivateIP)
		}
	}
	fmt.Println()
}

func getPublicIP(inst Instance) string {
	pip := inst.PublicIP
	if pip == "" {
		pip = inst.EIP
	}
	if pip == "" {
		pip = "-"
	}
	return pip
}

func statusIcon(inst Instance) string {
	if inst.Status == "Running" {
		return "✅"
	}
	return "⛔"
}
