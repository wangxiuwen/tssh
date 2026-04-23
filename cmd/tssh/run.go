package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
)

// cmdRun starts multiple port-forwards and execs a child process with
// <NAME>_HOST / <NAME>_PORT / <NAME>_ADDR env vars pointing at 127.0.0.1:<local>.
// Meant for "local Spring needs to reach prod MySQL + Redis + Kafka" case:
// one command up the whole dependency graph instead of N terminals.
//
//	tssh run --to mysql=rm-xxx,redis=r-xxx,kafka=10.0.0.3:9092 -- ./gradlew bootRun
//
// In the child:
//	MYSQL_HOST=127.0.0.1  MYSQL_PORT=54321  MYSQL_ADDR=127.0.0.1:54321
//	REDIS_HOST=127.0.0.1  REDIS_PORT=54322  REDIS_ADDR=127.0.0.1:54322
//	KAFKA_HOST=127.0.0.1  KAFKA_PORT=54323  KAFKA_ADDR=127.0.0.1:54323
func cmdRun(args []string) {
	toSpec := ""
	via := ""
	sepIdx := -1
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--to":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "❌ --to 需要参数 (key=target,key=target,...)")
				os.Exit(2)
			}
			toSpec = args[i+1]
			i += 2
		case "--via":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "❌ --via 需要一个 name")
				os.Exit(2)
			}
			via = args[i+1]
			i += 2
		case "-h", "--help":
			printRunHelp()
			return
		case "--":
			sepIdx = i
			i = len(args)
		default:
			fmt.Fprintf(os.Stderr, "❌ 未知选项: %s (别忘记 `--` 分隔 child command)\n", args[i])
			os.Exit(2)
		}
	}

	if toSpec == "" {
		printRunHelp()
		os.Exit(1)
	}
	if sepIdx < 0 || sepIdx == len(args)-1 {
		fmt.Fprintln(os.Stderr, "❌ 缺少要执行的命令 — 用 `--` 分隔, 例: tssh run --to ... -- ./gradlew bootRun")
		os.Exit(2)
	}
	childCmd := args[sepIdx+1:]

	targets, err := parseRunSpec(toSpec)
	fatal(err, "parse --to")

	cfg := mustLoadConfig()
	cache := getCache()
	ensureCache(cache)
	client, err := NewAliyunClient(cfg)
	fatal(err, "create client")

	// Resolve all targets (some need API calls — do them serially to keep
	// output order deterministic; N is small, overhead is dwarfed by one
	// port-forward startup).
	for i := range targets {
		host, port, vpcID, rerr := resolveFwdTarget(cfg, targets[i].raw)
		if rerr != nil {
			fatal(fmt.Errorf("target %s (%q): %w", targets[i].name, targets[i].raw, rerr), "resolve")
		}
		targets[i].host = host
		targets[i].remotePort = port
		targets[i].vpcID = vpcID
	}

	// Set up each forward. On any failure we roll back what's already up.
	var cleanups []func()
	rollback := func() {
		for _, c := range cleanups {
			c()
		}
	}

	for i := range targets {
		jump, err := pickJumpHost(cache, targets[i].vpcID, via)
		if err != nil {
			rollback()
			fatal(err, fmt.Sprintf("pick jump for %s", targets[i].name))
		}
		targets[i].jumpID = jump.ID
		targets[i].jumpName = jump.Name

		var socatPort int
		var cleanup func()
		if targets[i].host == "localhost" || targets[i].host == "127.0.0.1" {
			socatPort = targets[i].remotePort
		} else {
			socatPort, _, cleanup, err = setupSocatRelay(client, jump.ID, targets[i].host, targets[i].remotePort)
			if err != nil {
				rollback()
				fatal(err, fmt.Sprintf("socat for %s", targets[i].name))
			}
		}
		if cleanup != nil {
			cleanups = append(cleanups, cleanup)
		}

		targets[i].localPort = findFreePort()
		stop, err := startPortForwardBgWithCancel(cfg, jump.ID, targets[i].localPort, socatPort)
		if err != nil {
			rollback()
			fatal(err, fmt.Sprintf("port-forward for %s", targets[i].name))
		}
		cleanups = append(cleanups, stop)
	}

	// Print the topology so devs can paste it back into application.yml
	// if they want to pin the values.
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "🔌 端口转发就绪 — 注入到子进程 env:")
	for _, t := range targets {
		fmt.Fprintf(os.Stderr, "   %s  127.0.0.1:%d → %s:%d  (via %s)\n",
			envPrefix(t.name)+"_*", t.localPort, t.host, t.remotePort, t.jumpName)
	}
	fmt.Fprintln(os.Stderr)

	env := os.Environ()
	for _, t := range targets {
		prefix := envPrefix(t.name)
		env = append(env,
			prefix+"_HOST=127.0.0.1",
			fmt.Sprintf("%s_PORT=%d", prefix, t.localPort),
			fmt.Sprintf("%s_ADDR=127.0.0.1:%d", prefix, t.localPort),
		)
	}

	// Exec child. We inherit stdio so tools like ./gradlew that detect TTY
	// continue to work. Signal forwarding keeps Ctrl-C interactive.
	c := exec.Command(childCmd[0], childCmd[1:]...)
	c.Env = env
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	if err := c.Start(); err != nil {
		rollback()
		fatal(err, "start child")
	}

	// Forward SIGINT/SIGTERM to child. On child exit we roll back and
	// propagate the exit code so CI/make integrations keep working.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	var once sync.Once
	relaySignal := func(sig os.Signal) {
		if c.Process != nil {
			_ = c.Process.Signal(sig)
		}
		once.Do(rollback)
	}
	go func() {
		for sig := range sigCh {
			relaySignal(sig)
		}
	}()

	werr := c.Wait()
	once.Do(rollback)

	if werr != nil {
		if ee, ok := werr.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "❌ child: %v\n", werr)
		os.Exit(1)
	}
}

// runTarget is one entry on the --to line; filled in stages (raw → resolved →
// assigned-jump-and-port).
type runTarget struct {
	name       string // user label (env prefix)
	raw        string // raw target (rm-xxx / host:port / ...)
	host       string // resolved
	remotePort int
	vpcID      string
	jumpID     string
	jumpName   string
	localPort  int
}

func parseRunSpec(spec string) ([]runTarget, error) {
	var out []runTarget
	seen := map[string]bool{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.IndexByte(part, '=')
		if eq <= 0 || eq == len(part)-1 {
			return nil, fmt.Errorf("bad --to entry %q, 期望 key=target", part)
		}
		name := strings.TrimSpace(part[:eq])
		raw := strings.TrimSpace(part[eq+1:])
		if !isValidEnvName(name) {
			return nil, fmt.Errorf("key %q 含非法字符 — 只允许字母/数字/下划线, 且不能以数字开头", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate --to key: %s", name)
		}
		seen[name] = true
		out = append(out, runTarget{name: name, raw: raw})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--to 不能为空")
	}
	return out, nil
}

// envPrefix normalizes a --to key into an env-var prefix: upper-case, replace
// hyphens. "redis-cache" → "REDIS_CACHE" → REDIS_CACHE_HOST/_PORT/_ADDR.
func envPrefix(name string) string {
	return strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}

func isValidEnvName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		ok := r == '_' || r == '-' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(i > 0 && r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	// Must not start with a digit (already guaranteed above since i==0 excludes digits).
	return true
}

func printRunHelp() {
	fmt.Println(`用法: tssh run --to <key=target>[,<key=target>...] [--via <jump>] -- <cmd> [args...]

同时打开多条端口转发, 把 <KEY>_HOST / <KEY>_PORT / <KEY>_ADDR 注入子进程,
适合本地 Spring 启动前一次把 MySQL/Redis/MQ 全接上.

--to 条目 (target 与 tssh fwd 同):
  mysql=rm-xxxxxxxx           RDS 实例 ID
  redis=r-xxxxxxxx            Redis 实例 ID
  kafka=10.0.0.3:9092         host:port
  nacos=nacos.internal:8848   任意内网 host

--via <jump>      全部走指定跳板; 不写就按每个 target 的 VPC 自动挑

示例:
  tssh run --to mysql=rm-xxx,redis=r-xxx,kafka=10.0.0.3:9092 -- ./gradlew bootRun
  # 子进程会看到:
  #   MYSQL_HOST=127.0.0.1 MYSQL_PORT=54321 MYSQL_ADDR=127.0.0.1:54321
  #   REDIS_HOST=127.0.0.1 REDIS_PORT=54322 REDIS_ADDR=127.0.0.1:54322
  #   KAFKA_HOST=127.0.0.1 KAFKA_PORT=54323 KAFKA_ADDR=127.0.0.1:54323

application.yml 配合写法 (多数项目本来就支持):
  spring.datasource.url: jdbc:mysql://${MYSQL_HOST:localhost}:${MYSQL_PORT:3306}/app
  spring.redis.host: ${REDIS_HOST:localhost}
  spring.redis.port: ${REDIS_PORT:6379}

Ctrl+C 或子进程退出时自动清理所有转发.`)
}
