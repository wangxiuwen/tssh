package main

import cmddb "github.com/wangxiuwen/tssh/internal/cmd/db"

// cmdRedis — delegate to internal/cmd/db (Phase 2 db-group extraction).
func cmdRedis(args []string) { cmddb.Redis(appRuntime, args) }
