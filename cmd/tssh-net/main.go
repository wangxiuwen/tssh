// Command tssh-net is the network-access slice of the tssh toolkit — everything
// devs need to reach VPC-private services from a laptop without SSH keys.
//
//	tssh-net socks   <name>                     SOCKS5 proxy
//	tssh-net fwd     <target>                   single port-forward
//	tssh-net run     --to k=v,k=v -- <cmd>      multi-port + env injection
//	tssh-net shell   <name>                     subshell with SOCKS5 env preset
//	tssh-net vpn     <name> --cidr ...          L3 TUN transparent proxy
//	tssh-net browser <name> [url...]            dedicated Chrome through SOCKS5
//
// Same source as the full tssh: internal/cmd/net is linked into both.
package main

import (
	"fmt"
	"os"

	cmdnet "github.com/wangxiuwen/tssh/internal/cmd/net"
	"github.com/wangxiuwen/tssh/internal/runtime"
)

const version = "1.17.0"

func main() {
	profile := ""
	args := os.Args[1:]

	// --profile is long-form only. `-p` is used by socks/shell/fwd/browser
	// as local-port flag, so stripping it here as profile alias would collide.
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
	case "socks":
		cmdnet.Socks(rt, rest[1:])
	case "fwd":
		cmdnet.Fwd(rt, rest[1:])
	case "run":
		cmdnet.Run(rt, rest[1:])
	case "shell":
		cmdnet.Shell(rt, rest[1:])
	case "vpn":
		cmdnet.VPN(rt, rest[1:])
	case "browser":
		cmdnet.Browser(rt, rest[1:])
	case "-h", "--help", "help":
		usage()
	case "-v", "--version", "version":
		fmt.Println("tssh-net", version)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", rest[0])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Printf(`tssh-net %s — network-access slice of tssh

Usage:
  tssh-net [--profile <name>] <subcommand> [args...]

Subcommands:
  socks   <name>                                SOCKS5 proxy
  fwd     <host:port|rm-xxx|r-xxx>              zero-config single port-forward
  run     --to k=v,k=v -- <cmd>                 multi-port + env injection
  shell   <name>                                subshell with preset SOCKS5 env
  vpn     <name> --cidr 10.0.0.0/16             L3 TUN transparent proxy (root)
  browser <name> [url...]                       Chrome window through SOCKS5

All subcommands share the same CLI as the main tssh binary.
`, version)
}
