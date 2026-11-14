package main

import "testing"

// Qoder（native）与 cc 一样在启动几秒后才上报 session：启动取证要等到它，否则 init 会判身份漂移。
func TestLaunchWaitsForLateSession(t *testing.T) {
	e := newEnv(t)
	e.rawConfig(`{ "default_profile": { "owner": "cc", "worker": "qoder/model-q", "advisor": "none" }, "projects": {} }`)
	t.Setenv("NAKAMA_FAKE_SESSION_AFTER", "3")
	e.mustRun("up")
	eq(t, e.str("roles", "W1", "launch_identity", "session"), "sess-late", "qoder 启动观测带 session")
	has(t, e.mustRun("init", "--team", e.teamID()), "READY", "init 不再判漂移")
}

func TestSlowRecognitionAndResumeAfterLaunchFailure(t *testing.T) {
	e := newEnv(t)
	e.rawConfig(`{ "default_profile": { "owner": "cc", "worker": "cc", "advisor": "none" }, "projects": {} }`)
	t.Setenv("NAKAMA_FAKE_GET_DELAY_N", "12") // 超过旧的 6 次窗口
	e.mustRun("up")

	e2 := newEnv(t)
	e2.config("codex")
	t.Setenv("NAKAMA_FAKE_GET_DELAY_N", "")
	t.Setenv("NAKAMA_FAKE_GET_FAIL", "1")
	has(t, e2.mustFail("up"), "Owner 启动失败", "取证失败")
	eq(t, e2.str("pending", "step"), "identity", "停在取证")
	t.Setenv("NAKAMA_FAKE_GET_FAIL", "")
	e2.mustRun("up")
	eq(t, e2.str("phase"), "launched", "重跑 up 补取证并启动其余成员")
	eq(t, e2.count("agent", "start"), 2, "Owner 不重复启动")
	has(t, e2.mustRun("init", "--team", e2.teamID()), "READY", "可初始化")
}

func TestResolveUnknownInitPrompt(t *testing.T) {
	e := newEnv(t)
	e.config("codex")
	e.mustRun("up")
	id := e.teamID()
	worker := id + "_execute"
	t.Setenv("FAKE_PROMPT_ERROR", "agent_prompt_stalled")
	e.mustFail("init", "--team", id)
	t.Setenv("FAKE_PROMPT_ERROR", "")
	has(t, e.mustRun("status", "--team", id), "nakama resolve --team "+id, "status 给出出口")
	has(t, e.mustFail("init", "--team", id), "未确认副作用", "不自动重发")
	has(t, e.mustFail("resolve", "--team", id), "选一个", "必须表态")

	e.mustRun("resolve", "--team", id, "--not-delivered")
	t.Setenv("FAKE_PROMPT_ERROR", "agent_prompt_stalled")
	e.mustFail("init", "--team", id)
	t.Setenv("FAKE_PROMPT_ERROR", "")
	eq(t, len(e.prompts(worker)), 2, "未送达 → init 重发")

	e.mustRun("resolve", "--team", id, "--delivered")
	eq(t, e.str("roles", "W1", "inited"), "true", "已送达 → 记为已初始化")
	has(t, e.mustRun("init", "--team", id), "READY", "继续初始化其余成员")
	eq(t, len(e.prompts(worker)), 2, "不再重发")
	has(t, e.mustRun("resolve", "--team", id, "--delivered"), "没有待确认", "无 pending 时空操作")

	// split / run 的不明结果不在 resolve 范围。
	e.stateSet(func(m map[string]any) {
		m["pending"] = map[string]any{"op_id": "1", "role": "W1", "step": "run", "delivery": "UNKNOWN"}
	})
	has(t, e.mustFail("resolve", "--team", id, "--delivered"), "不能用 resolve 处理", "拒绝")

	// 记录 session 为空而投递后出现了 session：已送达的证据，不接受 --not-delivered。
	e3 := newEnv(t)
	e3.config("codex")
	e3.mustRun("up")
	t.Setenv("FAKE_PROMPT_ERROR", "agent_prompt_stalled")
	e3.mustFail("init", "--team", e3.teamID())
	t.Setenv("FAKE_PROMPT_ERROR", "")
	e3.rowSet(e3.str("roles", "W1", "name"), e3.str("roles", "W1", "pane"), e3.str("roles", "W1", "identity", "terminal_id"), "codex", "sess-w")
	has(t, e3.mustFail("resolve", "--team", e3.teamID(), "--not-delivered"), "输入已送达", "证据与声明相反时拒绝")
	e3.mustRun("resolve", "--team", e3.teamID(), "--delivered")
	eq(t, e3.str("roles", "W1", "identity", "session"), "sess-w", "补全 session")
	has(t, e3.mustRun("init", "--team", e3.teamID()), "READY", "继续")
}

func TestInitRefusesStartupDialog(t *testing.T) {
	e := newEnv(t)
	e.config("codex")
	e.mustRun("up")
	t.Setenv("FAKE_PANE_TEXT", "  enter confirm · esc skip")
	has(t, e.mustFail("init", "--team", e.teamID()), "停在启动对话框", "拒绝")
	eq(t, e.count("agent", "prompt"), 0, "未发送输入")
	eq(t, e.str("pending"), "<nil>", "不留 pending")
	t.Setenv("FAKE_PANE_TEXT", "")
	t.Setenv("FAKE_PANE_READ_FAIL", "1")
	has(t, e.mustFail("init", "--team", e.teamID()), "无法读取", "屏幕读不到也拒绝")
	eq(t, e.count("agent", "prompt"), 0, "未发送输入")
	t.Setenv("FAKE_PANE_READ_FAIL", "")
	has(t, e.mustRun("init", "--team", e.teamID()), "READY", "对话框处理后可初始化")
}

// 取证已保存但 pending 未清时中断，之后 pane 换了实例：重试不得覆盖基准。
func TestIdentityRetryKeepsSavedBaseline(t *testing.T) {
	e := newEnv(t)
	e.config("codex")
	e.mustRun("up")
	e.stateSet(func(m map[string]any) {
		m["pending"] = map[string]any{"op_id": "1", "role": "W1", "step": "identity", "delivery": "NOT_SENT"}
	})
	e.rowSet(e.str("roles", "W1", "name"), e.str("roles", "W1", "pane"), e.str("roles", "W1", "launch_identity", "terminal_id"), "codex", "other-session")
	has(t, e.mustFail("init", "--team", e.teamID()), "与启动观测不一致", "拒绝")
	eq(t, e.str("roles", "W1", "launch_identity", "session"), "", "基准未被覆盖")
}
