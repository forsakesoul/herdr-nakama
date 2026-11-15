package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// 假 herdr：测试二进制自身在 TestMain 识别 NAKAMA_FAKE_HERDR_LOG 后充当 HERDR_BIN_PATH，
// 记录 argv（JSON 行）、按环境变量注入错误、用 barrier 文件做并发编排（不用固定 sleep）。
func TestMain(m *testing.M) {
	os.Setenv("NAKAMA_LANG", "zh")                         // 既有断言按中文源文案编写；其他语言由 i18n_test 覆盖
	if len(os.Args) > 1 && os.Args[1] == "--hr-run-main" { // 以测试二进制充当真实入口
		stdinIsTTY = func() bool { return os.Getenv("NAKAMA_TEST_TTY") != "" } // 子进程 stdin 是管道，按测试声明视作终端
		os.Exit(run(os.Args[2:]))
	}
	if os.Getenv("NAKAMA_FAKE_HERDR_LOG") != "" {
		fakeHerdr(os.Args[1:])
		return
	}
	os.Exit(m.Run())
}

type nameRow struct{ key, pane, term, kind, sess string }

func readRows(file string) []nameRow {
	b, _ := os.ReadFile(file)
	var rows []nameRow
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if l == "" {
			continue
		}
		f := append(strings.SplitN(l, "|", 5), "", "", "", "", "")
		rows = append(rows, nameRow{f[0], f[1], f[2], f[3], f[4]})
	}
	return rows
}

func writeRows(file string, rows []nameRow) {
	var sb strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&sb, "%s|%s|%s|%s|%s\n", r.key, r.pane, r.term, r.kind, r.sess)
	}
	os.WriteFile(file, []byte(sb.String()), 0o644)
}

func appendRow(file string, r nameRow) { writeRows(file, append(readRows(file), r)) }

func fakeHerdr(args []string) {
	log := os.Getenv("NAKAMA_FAKE_HERDR_LOG")
	names := log + ".names"
	line, _ := json.Marshal(map[string]any{"argv": args})
	f, _ := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	f.Write(append(line, '\n'))
	f.Close()
	get := func(i int) string {
		if i < len(args) {
			return args[i]
		}
		return ""
	}
	env := func(k, d string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return d
	}
	lookup := func(t string) (nameRow, bool) {
		for _, r := range readRows(names) {
			if r.key == t {
				return r, true
			}
		}
		return nameRow{}, false
	}
	lookupPane := func(t string) (nameRow, bool) {
		for _, r := range readRows(names) {
			if r.pane == t {
				return r, true
			}
		}
		return nameRow{}, false
	}
	errOut := func(code string) {
		fmt.Fprintf(os.Stderr, `{"error":{"code":"%s"}}`+"\n", code)
		os.Exit(1)
	}
	counter := func(file string) int {
		b, _ := os.ReadFile(file)
		n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		n++
		os.WriteFile(file, []byte(strconv.Itoa(n)), 0o644)
		return n
	}
	switch get(0) + " " + get(1) {
	case "pane current", "pane get":
		if os.Getenv("NAKAMA_FAKE_PANE_GET_FAIL") != "" {
			errOut("server_unavailable")
		}
		pane := env("FAKE_ANCHOR_PANE", "w0:p1")
		if get(1) == "get" && get(2) != "" {
			pane = get(2)
		}
		cwd := env("FAKE_ANCHOR_CWD", "/tmp")
		tab := "w0:t1"
		if other := os.Getenv("FAKE_OTHER_TAB_PANE"); other != "" && pane == other {
			tab = "w0:t9" // 模拟新 pane 落在锚点以外的 tab
		}
		if os.Getenv("FAKE_ANCHOR_CWD_EMPTY") != "" {
			cwd = ""
		}
		term := "term_" + pane
		if r, ok := lookupPane(pane); ok {
			term = r.term
		} else if at := os.Getenv("FAKE_ANCHOR_TERM"); at != "" && pane == env("FAKE_ANCHOR_PANE", "w0:p1") {
			// 模拟重启后 shell 锚点的 terminal 变化（锚点无 agent 行，无法经 agent get 观测）。
			term = at
		}
		fmt.Printf(`{"id":"cli:pane:%s","result":{"pane":{"pane_id":"%s","cwd":"%s","workspace_id":"w0","tab_id":"%s","terminal_id":"%s","agent_status":"unknown"}}}`+"\n", get(1), pane, cwd, tab, term)
	case "pane split":
		if dir := os.Getenv("NAKAMA_FAKE_SPLIT_BARRIER"); dir != "" {
			if _, err := os.Stat(filepath.Join(dir, "ready")); err != nil {
				os.WriteFile(filepath.Join(dir, "ready"), nil, 0o644)
				for i := 0; i < 400; i++ {
					if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
						break
					}
					time.Sleep(50 * time.Millisecond)
				}
			}
		}
		b, _ := os.ReadFile(log + ".split")
		n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		n++
		if failAt := os.Getenv("NAKAMA_FAKE_SPLIT_FAIL_AT"); failAt != "" && failAt == strconv.Itoa(n) {
			errOut("pane_split_failed")
		}
		os.WriteFile(log+".split", []byte(strconv.Itoa(n)), 0o644)
		fmt.Printf(`{"result":{"pane":{"pane_id":"w0:p%d"}}}`+"\n", 9+n)
	case "pane run":
		kind := "shell"
		switch cmd := get(3); {
		case strings.Contains(cmd, "pi --approve"):
			kind = "pi"
		case strings.Contains(cmd, "cc"):
			kind = "claude"
		}
		appendRow(names, nameRow{env("NAKAMA_FAKE_MANUAL_NAME", "@"+get(2)), get(2), "term_" + get(2), kind, ""})
		fmt.Println(`{"result":{"ok":true}}`)
	case "agent start":
		name, kind, pane := get(2), "", ""
		for i := 3; i < len(args); i++ {
			switch args[i] {
			case "--kind":
				i++
				kind = get(i)
			case "--pane":
				i++
				pane = get(i)
			}
		}
		if os.Getenv("FAKE_STRICT_NAME") != "" && !regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`).MatchString(name) {
			fmt.Fprintln(os.Stderr, `{"error":{"code":"invalid_agent_name","message":"agent name must start with a lowercase letter (1-32 characters)"}}`)
			os.Exit(1)
		}
		if n, _ := strconv.Atoi(os.Getenv("FAKE_BUSY_ATTEMPTS")); n > 0 && counter(log+".busy") <= n {
			errOut("agent_pane_busy")
		}
		if sfx := os.Getenv("NAKAMA_FAKE_START_FAIL_SUFFIX"); sfx != "" && strings.HasSuffix(name, sfx) {
			errOut("server_unavailable") // 仅让名称后缀匹配的成员启动失败（多成员失败注入）
		}
		appendRow(names, nameRow{name, pane, "term_" + pane, kind, ""})
		if os.Getenv("NAKAMA_FAKE_START_NOT_READY") != "" {
			// 真实 Herdr：启动时卡在对话框，名字保留但返回 agent_not_ready。
			errOut("agent_not_ready")
		}
		fmt.Println(`{"result":{"ok":true}}`)
	case "agent rename":
		pane, name := get(2), get(3)
		if os.Getenv("NAKAMA_FAKE_RENAME_INPLACE") != "" {
			// 模拟真实 Herdr：就地改名（同一 pane 只保留一行）。FAIL_AT=n：第 n 次不生效并失败；APPLY_ERR_AT=n：生效后返回错误。
			n := strconv.Itoa(counter(log + ".rename"))
			if n == os.Getenv("NAKAMA_FAKE_RENAME_FAIL_AT") {
				errOut("agent_rename_failed")
			}
			term, kind, sess := "term_"+pane, "claude", ""
			if r, ok := lookupPane(pane); ok {
				term, kind, sess = r.term, r.kind, r.sess
			}
			kept := []nameRow{}
			for _, r := range readRows(names) {
				if r.pane != pane {
					kept = append(kept, r)
				}
			}
			writeRows(names, append(kept, nameRow{name, pane, term, kind, sess}))
			if n == os.Getenv("NAKAMA_FAKE_RENAME_APPLY_ERR_AT") {
				errOut("agent_rename_timeout")
			}
			fmt.Println(`{"result":{"ok":true}}`)
			return
		}
		term, kind := "term_"+pane, "claude"
		if r, ok := lookupPane(pane); ok {
			term, kind = r.term, r.kind
		}
		appendRow(names, nameRow{name, pane, term, kind, os.Getenv("NAKAMA_FAKE_RENAME_SESSION")})
		fmt.Println(`{"result":{"ok":true}}`)
	case "agent get":
		if dir := os.Getenv("NAKAMA_FAKE_GET_BARRIER"); dir != "" {
			if _, err := os.Stat(filepath.Join(dir, "ready")); err != nil {
				os.WriteFile(filepath.Join(dir, "ready"), nil, 0o644)
				for i := 0; i < 400; i++ {
					if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
						break
					}
					time.Sleep(50 * time.Millisecond)
				}
			}
		}
		if n, _ := strconv.Atoi(os.Getenv("NAKAMA_FAKE_GET_DELAY_N")); n > 0 && counter(log+".getcount") <= n {
			errOut("agent_not_found")
		}
		if c := os.Getenv("NAKAMA_FAKE_GET_ERROR"); c != "" {
			errOut(c)
		}
		if os.Getenv("NAKAMA_FAKE_GET_FAIL") != "" {
			errOut("agent_not_found")
		}
		r, ok := lookup(get(2))
		if !ok {
			r, ok = lookupPane(get(2))
		}
		if !ok {
			errOut("agent_not_found")
		}
		name := r.key
		if strings.HasPrefix(name, "@") {
			name = ""
		}
		sess := "null"
		if r.sess != "" {
			sess = `{"value":"` + r.sess + `"}`
		}
		if n, _ := strconv.Atoi(os.Getenv("NAKAMA_FAKE_SESSION_AFTER")); n > 0 && sess == "null" && counter(log+".sessget."+r.pane) > n {
			sess = `{"value":"sess-late"}`
		}
		status := env("FAKE_STATUS", "idle")
		if seq := os.Getenv("NAKAMA_FAKE_STATUS_SEQ"); seq != "" {
			// 按成功 get 的次序逐个给出状态，末项重复；blocked/unknown 时无 session，进入 idle 等稳定状态后才上报 NAKAMA_FAKE_LATE_SESSION。
			states := strings.Split(seq, ",")
			status = states[min(counter(log+".statusget"), len(states))-1]
			if status == "blocked" || status == "unknown" {
				sess = "null"
			} else if late := os.Getenv("NAKAMA_FAKE_LATE_SESSION"); late != "" && sess == "null" {
				sess = `{"value":"` + late + `"}`
			}
		}
		if sfx := os.Getenv("FAKE_WORKING_SUFFIX"); sfx != "" && name != "" && strings.HasSuffix(name, sfx) {
			status = "working"
		}
		fmt.Printf(`{"result":{"agent":{"name":"%s","pane_id":"%s","terminal_id":"%s","agent":"%s","agent_session":%s,"agent_status":"%s"}}}`+"\n", name, r.pane, r.term, r.kind, sess, status)
	case "agent prompt":
		if dir := os.Getenv("NAKAMA_FAKE_PROMPT_BARRIER"); dir != "" && get(2) == os.Getenv("NAKAMA_FAKE_PROMPT_BARRIER_NAME") {
			os.WriteFile(filepath.Join(dir, "ready"), nil, 0o644)
			for i := 0; i < 400; i++ {
				if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
		}
		if code := os.Getenv("FAKE_PROMPT_ERROR"); code != "" {
			errOut(code)
		}
		fmt.Println(`{"result":{"ok":true}}`)
	case "plugin update":
		if os.Getenv("FAKE_PLUGIN_UPDATE_FAIL") != "" {
			fmt.Fprintln(os.Stderr, "error: unrecognized subcommand 'update'")
			os.Exit(2)
		}
		fmt.Println(`{"result":{"ok":true}}`)
	case "plugin list":
		fmt.Printf(`{"result":{"plugins":[{"source":{"kind":"%s"}}]}}`+"\n", env("FAKE_PLUGIN_SOURCE", "local"))
	case "plugin pane":
		if os.Getenv("NAKAMA_FAKE_PLUGIN_OPEN_FAIL") != "" {
			errOut("plugin_pane_open_failed")
		}
		fmt.Println(`{"result":{"ok":true}}`)
	case "pane read":
		if os.Getenv("FAKE_PANE_READ_FAIL") != "" {
			errOut("server_unavailable")
		}
		fmt.Println(os.Getenv("FAKE_PANE_TEXT"))
	case "agent read":
		fmt.Println("recent output")
	case "agent list":
		fmt.Println(`{"result":{"agents":[]}}`)
	default:
		fmt.Println(`{"result":{"ok":true}}`)
	}
}

// ---- 测试环境 ----
type tenv struct {
	t         *testing.T
	root      string // 临时根
	proj      string
	ctx       string
	log       string
	repoRoot  string // 插件根（prompts/）
	ccEnvPath string
}

func newEnv(t *testing.T) *tenv {
	t.Helper()
	root := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	repo, _ := filepath.Abs("../..")
	e := &tenv{t: t, root: root, proj: filepath.Join(root, "proj"), ctx: filepath.Join(root, "ctx.md"), log: filepath.Join(root, "herdr.log"), repoRoot: repo}
	for k, v := range map[string]string{
		"HOME": root, "TMPDIR": root, "HERDR_BIN_PATH": exe,
		"HERDR_PLUGIN_STATE_DIR": filepath.Join(root, "state"), "HERDR_PLUGIN_CONFIG_DIR": filepath.Join(root, "config"),
		"NAKAMA_FAKE_HERDR_LOG": e.log, "FAKE_ANCHOR_PANE": "w0:p1", "FAKE_ANCHOR_CWD": e.proj,
		"HERDR_PANE_ID": "", "HERDR_PLUGIN_CONTEXT_JSON": "",
	} {
		t.Setenv(k, v)
	}
	os.MkdirAll(e.proj, 0o755)
	for _, c := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "git@example.invalid:acme/demo-repo.git"}} {
		if out, err := exec.Command("git", append([]string{"-C", e.proj}, c...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", c, err, out)
		}
	}
	os.WriteFile(e.ctx, []byte("项目上下文：只读参考。\n"), 0o644)
	os.WriteFile(e.log, nil, 0o644)
	os.WriteFile(e.log+".names", nil, 0o644)
	return e
}

func (e *tenv) config(kind string) {
	e.t.Helper()
	proj := fmt.Sprintf(`{ "demo-repo": { "prefix": "dm", "context_file": %q, "trigger": ";dm.ctx" } }`, e.ctx)
	cfg := map[string]string{
		"normal": `{ "default_profile": { "owner": "sol", "worker": "ds", "advisor": "none", "pi_provider": "demo-provider",
  "engine_map": { "sol": "codex/gpt-6.1-sol:high", "ds": { "owner": "pi/model-x:low", "worker": "pi/model-x:low", "advisor": "pi/model-x:high" }, "cc": "cc" } },
  "projects": ` + proj + ` }`,
		"codex":     `{ "default_profile": { "owner": "codex/gpt-6.1-sol:high", "worker": "codex/gpt-6.1-luna:medium", "advisor": "none" }, "projects": {} }`,
		"noproj":    `{ "default_profile": { "owner": "codex/gpt-6.1-sol:high", "worker": "codex/gpt-6.1-sol:medium", "advisor": "none" }, "projects": {} }`,
		"badctx":    fmt.Sprintf(`{ "default_profile": { "owner": "codex/gpt-6.1-sol:high", "worker": "codex/gpt-6.1-sol:medium", "advisor": "none" }, "projects": { "demo-repo": { "prefix": "dm", "context_file": %q, "trigger": ";dm.ctx" } } }`, filepath.Join(e.root, "missing.md")),
		"badprefix": fmt.Sprintf(`{ "default_profile": { "owner": "codex/gpt-6.1-sol:high", "worker": "codex/gpt-6.1-sol:medium", "advisor": "none" }, "projects": { "demo-repo": { "prefix": "Bad_Name", "context_file": %q, "trigger": ";dm.ctx" } } }`, e.ctx),
		"ccenv":     fmt.Sprintf(`{ "default_profile": { "owner": "ccenv/model-x", "worker": "cc", "advisor": "none", "cc_env_file": %q }, "projects": %s }`, e.ccEnvPath, proj),
		"piowner":   `{ "default_profile": { "owner": "pi/model-x:low", "worker": "codex/gpt-6.1-luna:medium", "advisor": "none", "pi_provider": "demo-provider" }, "projects": {} }`,
	}[kind]
	e.rawConfig(cfg)
}

func (e *tenv) rawConfig(s string) {
	e.t.Helper()
	dir := os.Getenv("HERDR_PLUGIN_CONFIG_DIR")
	os.MkdirAll(dir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(s), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// run 在进程内执行一次命令（假 herdr 仍是独立子进程）；返回是否成功与合并输出。
func (e *tenv) run(args ...string) (bool, string) {
	var buf syncBuf
	a := &app{stdout: &buf, stderr: &buf, root: e.repoRoot, st: newStore()} // poll 零值=不等待
	defer a.unlockAll()
	err := a.dispatch(args)
	return err == nil, buf.String()
}

func (e *tenv) mustRun(args ...string) string {
	e.t.Helper()
	ok, out := e.run(args...)
	if !ok {
		e.t.Fatalf("%v 应成功：\n%s", args, out)
	}
	return out
}

func (e *tenv) mustFail(args ...string) string {
	e.t.Helper()
	ok, out := e.run(args...)
	if ok {
		e.t.Fatalf("%v 应非零：\n%s", args, out)
	}
	return out
}

func (e *tenv) argvs() [][]string {
	b, _ := os.ReadFile(e.log)
	var all [][]string
	for _, l := range strings.Split(string(b), "\n") {
		var r struct{ Argv []string }
		if json.Unmarshal([]byte(l), &r) == nil && r.Argv != nil {
			all = append(all, r.Argv)
		}
	}
	return all
}

func (e *tenv) count(a, b string) int {
	n := 0
	for _, v := range e.argvs() {
		if len(v) > 1 && v[0] == a && v[1] == b {
			n++
		}
	}
	return n
}

func (e *tenv) argvOf(a, b, third string) []string {
	for _, v := range e.argvs() {
		if len(v) > 2 && v[0] == a && v[1] == b && v[2] == third {
			return v
		}
	}
	return nil
}

func (e *tenv) prompts(name string) []string {
	var out []string
	for _, v := range e.argvs() {
		if len(v) > 3 && v[0] == "agent" && v[1] == "prompt" && v[2] == name {
			out = append(out, v[3])
		}
	}
	return out
}

func (e *tenv) teamID() string {
	ents, _ := os.ReadDir(filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "teams"))
	if len(ents) == 0 {
		e.t.Fatal("没有编组状态")
	}
	return strings.TrimSuffix(ents[0].Name(), ".json")
}

func (e *tenv) stateFile() string {
	return filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "teams", e.teamID()+".json")
}

// state 返回解析后的状态（类 jq 取值用 dig）。
func (e *tenv) state() map[string]any {
	var m map[string]any
	b, err := os.ReadFile(e.stateFile())
	if err != nil {
		e.t.Fatal(err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		e.t.Fatal(err)
	}
	return m
}

func dig(m map[string]any, path ...string) any {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

func (e *tenv) str(path ...string) string {
	if s, ok := dig(e.state(), path...).(string); ok {
		return s
	}
	return fmt.Sprint(dig(e.state(), path...))
}

// stateSet 就地改状态文件，模拟旧测试里的 jq 篡改。
func (e *tenv) stateSet(f func(m map[string]any)) {
	m := e.state()
	f(m)
	b, _ := json.Marshal(m)
	os.WriteFile(e.stateFile(), b, 0o600)
}

func (e *tenv) names() string { return e.log + ".names" }
func (e *tenv) rowSet(key, pane, term, kind, sess string) {
	rows := readRows(e.names())
	kept := rows[:0]
	for _, r := range rows {
		if r.key != key {
			kept = append(kept, r)
		}
	}
	writeRows(e.names(), append(kept, nameRow{key, pane, term, kind, sess}))
}
func (e *tenv) rowDel(key string) {
	rows := readRows(e.names())
	kept := rows[:0]
	for _, r := range rows {
		if r.key != key {
			kept = append(kept, r)
		}
	}
	writeRows(e.names(), kept)
}
func (e *tenv) paneRowSet(pane, term, kind, sess string) { e.rowSet("@"+pane, pane, term, kind, sess) }

func has(t *testing.T, out, sub, msg string) {
	t.Helper()
	if !strings.Contains(out, sub) {
		t.Fatalf("%s：缺少 %q\n--- 输出 ---\n%s", msg, sub, out)
	}
}
func hasNot(t *testing.T, out, sub, msg string) {
	t.Helper()
	if strings.Contains(out, sub) {
		t.Fatalf("%s：不应包含 %q\n--- 输出 ---\n%s", msg, sub, out)
	}
}
func eq[T comparable](t *testing.T, got, want T, msg string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s：got=%v want=%v", msg, got, want)
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func commandIn(name string, args []string, extraEnv ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	c.Env = append(os.Environ(), extraEnv...)
	return c
}

func regexpMust(p string) *regexp.Regexp { return regexp.MustCompile(p) }

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
