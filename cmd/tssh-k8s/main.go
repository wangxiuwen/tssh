// Command tssh-k8s is the k8s-only slice of the tssh toolkit. Ships ks, kf,
// logs and events only — no ECS management, no ARMS, no browser/vpn bits.
//
//	tssh-k8s ks     <jump> <svc>  [-n ns] [-j]
//	tssh-k8s kf     <jump> <svc:port>[=<local>] ... [--browser] [-j]
//	tssh-k8s logs   <jump> <svc>  [-n ns] [-l sel] [-f]
//	tssh-k8s events <jump>        [-n ns] [--svc x] [--level Warning] [-w]
//
// Same source as the full tssh: internal/cmd/k8s is linked into both.
package main

import (
	"fmt"
	"os"

	"github.com/wangxiuwen/tssh/internal/cmd/k8s"
	"github.com/wangxiuwen/tssh/internal/runtime"
)

func main() {
	profile := ""
	args := os.Args[1:]

	// --profile is handled here (top-level Aliyun-account concern). Only long
	// form — `-p` collides with subcommand port flags (socks/shell/fwd/browser
	// all use `-p` for local port in the net/db groups).
	var rest []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--profile" && i+1 < len(args) {
			profile = args[i+1]
			i++
			continue
		}
		rest = append(rest, args[i])
	}

	if len(rest) == 0 {
		usage()
		os.Exit(1)
	}

	rt := runtime.New(profile)

	switch rest[0] {
	case "ks":
		k8s.KS(rt, rest[1:])
	case "kf":
		k8s.KF(rt, rest[1:])
	case "logs":
		k8s.Logs(rt, rest[1:])
	case "events":
		k8s.Events(rt, rest[1:])
	case "-h", "--help", "help":
		usage()
	case "-v", "--version", "version":
		fmt.Println("tssh-k8s", version)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", rest[0])
		usage()
		os.Exit(1)
	}
}

const version = "1.17.0"

func usage() {
	fmt.Printf(`tssh-k8s %s — k8s-only slice of tssh

Usage:
  tssh-k8s [--profile <name>] <subcommand> [args...]

Subcommands:
  ks     <jump> <svc>  [-n ns] [-j]                      service 健康诊断
  kf     <jump> <svc:port>[=<local>] ... [--browser]     一次多 svc port-forward
  logs   <jump> <svc>  [-n ns] [-l sel] [-f]             多 pod 日志流聚合
  events <jump>        [-n ns] [--svc x] [--level Warning] [-w]  k8s 事件查看

本 binary 是 slim runtime. kf 需要的 port-forward 底层在 Phase 4 会搬到
共享包; 在那之前如果你用 kf, 请用主 tssh 二进制.
`, version)
}
