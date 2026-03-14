package main

import (
	"fmt"
	"strings"

	"github.com/manifoldco/promptui"
)

// FuzzySelect provides a built-in interactive fuzzy search selector using promptui
func FuzzySelect(instances []Instance, initialQuery string) (*Instance, error) {
	if len(instances) == 0 {
		return nil, fmt.Errorf("no instances found")
	}

	// Calculate max widths for alignment
	maxNameLen := 4
	for _, inst := range instances {
		if len(inst.Name) > maxNameLen && len(inst.Name) < 50 {
			maxNameLen = len(inst.Name)
		}
	}

	// Prepare data for the prompt
	type item struct {
		Index     int
		Name      string
		Status    string
		PublicIP  string
		PrivateIP string
		ID        string
	}

	var items []item
	for i, inst := range instances {
		pip := inst.PublicIP
		if pip == "" {
			pip = inst.EIP
		}
		if pip == "" {
			pip = "-"
		}
		status := "✅"
		if inst.Status != "Running" {
			status = "⛔"
		}
		items = append(items, item{
			Index:     i + 1,
			Name:      inst.Name,
			Status:    status,
			PublicIP:  pip,
			PrivateIP: inst.PrivateIP,
			ID:        inst.ID,
		})
	}

	templates := &promptui.SelectTemplates{
		Label:    "{{ . }}",
		Active:   fmt.Sprintf("  \033[36m👉\033[0m {{ .Index | cyan | printf \"%%-4v\" }} {{ .Status }} {{ .Name | cyan | printf \"%%-%dv\" }} {{ .PublicIP | cyan | printf \"%%-16v\" }} {{ .PrivateIP | cyan | printf \"%%-16v\" }} {{ .ID | cyan }}\033[0m", maxNameLen),
		Inactive: fmt.Sprintf("     {{ .Index | printf \"%%-4v\" }} {{ .Status }} {{ .Name | printf \"%%-%dv\" }} {{ .PublicIP | printf \"%%-16v\" }} {{ .PrivateIP | printf \"%%-16v\" }} {{ .ID }}", maxNameLen),
		Selected: fmt.Sprintf("  \033[32m✔\033[0m {{ .Name | green }} (\033[36m{{ .PrivateIP }}\033[0m)"),
		Details:  "",
	}

	searcher := func(input string, index int) bool {
		i := items[index]
		name := strings.ToLower(i.Name)
		input = strings.ToLower(input)
		id := strings.ToLower(i.ID)
		
		return strings.Contains(name, input) ||
			strings.Contains(i.PrivateIP, input) ||
			strings.Contains(i.PublicIP, input) ||
			strings.Contains(id, input) ||
			fmt.Sprintf("%d", i.Index) == input
	}

	prompt := promptui.Select{
		Label:     "🔍 搜索 / 选择服务器 (支持名称/IP/编号):",
		Items:     items,
		Templates: templates,
		Size:      15,
		Searcher:  searcher,
	}

	i, _, err := prompt.Run()

	if err != nil {
		fmt.Printf("退出了选择 (%v)\n", err)
		return nil, err
	}

	return &instances[items[i].Index-1], nil
}

// PrintInstanceList prints a formatted table of all instances
func PrintInstanceList(instances []Instance) {
	running := 0
	for _, inst := range instances {
		if inst.Status == "Running" {
			running++
		}
	}

	fmt.Println()
	fmt.Printf("  Aliyun ECS Instances (total: %d | running: %d)\n", len(instances), running)
	fmt.Println("  " + strings.Repeat("═", 99))
	fmt.Printf("  %-4s %-35s %-4s %-16s %-16s %s\n",
		"#", "NAME", "ST", "PUBLIC-IP", "PRIVATE-IP", "INSTANCE-ID")
	fmt.Println("  " + strings.Repeat("─", 99))

	for i, inst := range instances {
		pip := inst.PublicIP
		if pip == "" {
			pip = inst.EIP
		}
		if pip == "" {
			pip = "-"
		}
		status := "✅"
		if inst.Status != "Running" {
			status = "⛔"
		}
		fmt.Printf("  %-4d %-35s %s  %-16s %-16s %s\n",
			i+1, inst.Name, status, pip, inst.PrivateIP, inst.ID)
	}
	fmt.Println()
}
