package main

import cmdarms "github.com/wangxiuwen/tssh/internal/cmd/arms"

// cmdArms — delegate to internal/cmd/arms (Phase 2 arms-group extraction).
func cmdArms(args []string) { cmdarms.Arms(appRuntime, args) }
