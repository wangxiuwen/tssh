package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// cmdWeb starts an embedded HTTP server with a management web UI.
//
// Security: /api/exec can run arbitrary commands on all ECS instances bound
// to the current AccessKey. Defaults are locked down to prevent accidents:
//   - bind 127.0.0.1 only
//   - opt-in to public bind via --bind <addr>, and only if a --token is set
//   - token compared in constant time
func cmdWeb(args []string) {
	port := "8080"
	token := ""
	bindHost := "127.0.0.1"
	for i := 0; i < len(args); i++ {
		if (args[i] == "--port" || args[i] == "-p") && i+1 < len(args) {
			port = args[i+1]
			i++
		} else if (args[i] == "--token" || args[i] == "-t") && i+1 < len(args) {
			token = args[i+1]
			i++
		} else if args[i] == "--bind" && i+1 < len(args) {
			bindHost = args[i+1]
			i++
		}
	}

	// Guard: public bind without token is a remote command-execution exposure.
	if bindHost != "127.0.0.1" && bindHost != "localhost" && token == "" {
		fmt.Fprintf(os.Stderr, "❌ --bind %s 要求必须设置 --token (/api/exec 可执行任意命令)\n", bindHost)
		fmt.Fprintln(os.Stderr, "   示例: tssh web --bind 0.0.0.0 --token $(openssl rand -hex 16)")
		os.Exit(2)
	}

	tokenBytes := []byte(token)
	// Auth middleware — constant-time compare mitigates timing attacks.
	requireAuth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if token != "" {
				got := ""
				if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
					got = strings.TrimPrefix(h, "Bearer ")
				} else {
					got = r.URL.Query().Get("token")
				}
				if subtle.ConstantTimeCompare([]byte(got), tokenBytes) != 1 {
					http.Error(w, `{"error":"unauthorized"}`, 401)
					return
				}
			}
			next(w, r)
		}
	}

	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/instances", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		cache := getCache()
		instances, err := cache.Load()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		json.NewEncoder(w).Encode(instances)
	}))

	mux.HandleFunc("/api/exec", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST only", 405)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		var req struct {
			Target  string `json:"target"`
			Command string `json:"command"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		if req.Target == "" || req.Command == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "target and command required"})
			return
		}

		config := mustLoadConfig()
		client, err := NewAliyunClient(config)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		cache := getCache()
		inst, err2 := resolveInstance(cache, req.Target)
		if err2 != nil {
			json.NewEncoder(w).Encode(map[string]string{"error": err2.Error()})
			return
		}
		result, err := client.RunCommand(inst.ID, req.Command, 30)

		resp := map[string]interface{}{
			"name": inst.Name,
			"id":   inst.ID,
		}
		if err != nil {
			resp["error"] = err.Error()
		} else {
			resp["output"] = decodeOutput(result.Output)
			resp["exit_code"] = result.ExitCode
		}
		json.NewEncoder(w).Encode(resp)
	}))

	mux.HandleFunc("/api/sync", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST only", 405)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := cmdSyncQuiet(); err != nil {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))

	mux.HandleFunc("/api/info/", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		name := strings.TrimPrefix(r.URL.Path, "/api/info/")
		if name == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "instance name required in URL path"})
			return
		}
		cache := getCache()
		inst, err := resolveInstance(cache, name)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		config := mustLoadConfig()
		client, _ := NewAliyunClient(config)
		detail, _ := client.GetInstanceDetail(inst.ID)

		resp := map[string]interface{}{
			"instance": inst,
			"detail":   detail,
		}
		json.NewEncoder(w).Encode(resp)
	}))

	// Serve embedded HTML UI
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, webHTML)
	})

	addr := bindHost + ":" + port
	fmt.Printf("🌐 tssh web 面板已启动\n")
	fmt.Printf("   地址: http://%s\n", addr)
	if token == "" {
		fmt.Printf("   认证: ⚠️  未设置 token (仅本机可访问, 如需远程加 --token 和 --bind 0.0.0.0)\n")
	} else {
		fmt.Printf("   认证: Bearer token 已启用\n")
	}
	fmt.Printf("   按 Ctrl+C 退出\n")

	// Explicit timeouts so a slow-loris client (or a forgotten curl hang) can't
	// hold connections forever. /api/exec takes up to 30s; give WriteTimeout
	// a bit more headroom. ReadHeaderTimeout stops pre-TLS slowloris.
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
}

const webHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>tssh — 云服务器管理面板</title>
<style>
:root { --bg: #0f172a; --card: #1e293b; --accent: #3b82f6; --ok: #22c55e; --warn: #f59e0b; --err: #ef4444; --text: #e2e8f0; --muted: #94a3b8; }
* { margin: 0; padding: 0; box-sizing: border-box; }
body { font-family: 'Inter', system-ui, sans-serif; background: var(--bg); color: var(--text); min-height: 100vh; }
.header { background: linear-gradient(135deg, #1e3a5f, #0f172a); padding: 20px 30px; border-bottom: 1px solid #334155; display: flex; justify-content: space-between; align-items: center; }
.header h1 { font-size: 1.5rem; font-weight: 600; }
.header h1 span { color: var(--accent); }
.header .actions { display: flex; gap: 10px; }
.btn { padding: 8px 16px; border: 1px solid #475569; background: var(--card); color: var(--text); border-radius: 6px; cursor: pointer; font-size: 13px; transition: all .2s; }
.btn:hover { background: #334155; border-color: var(--accent); }
.btn-primary { background: var(--accent); border-color: var(--accent); }
.btn-primary:hover { background: #2563eb; }
.container { max-width: 1200px; margin: 0 auto; padding: 20px; }
.search { width: 100%; padding: 10px 16px; background: var(--card); border: 1px solid #334155; border-radius: 8px; color: var(--text); font-size: 14px; margin-bottom: 16px; }
.search:focus { outline: none; border-color: var(--accent); }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); gap: 12px; }
.card { background: var(--card); border: 1px solid #334155; border-radius: 8px; padding: 16px; transition: all .2s; cursor: pointer; }
.card:hover { border-color: var(--accent); transform: translateY(-1px); }
.card .name { font-weight: 600; font-size: 14px; margin-bottom: 6px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.card .meta { display: flex; gap: 12px; font-size: 12px; color: var(--muted); }
.card .status { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 6px; }
.card .status.running { background: var(--ok); }
.card .status.stopped { background: var(--err); }
.modal { display: none; position: fixed; inset: 0; background: rgba(0,0,0,.7); z-index: 100; align-items: center; justify-content: center; }
.modal.active { display: flex; }
.modal-content { background: var(--card); border-radius: 12px; padding: 24px; width: 90%; max-width: 700px; max-height: 80vh; overflow-y: auto; }
.modal h2 { margin-bottom: 16px; font-size: 1.2rem; }
.modal .close { float: right; cursor: pointer; font-size: 20px; color: var(--muted); }
.terminal { background: #0a0a0a; border-radius: 8px; padding: 12px; font-family: 'JetBrains Mono', monospace; font-size: 13px; color: #4ade80; white-space: pre-wrap; word-break: break-all; min-height: 100px; max-height: 400px; overflow-y: auto; margin-top: 12px; }
.exec-form { display: flex; gap: 8px; margin-top: 12px; }
.exec-form input { flex: 1; padding: 8px 12px; background: #0a0a0a; border: 1px solid #334155; border-radius: 6px; color: var(--text); font-family: monospace; }
.stats { display: flex; gap: 20px; margin-bottom: 16px; }
.stat { background: var(--card); border: 1px solid #334155; border-radius: 8px; padding: 14px 20px; }
.stat .val { font-size: 1.5rem; font-weight: 700; color: var(--accent); }
.stat .label { font-size: 12px; color: var(--muted); margin-top: 2px; }
</style>
</head>
<body>
<div class="header">
  <h1>🖥 <span>tssh</span> 管理面板</h1>
  <div class="actions"><button class="btn" onclick="syncCache()">🔄 刷新缓存</button></div>
</div>
<div class="container">
  <div class="stats" id="stats"></div>
  <input class="search" placeholder="🔍 搜索实例 (名称/IP/ID)..." oninput="filterInstances(this.value)">
  <div class="grid" id="grid"></div>
</div>

<div class="modal" id="modal">
  <div class="modal-content">
    <span class="close" onclick="closeModal()">✕</span>
    <h2 id="modal-title"></h2>
    <div id="modal-info"></div>
    <div class="exec-form">
      <input id="cmd-input" placeholder="输入命令..." onkeydown="if(event.key==='Enter')execCmd()">
      <button class="btn btn-primary" onclick="execCmd()">▶ 执行</button>
    </div>
    <div class="terminal" id="terminal">等待输入命令...</div>
  </div>
</div>

<script>
let instances = [];
let currentInst = null;

async function loadInstances() {
  const res = await fetch('/api/instances');
  instances = await res.json();
  renderStats();
  renderGrid(instances);
}

function renderStats() {
  const total = instances.length;
  const running = instances.filter(i => i.status === 'Running').length;
  const stopped = total - running;
  document.getElementById('stats').innerHTML =
    '<div class="stat"><div class="val">' + total + '</div><div class="label">总实例</div></div>' +
    '<div class="stat"><div class="val" style="color:var(--ok)">' + running + '</div><div class="label">运行中</div></div>' +
    '<div class="stat"><div class="val" style="color:var(--err)">' + stopped + '</div><div class="label">已停止</div></div>';
}

function renderGrid(list) {
  document.getElementById('grid').innerHTML = list.map(inst => {
    const st = inst.status === 'Running' ? 'running' : 'stopped';
    return '<div class="card" onclick=\'showInstance("'+inst.name+'")\'>' +
      '<div class="name"><span class="status '+st+'"></span>' + inst.name + '</div>' +
      '<div class="meta"><span>' + inst.private_ip + '</span><span>' + inst.id + '</span></div></div>';
  }).join('');
}

function filterInstances(q) {
  q = q.toLowerCase();
  const filtered = instances.filter(i => i.name.toLowerCase().includes(q) || i.private_ip.includes(q) || i.id.includes(q));
  renderGrid(filtered);
}

async function showInstance(name) {
  currentInst = instances.find(i => i.name === name);
  document.getElementById('modal-title').textContent = currentInst.name;
  document.getElementById('modal-info').innerHTML =
    '<div class="meta" style="gap:20px"><span>ID: '+currentInst.id+'</span><span>IP: '+currentInst.private_ip+'</span><span>状态: '+currentInst.status+'</span></div>';
  document.getElementById('terminal').textContent = '等待输入命令...';
  document.getElementById('modal').classList.add('active');
  document.getElementById('cmd-input').focus();
}

function closeModal() {
  document.getElementById('modal').classList.remove('active');
}

async function execCmd() {
  const cmd = document.getElementById('cmd-input').value;
  if (!cmd || !currentInst) return;
  const terminal = document.getElementById('terminal');
  terminal.textContent = '$ ' + cmd + '\n执行中...';
  try {
    const res = await fetch('/api/exec', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({target: currentInst.name, command: cmd})
    });
    const data = await res.json();
    terminal.textContent = '$ ' + cmd + '\n' + (data.output || data.error || 'no output');
  } catch(e) {
    terminal.textContent = '$ ' + cmd + '\n❌ ' + e.message;
  }
  document.getElementById('cmd-input').value = '';
}

async function syncCache() {
  await fetch('/api/sync', {method:'POST'});
  loadInstances();
}

document.addEventListener('keydown', e => { if (e.key === 'Escape') closeModal(); });
loadInstances();
</script>
</body>
</html>`
