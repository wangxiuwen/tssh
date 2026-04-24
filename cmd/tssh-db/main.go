// Command tssh-db is the database slice of the tssh toolkit — Redis + RDS
// listing, inspection, and direct connect via the built-in RESP / MySQL wire
// clients. Same source as the full tssh: internal/cmd/db is linked into both.
package main

import (
	"fmt"
	"os"

	cmddb "github.com/wangxiuwen/tssh/internal/cmd/db"
	"github.com/wangxiuwen/tssh/internal/runtime"
)

const version = "1.17.0"

func main() {
	profile := ""
	args := os.Args[1:]

	// --profile long-form only; -p reserved for subcommand-level port flags.
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
	case "redis":
		cmddb.Redis(rt, rest[1:])
	case "rds":
		cmddb.RDS(rt, rest[1:])
	case "-h", "--help", "help":
		usage()
	case "-v", "--version", "version":
		fmt.Println("tssh-db", version)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", rest[0])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Printf(`tssh-db %s — database slice of tssh

Usage:
  tssh-db [--profile <name>] <subcommand> [args...]

Subcommands:
  redis ls / info <id>                Redis 实例管理
  redis <name|id>                     直连 Redis (built-in RESP client)
  rds ls / info <id>                  RDS 实例管理
  rds <name|id> [-u<user>]            直连 RDS (built-in MySQL client)
`, version)
}
