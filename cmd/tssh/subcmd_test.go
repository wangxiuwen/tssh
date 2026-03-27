package main

import (
	"bytes"
	"os"
	"testing"
)

func TestCmdGroup_Dispatch_MatchesName(t *testing.T) {
	called := ""
	g := CmdGroup{
		Name: "test",
		Commands: []SubCmd{
			{Name: "foo", Run: func(args []string) { called = "foo" }},
			{Name: "bar", Run: func(args []string) { called = "bar" }},
		},
	}
	g.Dispatch([]string{"bar"})
	if called != "bar" {
		t.Errorf("expected bar, got %s", called)
	}
}

func TestCmdGroup_Dispatch_MatchesAlias(t *testing.T) {
	called := ""
	g := CmdGroup{
		Name: "test",
		Commands: []SubCmd{
			{Name: "list", Aliases: []string{"ls"}, Run: func(args []string) { called = "list" }},
		},
	}
	g.Dispatch([]string{"ls"})
	if called != "list" {
		t.Errorf("expected list, got %s", called)
	}
}

func TestCmdGroup_Dispatch_Default(t *testing.T) {
	called := false
	g := CmdGroup{
		Name:    "test",
		Default: func(args []string) { called = true },
	}
	g.Dispatch(nil)
	if !called {
		t.Error("default should be called")
	}
}

func TestCmdGroup_Dispatch_PassesArgs(t *testing.T) {
	var got []string
	g := CmdGroup{
		Name: "test",
		Commands: []SubCmd{
			{Name: "run", Run: func(args []string) { got = args }},
		},
	}
	g.Dispatch([]string{"run", "-j", "hello"})
	if len(got) != 2 || got[0] != "-j" || got[1] != "hello" {
		t.Errorf("expected [-j hello], got %v", got)
	}
}

func TestCmdGroup_PrintHelp(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	g := CmdGroup{
		Name: "mygrp",
		Desc: "测试描述",
		Commands: []SubCmd{
			{Name: "foo", Desc: "做 foo 的事"},
			{Name: "bar", Aliases: []string{"b"}, Desc: "做 bar 的事"},
		},
	}
	g.PrintHelp()

	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	out := buf.String()

	if !bytes.Contains([]byte(out), []byte("mygrp")) {
		t.Errorf("help should contain group name, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("bar, b")) {
		t.Errorf("help should contain alias, got: %s", out)
	}
}

func TestContains(t *testing.T) {
	if !contains([]string{"a", "b"}, "b") {
		t.Error("should contain b")
	}
	if contains([]string{"a", "b"}, "c") {
		t.Error("should not contain c")
	}
	if contains(nil, "a") {
		t.Error("nil should not contain anything")
	}
}
