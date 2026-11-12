package main

import "testing"

// 恢复计划前必须整组复核；持久化计划必须与编组记录跨字段一致。全部在假 herdr 上验证。

// interruptBefore 在指定角色的 rename 前触发停止，留下未完成的计划（已完成的步骤保持 done）。
func interruptBefore(t *testing.T, e *tenv, id, role string, extra ...string) {
	t.Helper()
	args := append([]string{"--team", id, "--confirm-anchor", "term_new_anchor"}, extra...)
	err, out := e.rebindInProc(func(a *app) {
		a.hookBeforeRename = func(r string) {
			if r == role {
				a.stop.Store(true)
			}
		}
	}, args...)
	if err == nil {
		t.Fatalf("应在 %s 前中断：\n%s", role, out)
	}
}

// planSteps 返回持久化计划中 steps 的可修改切片。
func planSteps(m map[string]any) []any {
	return m["rebind_pending"].(map[string]any)["steps"].([]any)
}

func TestRebindResumeRevalidatesWholePlan(t *testing.T) {
	t.Run("中断后锚点 cwd 漂移：恢复零新增 rename 并拒绝", func(t *testing.T) {
		e, id := rebindSetup(t, true, false)
		restart(e, restartOpt{auto: true})
		t.Setenv("FAKE_ANCHOR_TERM", "term_new_anchor")
		interruptBefore(t, e, id, "O1")
		t.Setenv("FAKE_ANCHOR_CWD", e.proj+"/different")
		before, renames := readState(t, e), e.count("agent", "rename")
		ok, out := e.run("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
		if ok {
			t.Fatalf("上下文漂移应拒绝：\n%s", out)
		}
		eq(t, e.count("agent", "rename"), renames, "零新增 rename")
		eq(t, string(readState(t, e)), string(before), "计划与当前 identity 原样保留")
	})

	t.Run("owner 已 done 后其 session 变化：恢复在 rename worker 前即失败", func(t *testing.T) {
		e, id := rebindSetup(t, true, false)
		restart(e, restartOpt{auto: true})
		t.Setenv("FAKE_ANCHOR_TERM", "term_new_anchor")
		interruptBefore(t, e, id, "W1")
		eq(t, stateAt(e, "rebind_pending", "steps", 0, "result"), "done", "owner 已完成")
		name, pane := e.str("roles", "O1", "name"), e.str("roles", "O1", "pane")
		e.rowSet(name, pane, "term_new_"+pane, "codex", "different-session")
		before, renames := readState(t, e), e.count("agent", "rename")
		ok, out := e.run("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
		if ok {
			t.Fatalf("已完成角色漂移应拒绝：\n%s", out)
		}
		eq(t, e.count("agent", "rename"), renames, "worker 未被改名")
		eq(t, string(readState(t, e)), string(before), "状态不变")
	})

	t.Run("持久化计划只剩 owner 一步：拒绝、零 rename、rev 不变", func(t *testing.T) {
		e, id := rebindSetup(t, true, false)
		restart(e, restartOpt{auto: true})
		t.Setenv("FAKE_ANCHOR_TERM", "term_new_anchor")
		interruptBefore(t, e, id, "O1")
		e.stateSet(func(m map[string]any) {
			p := m["rebind_pending"].(map[string]any)
			p["steps"] = planSteps(m)[:1]
		})
		before, renames := readState(t, e), e.count("agent", "rename")
		ok, out := e.run("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
		if ok {
			t.Fatalf("不完整计划应拒绝：\n%s", out)
		}
		eq(t, e.count("agent", "rename"), renames, "零 rename")
		eq(t, e.str("team_rev"), "1", "rev 不变")
		eq(t, string(readState(t, e)), string(before), "状态字节不变")
	})

	t.Run("计划 old 身份与记录不一致：拒绝", func(t *testing.T) {
		e, id := rebindSetup(t, true, false)
		restart(e, restartOpt{auto: true})
		t.Setenv("FAKE_ANCHOR_TERM", "term_new_anchor")
		interruptBefore(t, e, id, "O1")
		e.stateSet(func(m map[string]any) {
			planSteps(m)[0].(map[string]any)["old"].(map[string]any)["terminal_id"] = "term_bogus"
		})
		before, renames := readState(t, e), e.count("agent", "rename")
		ok, out := e.run("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
		if ok {
			t.Fatalf("old 与记录不一致应拒绝：\n%s", out)
		}
		eq(t, e.count("agent", "rename"), renames, "零 rename")
		eq(t, string(readState(t, e)), string(before), "状态字节不变")
	})

	t.Run("计划包含编组不存在的成员 A9：拒绝且不 panic", func(t *testing.T) {
		e, id := rebindSetup(t, true, false)
		restart(e, restartOpt{auto: true})
		t.Setenv("FAKE_ANCHOR_TERM", "term_new_anchor")
		interruptBefore(t, e, id, "O1")
		e.stateSet(func(m map[string]any) {
			p := m["rebind_pending"].(map[string]any)
			extra := map[string]any{}
			for k, v := range planSteps(m)[0].(map[string]any) {
				extra[k] = v
			}
			extra["role"] = "A9" // 格式合法（A 类、实例 9），本组只有 O1/W1，不存在
			p["steps"] = append(planSteps(m), extra)
		})
		before, renames := readState(t, e), e.count("agent", "rename")
		ok, out := e.run("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
		if ok {
			t.Fatal("伪造的成员步骤应拒绝")
		}
		has(t, out, "计划包含编组中不存在或未冻结的角色 A9", "拒绝原因是计划引用了不存在的成员")
		eq(t, e.count("agent", "rename"), renames, "零 rename")
		eq(t, string(readState(t, e)), string(before), "状态字节不变")
	})

	t.Run("session 补全缺少确认记录：拒绝", func(t *testing.T) {
		e, id := rebindSetup(t, false, false) // worker 记录 session 为空
		restart(e, restartOpt{sessions: map[string]string{"W1": "sess-worker"}})
		t.Setenv("FAKE_ANCHOR_TERM", "term_new_anchor")
		interruptBefore(t, e, id, "O1", "--confirm-session", "worker=sess-worker")
		e.stateSet(func(m map[string]any) {
			m["rebind_pending"].(map[string]any)["confirm"] = []any{"anchor:terminal"}
		})
		before, renames := readState(t, e), e.count("agent", "rename")
		ok, _ := e.run("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
		if ok {
			t.Fatal("缺少 session 确认记录应拒绝")
		}
		eq(t, e.count("agent", "rename"), renames, "零 rename")
		eq(t, string(readState(t, e)), string(before), "状态字节不变")
	})
}

func TestRebindCommitRecordsAnchorIdentitySnapshots(t *testing.T) {
	e, id := rebindSetup(t, true, true)
	restart(e, restartOpt{})
	e.paneRowSet("w0:p1", "term_new_anchor", "shell", "sess-anchor")
	e.mustRun("rebind", "--team", id, "--confirm-session", "anchor=sess-anchor")
	eq(t, stateAt(e, "rebinds", 0, "result"), "committed", "审计 committed")
	eq(t, stateAt(e, "rebinds", 0, "anchor_old_identity", "session"), "", "锚点 session 确认前为空")
	eq(t, stateAt(e, "rebinds", 0, "anchor_new_identity", "session"), "sess-anchor", "锚点 session 确认后")
	eq(t, stateAt(e, "rebinds", 0, "anchor_new_identity", "terminal_id"), "term_new_anchor", "锚点新 terminal")
	eq(t, stateAt(e, "rebinds", 0, "steps", 0, "old", "session"), "sess-owner", "逐步审计含旧 session")
}

func TestStatusBoundRequiresRecordedName(t *testing.T) {
	e, id := rebindSetup(t, true, false)
	restart(e, restartOpt{})
	t.Setenv("FAKE_ANCHOR_TERM", "term_new_anchor")
	e.mustRun("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
	has(t, e.mustRun("status", "--team", id), "binding=bound", "改名后绑定有效")

	// 身份一致但任务名已被他人占用：不得显示 bound。
	workerName, workerPane := e.str("roles", "W1", "name"), e.str("roles", "W1", "pane")
	e.rowDel(workerName)
	e.rowSet("someone-else", workerPane, "term_new_"+workerPane, "pi", "sess-worker")
	st := e.mustRun("status", "--team", id)
	has(t, st, "binding=unverified", "他人名称时 worker 至多 unverified")
}

// 任何执行或恢复开始前，当前身份必须等于计划新身份：仍为旧身份时不放行。
func TestRebindRejectsWorkerBackOnOldTerminalBeforeRename(t *testing.T) {
	e, id := rebindSetup(t, true, false)
	restart(e, restartOpt{auto: true})
	t.Setenv("FAKE_ANCHOR_TERM", "term_new_anchor")
	interruptBefore(t, e, id, "O1")
	pane := e.str("roles", "W1", "pane")
	oldTerm := e.str("roles", "W1", "identity", "terminal_id")
	e.rowDel(bridgeAutoName(e.str("cwd"), "term_new_"+pane))
	e.rowSet("@"+pane, pane, oldTerm, "pi", "sess-worker")
	before, renames := readState(t, e), e.count("agent", "rename")
	ok, out := e.run("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
	if ok {
		t.Fatalf("worker 仍为旧身份应拒绝：\n%s", out)
	}
	has(t, out, "仍为旧身份", "明确的旧身份停止原因")
	eq(t, e.count("agent", "rename"), renames, "零新增 rename")
	eq(t, string(readState(t, e)), string(before), "状态字节不变")
}

// 计划中的新 session 为空（损坏计划）且观测也为空：拒绝，不改名，不提交。
func TestRebindRejectsEmptyNewSessionInPlan(t *testing.T) {
	e, id := rebindSetup(t, false, false)
	restart(e, restartOpt{auto: true, sessions: map[string]string{"W1": "sess-confirmed"}})
	t.Setenv("FAKE_ANCHOR_TERM", "term_new_anchor")
	interruptBefore(t, e, id, "O1", "--confirm-session", "worker=sess-confirmed")
	e.stateSet(func(m map[string]any) {
		planSteps(m)[1].(map[string]any)["new"].(map[string]any)["session"] = ""
		m["rebind_pending"].(map[string]any)["confirm"] = []any{"anchor:terminal"}
	})
	pane := e.str("roles", "W1", "pane")
	term := "term_new_" + pane
	e.rowDel(bridgeAutoName(e.str("cwd"), term))
	e.rowSet("@"+pane, pane, term, "pi", "")
	before, renames := readState(t, e), e.count("agent", "rename")
	ok, out := e.run("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
	if ok {
		t.Fatalf("新 session 为空的计划应拒绝：\n%s", out)
	}
	eq(t, e.count("agent", "rename"), renames, "零 rename")
	eq(t, e.str("team_rev"), "1", "rev 不变")
	eq(t, string(readState(t, e)), string(before), "状态字节不变")
}

// 有 identity 的锚点，计划新 session 为空、观测也为空：拒绝，零 rename，状态不变。
func TestRebindRejectsEmptyAnchorIdentitySessionInPlan(t *testing.T) {
	e, id := rebindSetup(t, true, true)
	restart(e, restartOpt{})
	e.paneRowSet("w0:p1", "term_new_anchor", "shell", "sess-anchor")
	err, _ := e.rebindInProc(func(a *app) {
		a.hookBeforeRename = func(string) { a.stop.Store(true) }
	}, "--team", id, "--confirm-session", "anchor=sess-anchor")
	if err == nil {
		t.Fatal("应在 rename 前中断")
	}
	e.stateSet(func(m map[string]any) {
		p := m["rebind_pending"].(map[string]any)
		p["anchor_identity_new"].(map[string]any)["session"] = ""
		p["confirm"] = []any{}
	})
	e.paneRowSet("w0:p1", "term_new_anchor", "shell", "")
	before, renames := readState(t, e), e.count("agent", "rename")
	ok, out := e.run("rebind", "--team", id)
	if ok {
		t.Fatalf("锚点新 session 为空应拒绝：\n%s", out)
	}
	eq(t, e.count("agent", "rename"), renames, "零 rename")
	eq(t, e.str("team_rev"), "1", "rev 不变")
	eq(t, string(readState(t, e)), string(before), "状态字节不变")
}
