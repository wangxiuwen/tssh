// Command tssh-arms is the observability slice of the tssh toolkit —
// Aliyun ARMS alerts + Grafana dashboards + Prometheus queries + trace lookup.
//
//	tssh-arms [--profile <name>] <subcommand> [args...]
//
// Same source as the full tssh: internal/cmd/arms is linked into both.
package main

import (
	"fmt"
	"os"

	cmdarms "github.com/wangxiuwen/tssh/internal/cmd/arms"
	"github.com/wangxiuwen/tssh/internal/runtime"
)

const version = "1.17.0"

func main() {
	profile := ""
	args := os.Args[1:]

	// --profile long-form only; -p reserved for subcommands that use it as port.
	var rest []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--profile" && i+1 < len(args) {
			profile = args[i+1]
			i++
			continue
		}
		rest = append(rest, args[i])
	}

	if len(rest) == 1 && (rest[0] == "-v" || rest[0] == "--version" || rest[0] == "version") {
		fmt.Println("tssh-arms", version)
		return
	}

	rt := runtime.New(profile)
	cmdarms.Arms(rt, rest)
}
