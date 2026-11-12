package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// h2 验收：审查修复（F1 locale 基线、F2 --instance 示例）、同 tab 约束、以及 h1 验收缺口 3c/3d/3e/3h/3j/3k。
// 旧状态 fixture 全部手写 JSON（不经 v4 helper 反推）。

// ---- F1：默认组映射与基线逐字一致（zh/en/ja × 1W+0A、1W+1A）。期望值由基线 772ce73 的目录格式串展开得到。----
func TestMappingMatchesBaselineAcrossLocales(t *testing.T) {
	cases := map[string]struct {
		lang, want string
		withAdv    bool
	}{
		"zh 无Advisor": {"zh", "任务=dm_x；Owner=dm_x_design，pane=p1，cc；执行者=dm_x_execute，pane=p2，cc；无独立 Advisor。消息实例：Owner=O1；Worker=W1；Advisor=无", false},
		"zh 有Advisor": {"zh", "任务=dm_x；Owner=dm_x_design，pane=p1，cc；执行者=dm_x_execute，pane=p2，cc；独立 Advisor=dm_x_advisor，pane=p3，cc。消息实例：Owner=O1；Worker=W1；Advisor=A1", true},
		"en 无Advisor": {"en", "Task=dm_x; Owner=dm_x_design, pane=p1, cc; Worker=dm_x_execute, pane=p2, cc; no independent Advisor.Message instances: Owner=O1; Worker=W1; Advisor=none", false},
		"en 有Advisor": {"en", "Task=dm_x; Owner=dm_x_design, pane=p1, cc; Worker=dm_x_execute, pane=p2, cc; independent Advisor=dm_x_advisor, pane=p3, cc.Message instances: Owner=O1; Worker=W1; Advisor=A1", true},
		"ja 无Advisor": {"ja", "タスク=dm_x；Owner=dm_x_design、pane=p1、cc；Worker=dm_x_execute、pane=p2、cc；独立 Advisor なし。メッセージインスタンス：Owner=O1；Worker=W1；Advisor=なし", false},
		"ja 有Advisor": {"ja", "タスク=dm_x；Owner=dm_x_design、pane=p1、cc；Worker=dm_x_execute、pane=p2、cc；独立 Advisor=dm_x_advisor、pane=p3、cc。メッセージインスタンス：Owner=O1；Worker=W1；Advisor=A1", true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("NAKAMA_LANG", c.lang)
			tm := &Team{Prefix: "dm_x", Roles: map[string]*Role{
				"O1": {Name: "dm_x_design", Pane: "p1", Engine: "cc", Instance: 1},
				"W1": {Name: "dm_x_execute", Pane: "p2", Engine: "cc", Instance: 1},
			}}
			if c.withAdv {
				tm.Roles["A1"] = &Role{Name: "dm_x_advisor", Pane: "p3", Engine: "cc", Instance: 1}
			}
			eq(t, tm.mapping(), c.want, "与 772ce73 基线逐字一致")
		})
	}
}

// ---- F2：--instance 错误示例必须可被 CLI 自己解析并成功执行 ----
func TestInstanceHintIsExecutable(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up", "--workers", "2", "--advisor", "cc")
	id := e.teamID()
	e.mustRun("init", "--team", id)
	out := e.mustFail("drop", "--team", id, "--role", "worker")
	has(t, out, "--instance 指定实例（如 1）", "示例为数字实例号")
	e.mustRun("drop", "--team", id, "--role", "worker", "--instance", "1")
	eq(t, e.str("roles", "W1"), "<nil>", "按提示执行成功")
}

// ---- 3c：新建组数量提问 ----
func TestPickCountPrompts(t *testing.T) {
	t.Run("默认回车 1+none", func(t *testing.T) {
		e := newEnv(t)
		e.config("normal")
		e.anchorEnv(t)
		withTTY(t, true)
		ok, out := e.pick("\n\n\n\ny\n")
		if !ok {
			t.Fatalf("默认应成功：\n%s", out)
		}
		has(t, out, "Worker  pi/model-x:low ×1", "摘要 Worker ×1")
		hasNot(t, out, "Advisor 数量", "advisor none 不问数量")
		eq(t, e.count("pane", "split"), 2, "owner + 1 worker")
	})
	t.Run("输入 2 与 3", func(t *testing.T) {
		e := newEnv(t)
		e.config("normal")
		e.anchorEnv(t)
		withTTY(t, true)
		ok, out := e.pick("\n\n2\ncc\n3\ny\n")
		if !ok {
			t.Fatalf("应成功：\n%s", out)
		}
		has(t, out, "Worker  pi/model-x:low ×2", "摘要 Worker ×2")
		has(t, out, "Advisor cc ×3", "摘要 Advisor ×3")
		eq(t, e.count("pane", "split"), 6, "owner + 2 worker + 3 advisor")
	})
	t.Run("非法输入重问", func(t *testing.T) {
		e := newEnv(t)
		e.config("normal")
		e.anchorEnv(t)
		withTTY(t, true)
		ok, out := e.pick("\n\n0\n5\nabc\n1\nnone\ny\n")
		if !ok {
			t.Fatalf("重问后应成功：\n%s", out)
		}
		eq(t, strings.Count(out, "数量须为 1-4 的整数"), 3, "0/5/abc 各重问一次")
		eq(t, e.count("pane", "split"), 2, "最终 1 worker")
	})
	t.Run("EOF 取消零副作用", func(t *testing.T) {
		e := newEnv(t)
		e.config("normal")
		e.anchorEnv(t)
		withTTY(t, true)
		ok, _ := e.pick("\n\n")
		eq(t, ok, false, "数量提问 EOF 应取消")
		e.noSideEffects(t)
	})
}

// ---- 3d：多 Worker 中 W2 launch 失败：pending UNKNOWN、不继续、重跑不重发 ----
func TestMultiWorkerLaunchFailureMarksUnknownNoResend(t *testing.T) {
	e := newEnv(t)
	e.config("codex")
	t.Setenv("NAKAMA_FAKE_START_FAIL_SUFFIX", "_execute2")
	has(t, e.mustFail("up", "--workers", "2"), "Worker 启动失败", "W2 启动失败即停止")
	eq(t, e.str("pending", "role"), "W2", "pending 指向 W2")
	eq(t, e.str("pending", "delivery"), "UNKNOWN", "pending UNKNOWN")
	starts := e.count("agent", "start")
	eq(t, starts, 3, "owner、W1、W2 各尝试一次，之后不再继续")
	has(t, e.mustFail("up", "--workers", "2"), "未确认副作用", "重跑拒绝而非重发")
	eq(t, e.count("agent", "start"), starts, "重跑不重发 start")
}

// ---- ①②③：同 tab 约束 ----
func TestNewPanesStayInAnchorTab(t *testing.T) {
	t.Run("① 正常多成员布局无 tab/window 命令", func(t *testing.T) {
		e := newEnv(t)
		e.config("normal")
		e.mustRun("up", "--workers", "2", "--advisor", "cc", "--advisors", "2")
		eq(t, e.count("pane", "split"), 5, "5 次 split")
		for _, v := range e.argvs() {
			if len(v) > 0 && (v[0] == "tab" || v[0] == "window") {
				t.Fatalf("不得创建 tab/window：%v", v)
			}
		}
	})
	t.Run("② base pane 在别的 tab → 零 split 拒绝", func(t *testing.T) {
		e := newEnv(t)
		e.config("normal")
		e.mustRun("up")
		e.mustRun("init", "--team", e.teamID())
		before := e.count("pane", "split")
		t.Setenv("FAKE_OTHER_TAB_PANE", e.str("roles", "W1", "pane"))
		has(t, e.mustFail("add", "--team", e.teamID(), "--role", "worker"), "不在锚点所在的 workspace/tab 内", "拒绝")
		eq(t, e.count("pane", "split"), before, "零新 split")
	})
	t.Run("③ split 后新 pane 在别的 tab → UNKNOWN 停止、不启动、不重发", func(t *testing.T) {
		e := newEnv(t)
		e.config("normal")
		e.mustRun("up")
		id := e.teamID()
		e.mustRun("init", "--team", id)
		// 下一个 split 产生的 pane 为 w0:p(9+已有 split 数+1)。
		next := "w0:p" + strconv.Itoa(9+e.count("pane", "split")+1)
		t.Setenv("FAKE_OTHER_TAB_PANE", next)
		splits, runs, starts := e.count("pane", "split"), e.count("pane", "run"), e.count("agent", "start")
		has(t, e.mustFail("add", "--team", id, "--role", "advisor", "--engine", "cc"), "不在锚点所在的 workspace/tab 内", "新 pane 越 tab 拒绝")
		eq(t, e.str("pending", "step"), "split", "pending 停在 split")
		eq(t, e.str("pending", "delivery"), "UNKNOWN", "标 UNKNOWN")
		eq(t, e.count("pane", "run")+e.count("agent", "start"), runs+starts, "不启动 agent")
		has(t, e.mustFail("add", "--team", id, "--role", "advisor", "--engine", "cc"), "未确认副作用", "重跑不重发")
		eq(t, e.count("pane", "split"), splits+1, "重跑不新增 split")
	})
}

// ---- 3e：手写的 schema 2 / 3 真实旧格式 fixture ----

const v3TeamJSON = `{
 "schema_version": 3,
 "team_id": "dm_fx0001",
 "prefix": "dm_fx0001",
 "anchor": {"pane_id": "w0:p1", "workspace_id": "w0", "tab_id": "w0:t1", "cwd": %[1]q, "terminal_id": "term_w0:p1", "identity": null},
 "cwd": %[1]q,
 "roles": {
  "owner": {"name": "dm_fx0001_design", "pane": "w0:p10", "engine": "codex/gpt-6.1-sol:high", "instance": 1, "launched": true, "inited": true, "pane_terminal": "term_w0:p10", "launch_identity": null,
            "identity": {"terminal_id": "term_w0:p10", "pane_id": "w0:p10", "kind": "codex", "session": "sess-owner"}},
  "worker": {"name": "dm_fx0001_execute2", "pane": "w0:p12", "engine": "pi/model-x:low", "instance": 2, "launched": true, "inited": true, "pane_terminal": "term_w0:p12", "launch_identity": null,
             "identity": {"terminal_id": "term_w0:p12", "pane_id": "w0:p12", "kind": "pi", "session": ""}},
  "advisor": null
 },
 "team_rev": 3,
 "protocol_version": "0000000000000000000000000000000000000000000000000000000000000000",
 "context": null,
 "phase": "initialized",
 "pending": %[2]s,
 "rebind_pending": %[3]s,
 "rebinds": [],
 "retired": [{"role": "worker", "name": "dm_fx0001_execute", "pane": "w0:p11", "engine": "pi/model-x:low", "instance": 1, "at": "2026-01-01T00:00:00Z", "op": "swap"}]
}`

const v3PlanJSON = `{
 "op_id": "1700000001", "at": "2026-01-01T00:00:00Z",
 "steps": [
  {"role": "owner", "pane": "w0:p10", "name": "dm_fx0001_design", "current_name": "dm_fx0001_design", "rename": true, "result": "done",
   "old": {"terminal_id": "term_w0:p10", "pane_id": "w0:p10", "kind": "codex", "session": "sess-owner"},
   "new": {"terminal_id": "term_w0:p10", "pane_id": "w0:p10", "kind": "codex", "session": "sess-owner"}},
  {"role": "worker", "pane": "w0:p12", "name": "dm_fx0001_execute2", "current_name": "", "rename": true, "result": "started",
   "old": {"terminal_id": "term_w0:p12", "pane_id": "w0:p12", "kind": "pi", "session": ""},
   "new": {"terminal_id": "term_new_w0:p12", "pane_id": "w0:p12", "kind": "pi", "session": "sess-worker"}}
 ],
 "anchor_old_terminal": "term_w0:p1", "anchor_new_terminal": "term_new_anchor",
 "confirm": %[1]s
}`

const v2TeamJSON = `{
 "schema_version": 2,
 "team_id": "dm_fx0002",
 "prefix": "dm_fx0002",
 "anchor": {"pane_id": "w0:p1", "workspace_id": "w0", "tab_id": "w0:t1", "cwd": %[1]q, "terminal_id": "term_w0:p1", "identity": null},
 "cwd": %[1]q,
 "roles": {
  "owner": {"name": "dm_fx0002_design", "pane": "w0:p10", "engine": "codex/gpt-6.1-sol:high", "instance": 1, "launched": true, "inited": true, "pane_terminal": "term_w0:p10", "launch_identity": null,
            "identity": {"terminal_id": "term_w0:p10", "pane_id": "w0:p10", "kind": "codex", "session": "sess-owner"}},
  "worker": {"name": "dm_fx0002_execute", "pane": "w0:p11", "engine": "pi/model-x:low", "instance": 1, "launched": true, "inited": true, "pane_terminal": "term_w0:p11", "launch_identity": null,
             "identity": {"terminal_id": "term_w0:p11", "pane_id": "w0:p11", "kind": "pi", "session": ""}},
  "advisor": null
 },
 "team_rev": 2,
 "protocol_version": "0000000000000000000000000000000000000000000000000000000000000000",
 "context": null,
 "phase": "initialized",
 "pending": null
}`

// fixtureDir 返回与状态文件同一真实路径的项目目录（符号链接已解析，与 pane get 的 cwd 比较一致）。
func fixtureCWD(t *testing.T, e *tenv) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(e.proj)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func writeFixture(t *testing.T, id, body string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "teams")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fixtureBytes(t *testing.T, id string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "teams", id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// 假 herdr 的当前观测与 v3 计划的 new 身份一致：owner 不变，worker 为新 terminal 且带 session。
func seedV3Rows(e *tenv) {
	e.rowSet("dm_fx0001_design", "w0:p10", "term_w0:p10", "codex", "sess-owner")
	e.rowSet("@w0:p12", "w0:p12", "term_new_w0:p12", "pi", "sess-worker")
	t := e.t
	t.Setenv("FAKE_ANCHOR_TERM", "term_new_anchor")
	t.Setenv("NAKAMA_FAKE_RENAME_INPLACE", "1")
}

func TestV2V3LegacyStateReadOnlyThenMigrates(t *testing.T) {
	t.Run("schema 2 只读命令不改写文件字节", func(t *testing.T) {
		e := newEnv(t)
		cwd := fixtureCWD(t, e)
		writeFixture(t, "dm_fx0002", strings.NewReplacer("%[1]q", "\""+cwd+"\"").Replace(v2TeamJSON))
		before := fixtureBytes(t, "dm_fx0002")
		e.mustRun("status", "--team", "dm_fx0002")
		eq(t, string(fixtureBytes(t, "dm_fx0002")), string(before), "status 不写 schema 2 文件")
	})

	t.Run("v3 含 rebind_pending：只读命令不改字节，确认后恢复、done 不重发、首次写为 schema 4", func(t *testing.T) {
		e := newEnv(t)
		cwd := fixtureCWD(t, e)
		seedV3Rows(e)
		body := strings.NewReplacer("%[1]q", "\""+cwd+"\"").Replace(v3TeamJSON)
		body = strings.Replace(body, "%[2]s", "null", 1)
		body = strings.Replace(body, "%[3]s", strings.NewReplacer("%[1]s", `["worker:session", "anchor:terminal"]`).Replace(v3PlanJSON), 1)
		writeFixture(t, "dm_fx0001", body)
		before := fixtureBytes(t, "dm_fx0001")

		e.mustRun("status", "--team", "dm_fx0001")
		e.mustRun("rebind", "--team", "dm_fx0001", "--preflight")
		eq(t, string(fixtureBytes(t, "dm_fx0001")), string(before), "status/preflight 不改字节")

		renames := e.count("agent", "rename")
		e.mustRun("rebind", "--team", "dm_fx0001", "--confirm-anchor", "term_new_anchor")
		eq(t, e.count("agent", "rename")-renames, 1, "只改 worker（done 的 owner 不重发）")
		eq(t, e.str("schema_version"), "4", "首次实际写入为 schema 4")
		eq(t, e.str("roles", "W2", "identity", "session"), "sess-worker", "非首实例 W2 迁移后确认生效")
		eq(t, e.str("rebind_pending"), "<nil>", "计划已提交")
		has(t, string(fixtureBytes(t, "dm_fx0001")), `"W2:session"`, "旧 worker:session 迁移为 W2:session")
		has(t, string(fixtureBytes(t, "dm_fx0001")), `"anchor:terminal"`, "anchor 确认原样保留")
	})

	t.Run("v3 计划缺失 worker 确认：拒绝且不写", func(t *testing.T) {
		e := newEnv(t)
		cwd := fixtureCWD(t, e)
		seedV3Rows(e)
		body := strings.NewReplacer("%[1]q", "\""+cwd+"\"").Replace(v3TeamJSON)
		body = strings.Replace(body, "%[2]s", "null", 1)
		body = strings.Replace(body, "%[3]s", strings.NewReplacer("%[1]s", `["anchor:terminal"]`).Replace(v3PlanJSON), 1)
		writeFixture(t, "dm_fx0001", body)
		before := fixtureBytes(t, "dm_fx0001")
		has(t, e.mustFail("rebind", "--team", "dm_fx0001", "--confirm-anchor", "term_new_anchor"), "session 补全缺少确认", "缺确认拒绝")
		eq(t, string(fixtureBytes(t, "dm_fx0001")), string(before), "拒绝不写")
	})

	t.Run("v3 计划与冲突确认：拒绝且不写", func(t *testing.T) {
		e := newEnv(t)
		cwd := fixtureCWD(t, e)
		seedV3Rows(e)
		body := strings.NewReplacer("%[1]q", "\""+cwd+"\"").Replace(v3TeamJSON)
		body = strings.Replace(body, "%[2]s", "null", 1)
		body = strings.Replace(body, "%[3]s", strings.NewReplacer("%[1]s", `["worker:session", "anchor:terminal"]`).Replace(v3PlanJSON), 1)
		writeFixture(t, "dm_fx0001", body)
		before := fixtureBytes(t, "dm_fx0001")
		e.mustFail("rebind", "--team", "dm_fx0001", "--confirm-anchor", "term_new_anchor", "--confirm-session", "W2=sess-other")
		eq(t, string(fixtureBytes(t, "dm_fx0001")), string(before), "冲突确认拒绝不写")
	})

	t.Run("v3 pending UNKNOWN 迁移后仍 UNKNOWN，不被清掉", func(t *testing.T) {
		e := newEnv(t)
		cwd := fixtureCWD(t, e)
		body := strings.NewReplacer("%[1]q", "\""+cwd+"\"").Replace(v3TeamJSON)
		body = strings.Replace(body, "%[2]s", `{"op_id": "9", "role": "worker", "step": "prompt", "delivery": "UNKNOWN"}`, 1)
		body = strings.Replace(body, "%[3]s", "null", 1)
		writeFixture(t, "dm_fx0001", body)
		before := fixtureBytes(t, "dm_fx0001")
		has(t, e.mustRun("status", "--team", "dm_fx0001"), "pending: step=prompt delivery=UNKNOWN", "status 显示 UNKNOWN")
		eq(t, string(fixtureBytes(t, "dm_fx0001")), string(before), "status 不清 UNKNOWN")
		e.mustFail("rebind", "--team", "dm_fx0001", "--confirm-anchor", "term_new_anchor")
		eq(t, string(fixtureBytes(t, "dm_fx0001")), string(before), "rebind 被 UNKNOWN 拒绝且不写")
	})
}

// ---- 3h：rebind 成员键、别名歧义与计划覆盖（纯函数层面，覆盖多 Worker 场景）----
func TestRebindMemberKeyAliasAndPlanCoverage(t *testing.T) {
	mk := func() *Team {
		return &Team{Roles: map[string]*Role{
			"O1": {Name: "d_design", Pane: "p1", Instance: 1},
			"W1": {Name: "d_execute", Pane: "p2", Instance: 1},
			"W2": {Name: "d_execute2", Pane: "p3", Instance: 2},
		}}
	}
	t.Run("成员键直接解析", func(t *testing.T) {
		o := rebindOpts{confirm: map[string]string{"W2": "s"}}
		eq(t, normalizeConfirm(mk(), &o), nil, "成员键合法")
		eq(t, o.confirm["W2"], "s", "保持成员键")
	})
	t.Run("多 Worker 时旧别名 worker= 报歧义", func(t *testing.T) {
		o := rebindOpts{confirm: map[string]string{"worker": "s"}}
		has(t, errString(normalizeConfirm(mk(), &o)), "不唯一", "歧义拒绝")
	})
	t.Run("别名与成员键同一成员取值冲突即拒", func(t *testing.T) {
		tm := &Team{Roles: map[string]*Role{"O1": {Name: "d_design", Pane: "p1", Instance: 1}, "W1": {Name: "d_execute", Pane: "p2", Instance: 1}}}
		o := rebindOpts{confirm: map[string]string{"worker": "a", "W1": "b"}}
		has(t, errString(normalizeConfirm(tm, &o)), "冲突", "冲突拒绝")
	})
	t.Run("计划缺 W2 被拒", func(t *testing.T) {
		tm := mk()
		tm.Anchor = Anchor{PaneID: "p0", TerminalID: "term_a"}
		plan := &RebindPlan{OpID: "1", AnchorOld: "term_a", AnchorNew: "term_a", Steps: []RebindStep{
			{Role: "O1", Name: "d_design", Pane: "p1", Old: Ident{TerminalID: "t1", PaneID: "p1", Kind: "codex", Session: "s"}, New: Ident{TerminalID: "t1", PaneID: "p1", Kind: "codex", Session: "s"}},
			{Role: "W1", Name: "d_execute", Pane: "p2", Old: Ident{TerminalID: "t2", PaneID: "p2", Kind: "pi", Session: "s"}, New: Ident{TerminalID: "t2", PaneID: "p2", Kind: "pi", Session: "s"}},
		}}
		tm.Roles["O1"].Identity = &plan.Steps[0].Old
		tm.Roles["W1"].Identity = &plan.Steps[1].Old
		tm.Roles["W2"].Identity = &Ident{TerminalID: "t3", PaneID: "p3", Kind: "pi", Session: "s"}
		has(t, errString(validatePlanAgainstTeam(tm, plan)), "计划缺少角色 W2", "缺成员拒绝")
	})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// ---- 3j：init 从 W2 / A2 pane 自动定位；已停用 pane 不命中 ----
func TestInitLocatesSecondMembersAndRetiredPaneNeverMatches(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up", "--workers", "2", "--advisor", "cc", "--advisors", "2")
	id := e.teamID()
	e.mustRun("init", "--team", id)
	w2 := e.str("roles", "W2", "pane")
	a2 := e.str("roles", "A2", "pane")
	e.mustRun("init", "--pane", w2)
	e.mustRun("init", "--pane", a2)
	e.mustRun("drop", "--team", id, "--role", "worker", "--instance", "2")
	e.mustRun("init", "--team", id)
	has(t, e.mustFail("init", "--pane", w2), "不属于任何编组", "已停用 pane 不命中")
}

// ---- 3k：clean 的 tasksIdle 覆盖第二个 Worker / Advisor ----
func TestCleanRefusesWhenSecondMemberWorking(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up", "--workers", "2", "--advisor", "cc", "--advisors", "2")
	id := e.teamID()
	e.mustRun("init", "--team", id)
	dir := e.str("task_dir")
	os.MkdirAll(filepath.Join(dir, "op_1"), 0o700)
	exists := func() bool { _, err := os.Lstat(filepath.Join(dir, "op_1")); return err == nil }

	for _, sfx := range []string{"_execute2", "_advisor2"} {
		t.Setenv("FAKE_WORKING_SUFFIX", sfx)
		out := e.mustFail("clean", "--team", id, "--all", "--yes")
		has(t, out, "working", "working 时拒绝清理")
		eq(t, exists(), true, "未删除任务目录")
	}
	t.Setenv("FAKE_WORKING_SUFFIX", "")
	e.mustRun("clean", "--team", id, "--all", "--yes")
	eq(t, exists(), false, "全部空闲后可清理")
}

// ---- add 达上限被拒 ----
func TestAddRejectsAtMemberLimit(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up", "--workers", "4", "--advisor", "cc", "--advisors", "4")
	id := e.teamID()
	e.mustRun("init", "--team", id)
	has(t, e.mustFail("add", "--team", id, "--role", "worker"), "已达上限", "第 5 个 Worker 拒绝")
	has(t, e.mustFail("add", "--team", id, "--role", "advisor", "--engine", "cc"), "已达上限", "第 5 个 Advisor 拒绝")
}

// F4：split 返回后、同 tab 证实前中断：落盘为 UNKNOWN，重跑 up/add/init 均停止，不 launch、不再 split。
func TestSplitInterruptedBeforeTabVerificationStaysUnknown(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	id := e.teamID()
	e.mustRun("init", "--team", id)
	before := struct{ splits, runs, starts int }{e.count("pane", "split"), e.count("pane", "run"), e.count("agent", "start")}

	var out, errb bytes.Buffer
	a := &app{stdout: &out, stderr: &errb, root: e.repoRoot, st: newStore()}
	a.hookAfterSplitPersist = func(string) bool { return true } // 模拟进程在 pane ID 落盘后、tab 核验前中断
	if err := a.dispatch([]string{"add", "--team", id, "--role", "advisor", "--engine", "cc"}); err == nil {
		t.Fatalf("中断窗口应返回非零：\n%s", out.String()+errb.String())
	}
	a.unlockAll()
	eq(t, e.str("pending", "step"), "split", "pending 停在 split")
	eq(t, e.str("pending", "delivery"), "UNKNOWN", "中断后仍为 UNKNOWN，不是 SENT")
	eq(t, e.count("pane", "split"), before.splits+1, "只发生一次 split")

	has(t, e.mustFail("up"), "未确认的副作用", "重跑 up 停止")
	has(t, e.mustFail("add", "--team", id, "--role", "advisor", "--engine", "cc"), "未确认的副作用", "重跑 add 停止")
	has(t, e.mustFail("init", "--team", id), "未确认的副作用", "init 停止")
	eq(t, e.str("pending", "delivery"), "UNKNOWN", "重跑后仍为 UNKNOWN")
	eq(t, e.count("pane", "split"), before.splits+1, "重跑零额外 split")
	eq(t, e.count("pane", "run")+e.count("agent", "start"), before.runs+before.starts, "重跑零 launch")
}
