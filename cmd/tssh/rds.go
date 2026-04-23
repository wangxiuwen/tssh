package main

import cmddb "github.com/wangxiuwen/tssh/internal/cmd/db"

// cmdRDS — delegate to internal/cmd/db (Phase 2 db-group extraction).
func cmdRDS(args []string) { cmddb.RDS(appRuntime, args) }
