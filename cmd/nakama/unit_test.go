package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrefixLength(t *testing.T) {
	prefix, err := genPrefix("abcdefgh")
	if err != nil {
		t.Fatal(err)
	}
	if !regexpMust(`^abcdefgh_[0-9a-f]{12}$`).MatchString(prefix) {
		t.Fatalf("后缀格式不兼容：%s", prefix)
	}
	for _, role := range []string{"_design", "_execute", "_advisor"} {
		if !nameValid(prefix + role) {
			t.Fatalf("最长项目前缀的角色名非法：%s", prefix+role)
		}
	}
}

func TestBridgeAutoName(t *testing.T) {
	for _, c := range []struct{ cwd, terminal, want string }{
		{"/x/My Project_v2", "t-1", "my-project_v2-46e9"},
		{"/x/123abc", "t-1", "a-123abc-46e9"},
		{"/x/..weird..name", "t-1", "weird-name-46e9"},
		{"/x/tail-", "t-2", "tail-b1ad"},
		{"", "t-1", "agent-46e9"},
		{"/x/中文", "t-1", "agent-46e9"},
		{"/x/" + strings.Repeat("a", 40), "t-1", strings.Repeat("a", 27) + "-46e9"},
	} {
		eq(t, bridgeAutoName(c.cwd, c.terminal), c.want, "Bridge 自动名契约")
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestNameValid(t *testing.T) { // 旧 hr_name_valid 整串校验（换行、大写、空、长度、合法）
	for s, want := range map[string]bool{
		"ok\nbad": false, "Bad_Name": false, "": false, "1abc": false, "a b": false,
		"dm_1a2b3c4d5e6f_design": true, "a": true, strings.Repeat("a", 32): true, strings.Repeat("a", 33): false,
	} {
		eq(t, nameValid(s), want, "nameValid("+s+")")
	}
	eq(t, prefixValid("dm\nx"), false, "prefix 含换行")
	eq(t, prefixValid("abcdefgh"), true, "8 位上限")
	eq(t, prefixValid("abcdefghi"), false, "9 位超限")
}

// 真实 Herdr 0.9.3 夹具：agent get 的 agent 字段即 kind，agent_session.value 即 session；
// 普通 shell pane 也有 terminal_id。
func TestRealFixtures(t *testing.T) {
	o, ok := parseAgent(fixture(t, "agent_get.json"))
	if !ok {
		t.Fatal("agent_get.json 应解析出 agent")
	}
	eq(t, o.ident(), Ident{TerminalID: "term_65db61a36ff8b61", PaneID: "w1P:p4", Kind: "codex", Session: "00000000-0000-0000-0000-000000000000"}, "ident")
	eq(t, parsePane(fixture(t, "pane_get_shell.json")).TerminalID, "term_shell_0001", "shell pane terminal_id")
}

func TestIdentSessionEmptyVsNonEmptyIsDrift(t *testing.T) {
	a := Ident{"t", "p", "k", ""}
	eq(t, sameIdent(a, a), true, "同值")
	eq(t, sameIdent(a, Ident{"t", "p", "k", "s"}), false, "session 空→非空算漂移")
	eq(t, sameIdent(Ident{"t", "p", "k", "s"}, Ident{"t", "p", "k", "s2"}), false, "session 变化")
	eq(t, sameIdent(a, Ident{"t", "p", "k2", ""}), false, "kind 变化")
	eq(t, (Ident{"t", "p", "", ""}).complete(), false, "缺 kind 不完整")
}

func TestParseEngine(t *testing.T) {
	ok := map[string]engine{
		"codex/gpt-6.1-sol":         {"codex", "gpt-6.1-sol", "high"},
		"codex/gpt-6.1-luna":        {"codex", "gpt-6.1-luna", "medium"},
		"codex/gpt-6.1-sol[1M]:max": {"codex", "gpt-6.1-sol[1M]", "max"},
		"pi/m:low":                  {"pi", "m", "low"},
		"pi/m":                      {"pi", "m", "medium"},
		"cc":                        {"cc", "claude-opus-5-5", "medium"},
		"cc/opus:high":              {"cc", "opus", "high"},
		"cc/haiku":                  {"cc", "haiku", ""},
		"ccenv/model-x[1M]":         {"ccenv", "model-x[1M]", ""},
		"qoder/Qwen3.8-Flash":       {"qoder", "Qwen3.8-Flash", ""},
	}
	for in, want := range ok {
		got, valid := parseEngine(in)
		if !valid || got != want {
			t.Errorf("%s → %+v,%v，期望 %+v", in, got, valid, want)
		}
	}
	for _, in := range []string{"", "codex", "codex/m\nx", "pi", "cc:high", "ccenv/m:high", "qoder/m:low", "x/y", "pi/m with space", "pi/m:Low"} {
		if _, valid := parseEngine(in); valid {
			t.Errorf("%q 应非法", in)
		}
	}
	for in, kind := range map[string]string{"codex/m": "codex", "qoder/m": "qodercli", "cc": "claude", "ccenv/m": "claude", "pi/m": "pi"} {
		e, _ := parseEngine(in)
		eq(t, e.kind(), kind, "kind("+in+")")
	}
}

func TestPaneCommandQuoting(t *testing.T) {
	e, _ := parseEngine("pi/model-x:low")
	eq(t, e.paneCommand("it's", ""), `pi --approve --provider 'it'\''s' --model 'model-x' --thinking 'low'`, "pi 命令")
	ce, _ := parseEngine("ccenv/m")
	eq(t, ce.paneCommand("", "/a b/$(x)'.env"), `set -a; . '/a b/$(x)'\''.env' || exit 1; set +a; cc --model 'm'`, "ccenv 命令")
	cc, _ := parseEngine("cc")
	eq(t, cc.paneCommand("", ""), "cc --model 'claude-opus-5-5' --effort 'medium'", "裸 cc 默认 Opus 5.5 + medium")
	haiku, _ := parseEngine("cc/haiku")
	eq(t, haiku.paneCommand("", ""), "cc --model 'haiku'", "cc/haiku 不传 --effort")
	eq(t, posixQuote(""), "''", "空串")
}

func TestSafeName(t *testing.T) {
	eq(t, safeName("anchor-w0:p1"), "anchor-w0_p1", "锁 key")
	eq(t, safeName("a b/../c"), "a_b_.._c", "路径分隔符")
	eq(t, safeName("中"), "___", "非 ASCII 逐字节")
}

func TestRenderRoleBlocks(t *testing.T) {
	tpl := "head\n<!-- team-rule:advisor -->\nA\n<!-- /team-rule:advisor -->\n<!-- team-rule:none -->\nN\n<!-- /team-rule:none -->\n<!-- mid-task -->\nM\n<!-- /mid-task -->\ntail"
	eq(t, renderRole(tpl, "advisor", false), "head\nA\ntail\n", "advisor 变体")
	eq(t, renderRole(tpl, "none", true), "head\nN\nM\ntail\n", "none + mid")
}

func TestValidateRejectsLooseStates(t *testing.T) {
	good := `{"schema_version":1,"team_id":"dm_x","prefix":"dm_x","anchor":{"pane_id":"p"},"phase":"launched","pending":null,
"roles":{"owner":{"name":"dm_x_design","engine":"cc","instance":1,"launched":true,"inited":false,"pane":"p1","identity":null,"launch_identity":null},
"worker":{"name":"dm_x_execute","engine":"cc","instance":1,"launched":true,"inited":false,"pane":"p2"},"advisor":null}}`
	if err := validateTeam([]byte(good)); err != nil {
		t.Fatalf("基线应合法：%v", err)
	}
	bad := map[string]string{
		"phase 非法":           strings.Replace(good, `"launched","pending"`, `"weird","pending"`, 1),
		"pending 无 delivery": strings.Replace(good, `"pending":null`, `"pending":{"step":"x"}`, 1),
		"identity 缺 kind":    strings.Replace(good, `"identity":null`, `"identity":{"terminal_id":"t","pane_id":"p1","session":""}`, 1),
		"identity pane 不符":   strings.Replace(good, `"identity":null`, `"identity":{"terminal_id":"t","pane_id":"zz","kind":"claude"}`, 1),
		"launched 缺失":        strings.Replace(good, `"launched":true,"inited":false,"pane":"p2"`, `"inited":false,"pane":"p2"`, 1),
		"worker 为 null":      strings.Replace(good, `"worker":{"name":"dm_x_execute","engine":"cc","instance":1,"launched":true,"inited":false,"pane":"p2"}`, `"worker":null`, 1),
		"launched 阶段 pane 缺": strings.Replace(good, `"pane":"p2"`, `"pane":null`, 1),
	}
	for name, s := range bad {
		if validateTeam([]byte(s)) == nil {
			t.Errorf("%s 应被拒绝", name)
		}
	}
}

func TestPendingGuardAndStoreRoundTrip(t *testing.T) {
	e := newEnv(t)
	e.config("codex")
	e.mustRun("up")
	a := &app{stdout: &syncBuf{}, stderr: &syncBuf{}, root: e.repoRoot, st: newStore()}
	tm, err := a.st.load(e.teamID())
	if err != nil {
		t.Fatal(err)
	}
	tm.setPending("owner", "prompt", "SENT")
	eq(t, a.pendingGuard(tm), true, "SENT 放行并清除")
	if tm.Pending != nil {
		t.Fatal("SENT 应清除 pending")
	}
	tm.setPending("owner", "prompt", "NOT_SENT")
	eq(t, a.pendingGuard(tm), false, "非 SENT 阻止")
	eq(t, tm.Pending.Delivery, "UNKNOWN", "标 UNKNOWN")
	reloaded, err := a.st.load(tm.TeamID)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, reloaded.Pending != nil && reloaded.Pending.Delivery == "UNKNOWN", true, "UNKNOWN 已持久化")
}

// cc 的 session 由 SessionStart hook 在进程被识别后才上报：启动观测须等到 session 出现，
// 否则 init 首次冻结会把空→非空判为指纹漂移。
func TestCaptureIdentityWaitsForLateSession(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "herdr")
	os.WriteFile(bin, []byte(`#!/bin/sh
n=$(cat "$0.count" 2>/dev/null || echo 0); n=$((n+1)); echo "$n" > "$0.count"
sess=null; [ "$n" -ge 3 ] && sess='{"value":"sess-late"}'
printf '{"result":{"agent":{"name":"","pane_id":"w0:p2","terminal_id":"term_p2","agent":"claude","agent_session":%s}}}\n' "$sess"
`), 0o755)
	t.Setenv("HERDR_BIN_PATH", bin)
	t.Setenv("NAKAMA_START_POLL_TRIES", "6")
	a := &app{stdout: os.Stdout, stderr: os.Stderr, st: store{dir: dir}}
	tm := &Team{TeamID: "t1", Roles: Roles{Worker: &Role{Name: "x_execute", Engine: "cc", Pane: "w0:p2"}}}
	if !a.captureIdentity(tm, "worker") {
		t.Fatal("captureIdentity 应成功")
	}
	eq(t, tm.Roles.Worker.LaunchIdentity.Session, "sess-late", "启动观测等到迟到的 session")
}

// 恢复已有重绑计划时，确认参数只能回显计划里已确认的值；任何不同都拒绝，不把新值套到旧计划上。
func TestCheckPlanConfirm(t *testing.T) {
	p := &RebindPlan{
		Steps:             []RebindStep{{Role: "owner", New: Ident{Session: "s-owner"}}, {Role: "worker", New: Ident{Session: "s-worker"}}},
		AnchorNew:         "term_new",
		AnchorIdentityNew: &Ident{Session: "s-anchor"},
	}
	ok := map[string]rebindOpts{
		"无确认参数":  {},
		"回显单个角色": {confirm: map[string]string{"owner": "s-owner"}},
		"回显全部":   {confirm: map[string]string{"owner": "s-owner", "worker": "s-worker", "anchor": "s-anchor"}, confirmAnchor: "term_new"},
	}
	for name, o := range ok {
		eq(t, checkPlanConfirm(p, o), "", name+"：回显计划值应放行")
	}
	bad := map[string]rebindOpts{
		"角色 session 不同":  {confirm: map[string]string{"owner": "other"}},
		"计划里没有的角色":       {confirm: map[string]string{"advisor": "s-owner"}},
		"锚点 session 不同":  {confirm: map[string]string{"anchor": "other"}},
		"锚点 terminal 不同": {confirmAnchor: "term_other"},
		"跨角色借用 session":  {confirm: map[string]string{"worker": "s-owner"}},
	}
	for name, o := range bad {
		if checkPlanConfirm(p, o) == "" {
			t.Errorf("%s：应拒绝", name)
		}
	}
	if checkPlanConfirm(&RebindPlan{}, rebindOpts{confirm: map[string]string{"anchor": "s"}}) == "" {
		t.Error("计划没有锚点身份时，确认锚点 session 应拒绝")
	}
}

func TestStartArgs(t *testing.T) {
	cx, _ := parseEngine("codex/gpt-6.1-sol:max")
	eq(t, strings.Join(cx.startArgs(), " "),
		`--no-daemon --model gpt-6.1-sol -c model_reasoning_effort="max" --dangerously-bypass-approvals-and-sandbox --no-alt-screen`, "codex 参数")
	qd, _ := parseEngine("qoder/Qwen3.8-Flash")
	eq(t, strings.Join(qd.startArgs(), " "), "--model Qwen3.8-Flash --permission-mode bypass_permissions", "qoder 参数")
	for in, kind := range map[string]string{"codex/m": "codex", "qoder/m": "qoder", "ccenv/m": "ccenv"} {
		e, _ := parseEngine(in)
		if e.hint() == "" {
			t.Errorf("%s 缺少启动提示", kind)
		}
	}
}
