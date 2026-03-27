package main

import (
	"fmt"
	"os"
	"strings"
)

// SubCmd defines a subcommand in a command group.
type SubCmd struct {
	Name    string            // primary name
	Aliases []string          // alternative names (e.g. "ls" for "list")
	Desc    string            // one-line description for help
	Run     func(args []string) // handler
}

// CmdGroup is a table-driven subcommand dispatcher.
type CmdGroup struct {
	Name     string   // parent command name (e.g. "arms")
	Desc     string   // short description
	Default  func(args []string) // handler when no subcommand given (nil = show help)
	Commands []SubCmd
}

// Dispatch routes args to the matching subcommand.
func (g *CmdGroup) Dispatch(args []string) {
	if len(args) == 0 {
		if g.Default != nil {
			g.Default(nil)
		} else {
			g.PrintHelp()
			os.Exit(1)
		}
		return
	}

	name := args[0]

	// help flags
	if name == "help" || name == "-h" || name == "--help" {
		g.PrintHelp()
		return
	}

	for _, cmd := range g.Commands {
		if cmd.Name == name || contains(cmd.Aliases, name) {
			cmd.Run(args[1:])
			return
		}
	}

	fmt.Fprintf(os.Stderr, "未知子命令: %s %s\n\n", g.Name, name)
	g.PrintHelp()
	os.Exit(1)
}

// PrintHelp prints the command group usage.
func (g *CmdGroup) PrintHelp() {
	fmt.Fprintf(os.Stderr, "用法: tssh %s <子命令>\n", g.Name)
	if g.Desc != "" {
		fmt.Fprintf(os.Stderr, "  %s\n", g.Desc)
	}
	fmt.Fprintln(os.Stderr, "\n子命令:")
	maxLen := 0
	for _, cmd := range g.Commands {
		n := cmd.Name
		if len(cmd.Aliases) > 0 {
			n += ", " + strings.Join(cmd.Aliases, ", ")
		}
		if len(n) > maxLen {
			maxLen = len(n)
		}
	}
	for _, cmd := range g.Commands {
		n := cmd.Name
		if len(cmd.Aliases) > 0 {
			n += ", " + strings.Join(cmd.Aliases, ", ")
		}
		fmt.Fprintf(os.Stderr, "  %-*s  %s\n", maxLen, n, cmd.Desc)
	}
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
