package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// 重绑测试只使用假 herdr（NAKAMA_FAKE_HERDR_LOG 的 argv 记录与 .names 行表），不触碰真实编组或 ~/.local/state。
// 假 herdr 的 rename 在 NAKAMA_FAKE_RENAME_INPLACE 下就地改名，与真实 Herdr 行为一致。

// rebindSetup：up + init 得到稳定组。owner（codex，native）与 worker（pi，手动 rename）的 session 先经第二次 init 回填。
// withWorkerSession=false 时 worker 的记录 session 保持为空，用于验证 --confirm-session 路径。
// anchorIdentity=true 时锚点先登记为 agent（kind shell），用于验证有 Identity 的锚点规则。
func rebindSetup(t *testing.T, withWorkerSession, anchorIdentity bool) (*tenv, string) {
	t.Helper()
	e := newEnv(t)
	t.Setenv("NAKAMA_FAKE_RENAME_INPLACE", "1")
	if anchorIdentity {
		e.paneRowSet("w0:p1", "term_w0:p1", "shell", "")
	}
	e.config("normal")
	e.mustRun("up")
	id := e.teamID()
	e.mustRun("init", "--team", id)
	e.rowSet(e.str("roles", "owner", "name"), e.str("roles", "owner", "pane"), e.str("roles", "owner", "identity", "terminal_id"), "codex", "sess-owner")
	if withWorkerSession {
		e.rowSet(e.str("roles", "worker", "name"), e.str("roles", "worker", "pane"), e.str("roles", "worker", "identity", "terminal_id"), "pi", "sess-worker")
	}
	e.mustRun("init", "--team", id)
	if anchorIdentity {
		e.paneRowSet("w0:p1", "term_w0:p1", "shell", "sess-anchor")
	}
	eq(t, e.str("phase"), "initialized", "setup 应得到稳定组")
	return e, id
}

type restartOpt struct {
	auto     bool              // 任务名改为 Bridge 自动名（否则清空）
	sessions map[string]string // 覆盖观测 session（缺省沿用记录）
	skip     map[string]bool   // 本次保持不变的角色（用于混合变化）
}

// restart 模拟 Herdr/电脑重启：pane、kind 与 session 保持，terminal 全部改变，任务名清空或改为 Bridge 自动名。
// 锚点 terminal 由调用方另行通过 FAKE_ANCHOR_TERM 设置。
func restart(e *tenv, o restartOpt) {
	cwd := e.str("cwd")
	for _, rn := range []string{"owner", "worker"} {
		if o.skip[rn] {
			continue
		}
		pane := e.str("roles", rn, "pane")
		name := e.str("roles", rn, "name")
		kind := e.str("roles", rn, "identity", "kind")
		sess := e.str("roles", rn, "identity", "session")
		if s, ok := o.sessions[rn]; ok {
			sess = s
		}
		newTerm := "term_new_" + pane
		key := "@" + pane
		if o.auto {
			key = bridgeAutoName(cwd, newTerm)
		}
		e.rowDel(name)
		e.rowDel("@" + pane)
		e.rowSet(key, pane, newTerm, kind, sess)
	}
}

// stateAt 读取状态中的嵌套值；整数路径元素按数组下标取值（e.str 只支持对象键）。
func stateAt(e *tenv, path ...any) string {
	var cur any = e.state()
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := cur.(map[string]any)
			if !ok {
				return "<bad>"
			}
			cur = m[k]
		case int:
			l, ok := cur.([]any)
			if !ok || k >= len(l) {
				return "<bad>"
			}
			cur = l[k]
		}
	}
	if cur == nil {
		return "" // 省略的 omitempty 字段按空读取
	}
	if s, ok := cur.(string); ok {
		return s
	}
	return fmt.Sprint(cur)
}

func readState(t *testing.T, e *tenv) []byte {
	t.Helper()
	b, err := os.ReadFile(e.stateFile())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// rebindInProc 在进程内执行 rebind 并允许注入钩子；假 herdr 仍是独立子进程。
func (e *tenv) rebindInProc(hook func(a *app), args ...string) (error, string) {
	var out, errb bytes.Buffer
	a := &app{stdout: &out, stderr: &errb, root: e.repoRoot, st: newStore(), in: bufio.NewReader(strings.NewReader(""))}
	if hook != nil {
		hook(a)
	}
	err := a.cmdRebind(args)
	a.unlockAll()
	return err, out.String() + errb.String()
}

func rowKeyFor(rows []nameRow, pane string) string {
	for _, r := range rows {
		if r.pane == pane {
			return r.key
		}
	}
	return ""
}

func TestRebindRestartedStableTeam(t *testing.T) {
	e, id := rebindSetup(t, true, false)
	ownerPane, workerPane := e.str("roles", "owner", "pane"), e.str("roles", "worker", "pane")
	ownerName, workerName := e.str("roles", "owner", "name"), e.str("roles", "worker", "name")
	origOwnerTerm := e.str("roles", "owner", "identity", "terminal_id")
	restart(e, restartOpt{})
	t.Setenv("FAKE_ANCHOR_TERM", "term_new_anchor")
	before := readState(t, e)
	renames := e.count("agent", "rename")

	ok, out := e.run("rebind", "--team", id)
	if ok {
		t.Fatalf("锚点 terminal 变化但未确认，应拒绝：\n%s", out)
	}
	has(t, out, "--confirm-anchor term_new_anchor", "给出精确确认参数")
	eq(t, string(readState(t, e)), string(before), "缺确认时状态字节不变")
	eq(t, e.count("agent", "rename"), renames, "缺确认时零 rename")

	e.mustRun("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
	eq(t, e.count("agent", "rename")-renames, 2, "两角色各 rename 一次")
	eq(t, e.str("roles", "owner", "identity", "terminal_id"), "term_new_"+ownerPane, "owner identity 已更新")
	eq(t, e.str("roles", "worker", "identity", "terminal_id"), "term_new_"+workerPane, "worker identity 已更新")
	eq(t, e.str("roles", "owner", "launch_identity", "terminal_id"), origOwnerTerm, "launch_identity 保留历史")
	eq(t, e.str("anchor", "terminal_id"), "term_new_anchor", "锚点 terminal 已更新")
	eq(t, e.str("team_rev"), "2", "提交后 team_rev+1")
	eq(t, e.str("schema_version"), "3", "写出当前 schema")
	eq(t, e.str("rebind_pending"), "<nil>", "计划已清除")
	eq(t, stateAt(e, "rebinds", 0, "result"), "committed", "审计记录 committed")
	eq(t, stateAt(e, "rebinds", 0, "anchor_new_terminal"), "term_new_anchor", "审计记录锚点新值")
	rows := readRows(e.names())
	eq(t, rowKeyFor(rows, ownerPane), ownerName, "owner 名称已改回角色名")
	eq(t, rowKeyFor(rows, workerPane), workerName, "worker 名称已改回角色名")

	// 第二次执行：完整 no-op，不增 rev，不加审计，不写状态。
	settled := readState(t, e)
	out2 := e.mustRun("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
	has(t, out2, "无需重绑", "重复执行为 no-op")
	eq(t, string(readState(t, e)), string(settled), "no-op 不改动状态字节")
	eq(t, e.str("team_rev"), "2", "no-op 不增 rev")

	st := e.mustRun("status", "--team", id)
	has(t, st, "binding=bound", "重绑后两角色均为 bound")
	hasNot(t, st, "candidate", "重绑后不再有候选")
}

func TestRebindRenameModes(t *testing.T) {
	// 任务名为空与为 Bridge 精确自动名都可改回。
	for name, auto := range map[string]bool{"清空": false, "Bridge 自动名": true} {
		t.Run(name, func(t *testing.T) {
			e, id := rebindSetup(t, true, false)
			restart(e, restartOpt{auto: auto})
			e.mustRun("rebind", "--team", id)
			eq(t, e.str("roles", "owner", "identity", "terminal_id"), "term_new_"+e.str("roles", "owner", "pane"), "owner 已绑定")
			has(t, e.mustRun("status", "--team", id), "binding=bound", "绑定有效")
		})
	}
}

func TestRebindRefusalsWriteNothing(t *testing.T) {
	cases := map[string]struct {
		withWorkerSession bool
		restart           restartOpt
		mutate            func(e *tenv, ownerPane, workerPane string)
		extra             []string
		want              string
	}{
		"他人占用的名称": {true, restartOpt{}, func(e *tenv, p, _ string) {
			e.rowDel("@" + p)
			e.rowSet("other-name", p, "term_new_"+p, "codex", "sess-owner")
		}, nil, "当前名称"},
		"目标任务名被其它 pane 占用": {true, restartOpt{}, func(e *tenv, _, _ string) {
			e.rowSet(e.str("roles", "owner", "name"), "w0:p99", "term_x", "claude", "")
		}, nil, "已被其它 pane 占用"},
		"session 不一致": {true, restartOpt{sessions: map[string]string{"owner": "sess-other"}}, func(*tenv, string, string) {}, nil, "session 不一致"},
		"kind 变化": {true, restartOpt{}, func(e *tenv, p, _ string) {
			e.rowDel("@" + p)
			e.rowSet("@"+p, p, "term_new_"+p, "claude", "sess-owner")
		}, nil, "kind 变化"},
		"agent get 失败（角色不可观测）": {true, restartOpt{}, func(e *tenv, p, _ string) {
			e.rowDel("@" + p)
		}, nil, "无法观测"},
		"记录 session 非空时确认值不一致": {true, restartOpt{}, func(*tenv, string, string) {}, []string{"--confirm-session", "owner=sess-wrong"}, "不一致"},
		"确认值与观测不一致":            {false, restartOpt{sessions: map[string]string{"worker": "sess-worker"}}, func(*tenv, string, string) {}, []string{"--confirm-session", "worker=sess-wrong"}, "不一致"},
		"确认参数指向不存在的角色":         {true, restartOpt{}, func(*tenv, string, string) {}, []string{"--confirm-session", "advisor=sess-x"}, "未被使用"},
		"锚点确认值不一致":             {true, restartOpt{}, func(*tenv, string, string) {}, []string{"--confirm-anchor", "term_wrong"}, "不一致"},
		"工作区上下文不一致": {true, restartOpt{}, func(e *tenv, _, _ string) {
			e.stateSet(func(m map[string]any) { m["anchor"].(map[string]any)["workspace_id"] = "w9" })
		}, nil, "workspace/tab/cwd 与编组记录不一致"},
		"部分角色不满足（worker kind 变化，owner 本可重绑）": {true, restartOpt{}, func(e *tenv, _, workerPane string) {
			e.rowDel(e.str("roles", "worker", "name"))
			e.rowDel("@" + workerPane)
			e.rowSet("@"+workerPane, workerPane, "term_new_"+workerPane, "claude", "sess-worker")
		}, nil, "kind 变化"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e, id := rebindSetup(t, c.withWorkerSession, false)
			restart(e, c.restart)
			t.Setenv("FAKE_ANCHOR_TERM", "term_new_anchor")
			c.mutate(e, e.str("roles", "owner", "pane"), e.str("roles", "worker", "pane"))
			before, renames := readState(t, e), e.count("agent", "rename")
			args := append([]string{"rebind", "--team", id}, c.extra...)
			if !containsArg(c.extra, "--confirm-anchor") {
				args = append(args, "--confirm-anchor", "term_new_anchor")
			}
			ok, out := e.run(args...)
			if ok {
				t.Fatalf("应拒绝：\n%s", out)
			}
			has(t, out, c.want, "拒绝原因")
			eq(t, string(readState(t, e)), string(before), "拒绝时状态字节不变")
			eq(t, e.count("agent", "rename"), renames, "拒绝时零 rename")
		})
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestRebindRequiresSessionConfirmation(t *testing.T) {
	e, id := rebindSetup(t, false, false) // worker 记录 session 为空
	restart(e, restartOpt{sessions: map[string]string{"worker": "sess-worker"}})
	t.Setenv("FAKE_ANCHOR_TERM", "term_new_anchor")
	before, renames := readState(t, e), e.count("agent", "rename")

	ok, out := e.run("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
	if ok {
		t.Fatalf("记录 session 为空，缺确认应拒绝：\n%s", out)
	}
	has(t, out, "--confirm-session worker=sess-worker", "输出精确确认参数")
	eq(t, string(readState(t, e)), string(before), "零状态写入")
	eq(t, e.count("agent", "rename"), renames, "零 rename")

	e.mustRun("rebind", "--team", id, "--confirm-anchor", "term_new_anchor", "--confirm-session", "worker=sess-worker")
	eq(t, e.str("roles", "worker", "identity", "session"), "sess-worker", "确认后 session 写入 identity")
	eq(t, e.str("roles", "worker", "launch_identity", "session"), "", "launch_identity 历史不变")
}

func TestRebindAnchorWithIdentity(t *testing.T) {
	e, id := rebindSetup(t, true, true)
	restart(e, restartOpt{})
	e.paneRowSet("w0:p1", "term_new_anchor", "shell", "sess-anchor")
	ok, out := e.run("rebind", "--team", id)
	if ok {
		t.Fatalf("锚点有 identity 且 session 为空时应要求确认：\n%s", out)
	}
	has(t, out, "--confirm-session anchor=sess-anchor", "锚点 session 确认参数")
	e.mustRun("rebind", "--team", id, "--confirm-session", "anchor=sess-anchor")
	eq(t, e.str("anchor", "identity", "terminal_id"), "term_new_anchor", "锚点 identity 已更新")
	eq(t, e.str("anchor", "identity", "session"), "sess-anchor", "锚点 session 已确认")
}

func TestRebindMixedTerminalChanges(t *testing.T) {
	e, id := rebindSetup(t, true, false)
	restart(e, restartOpt{skip: map[string]bool{"worker": true}})
	ownerRenames := e.count("agent", "rename")
	e.mustRun("rebind", "--team", id)
	eq(t, e.count("agent", "rename")-ownerRenames, 1, "只有 owner 需要改名")
	eq(t, e.str("roles", "worker", "identity", "terminal_id"), "term_"+e.str("roles", "worker", "pane"), "未变化的 worker 保持原 identity")
	eq(t, e.str("team_rev"), "2", "混合变化仍提交一次")
}

func TestRebindPreflightWritesNothing(t *testing.T) {
	e, id := rebindSetup(t, true, false)
	restart(e, restartOpt{})
	t.Setenv("FAKE_ANCHOR_TERM", "term_new_anchor")
	before, renames := readState(t, e), e.count("agent", "rename")
	out := e.mustRun("rebind", "--team", id, "--preflight", "--confirm-anchor", "term_new_anchor")
	has(t, out, "预检通过", "只读预检通过")
	eq(t, string(readState(t, e)), string(before), "预检不写状态")
	eq(t, e.count("agent", "rename"), renames, "预检不 rename")
}

func TestRebindRefusesUnstableStates(t *testing.T) {
	cases := map[string]func(e *tenv){
		"phase 非 initialized": func(e *tenv) { e.stateSet(func(m map[string]any) { m["phase"] = "launched" }) },
		"已 stale": func(e *tenv) {
			e.stateSet(func(m map[string]any) { dig(m, "roles", "owner").(map[string]any)["stale"] = true })
		},
		"存在 pending": func(e *tenv) {
			e.stateSet(func(m map[string]any) {
				m["pending"] = map[string]any{"op_id": "1", "role": "owner", "step": "prompt", "delivery": "UNKNOWN"}
			})
		},
		"角色未初始化": func(e *tenv) {
			e.stateSet(func(m map[string]any) { dig(m, "roles", "worker").(map[string]any)["inited"] = false })
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e, id := rebindSetup(t, true, false)
			restart(e, restartOpt{})
			t.Setenv("FAKE_ANCHOR_TERM", "term_new_anchor")
			mutate(e)
			before, renames := readState(t, e), e.count("agent", "rename")
			ok, _ := e.run("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
			if ok {
				t.Fatal("不稳定状态应拒绝")
			}
			eq(t, string(readState(t, e)), string(before), "拒绝时状态字节不变")
			eq(t, e.count("agent", "rename"), renames, "拒绝时零 rename")
		})
	}
}

func TestRebindPartialFailuresKeepPendingAndNeverBlindRollback(t *testing.T) {
	// 两角色均需改名（Bridge 自动名），锚点已确认；每个子用例在失败点之后用无注入的命令对账。
	prepare := func(t *testing.T) (*tenv, string, string) {
		e, id := rebindSetup(t, true, false)
		origOwnerTerm := e.str("roles", "owner", "identity", "terminal_id")
		restart(e, restartOpt{auto: true})
		t.Setenv("FAKE_ANCHOR_TERM", "term_new_anchor")
		return e, id, origOwnerTerm
	}
	t.Run("第二个 rename 失败：pending 保留、非零、不回滚，重跑只补剩余步骤", func(t *testing.T) {
		e, id, origOwnerTerm := prepare(t)
		base := e.count("agent", "rename")
		t.Setenv("NAKAMA_FAKE_RENAME_FAIL_AT", strconv.Itoa(base+2)) // owner 为 base+1，worker 为 base+2
		ok, out := e.run("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
		if ok {
			t.Fatalf("第二步失败应非零：\n%s", out)
		}
		eq(t, stateAt(e, "rebind_pending", "steps", 0, "result"), "done", "第一步已复核")
		eq(t, stateAt(e, "rebind_pending", "steps", 1, "result"), "started", "第二步保留 started")
		eq(t, e.str("roles", "owner", "identity", "terminal_id"), origOwnerTerm, "未提交前 identity 仍为记录值")
		t.Setenv("NAKAMA_FAKE_RENAME_FAIL_AT", "")
		e.mustRun("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
		eq(t, e.count("agent", "rename")-base, 3, "owner 一次 + worker 失败一次 + 重跑一次")
		eq(t, e.str("team_rev"), "2", "最终提交一次")
	})
	t.Run("rename 已生效但返回错误：重跑时按观测判定已完成，不重发", func(t *testing.T) {
		e, id, _ := prepare(t)
		base := e.count("agent", "rename")
		t.Setenv("NAKAMA_FAKE_RENAME_APPLY_ERR_AT", strconv.Itoa(base+1))
		ok, out := e.run("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
		if ok {
			t.Fatalf("返回错误应非零：\n%s", out)
		}
		eq(t, stateAt(e, "rebind_pending", "steps", 0, "result"), "started", "未确认的 rename 保留 started")
		t.Setenv("NAKAMA_FAKE_RENAME_APPLY_ERR_AT", "")
		e.mustRun("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
		eq(t, e.count("agent", "rename")-base, 2, "owner 生效一次不重发 + worker 一次")
	})
	t.Run("中断信号：在 rename 前停止，计划保留，重跑完成", func(t *testing.T) {
		e, id, _ := prepare(t)
		hook := func(a *app) {
			a.hookBeforeRename = func(role string) {
				if role == "owner" {
					a.stop.Store(true)
				}
			}
		}
		err, _ := e.rebindInProc(hook, "--team", id, "--confirm-anchor", "term_new_anchor")
		if err == nil {
			t.Fatal("信号后应非零")
		}
		eq(t, stateAt(e, "rebind_pending", "steps", 0, "result"), "", "信号时 owner 未开始")
		e.mustRun("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
		eq(t, e.str("team_rev"), "2", "重跑完成提交")
	})
	t.Run("预检后到提交前身份漂移：不提交，discard 后按当前观测重新绑定", func(t *testing.T) {
		e, id, origOwnerTerm := prepare(t)
		ownerName, ownerPane := e.str("roles", "owner", "name"), e.str("roles", "owner", "pane")
		hook := func(a *app) {
			a.hookBeforeCommit = func() { e.rowSet(ownerName, ownerPane, "term_drift", "codex", "sess-owner") }
		}
		err, _ := e.rebindInProc(hook, "--team", id, "--confirm-anchor", "term_new_anchor")
		if err == nil {
			t.Fatal("提交前漂移应拒绝提交")
		}
		eq(t, e.str("roles", "owner", "identity", "terminal_id"), origOwnerTerm, "漂移时 identity 不变")
		e.mustRun("rebind", "--team", id, "--discard-pending")
		eq(t, e.str("rebind_pending"), "<nil>", "计划已放弃")
		e.mustRun("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
		eq(t, e.str("roles", "owner", "identity", "terminal_id"), "term_drift", "按当前观测重新绑定")
	})
	t.Run("Bridge 在途自动命名：rename 后复核失败，重跑时允许再次改回", func(t *testing.T) {
		e, id, _ := prepare(t)
		workerName, workerPane := e.str("roles", "worker", "name"), e.str("roles", "worker", "pane")
		hook := func(a *app) {
			a.hookAfterRename = func(role string) {
				if role == "worker" {
					// 模拟 Bridge 在改名之后又自动命名：先删除该 pane 的既有行，避免同 pane 出现两行。
					e.rowDel(workerName)
					e.rowSet(bridgeAutoName(e.str("cwd"), "term_new_"+workerPane), workerPane, "term_new_"+workerPane, "pi", "sess-worker")
				}
			}
		}
		err, _ := e.rebindInProc(hook, "--team", id, "--confirm-anchor", "term_new_anchor")
		if err == nil {
			t.Fatal("Bridge 竞争后复核应失败")
		}
		eq(t, stateAt(e, "rebind_pending", "steps", 1, "result"), "started", "worker 保留 started")
		e.mustRun("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
		eq(t, e.str("team_rev"), "2", "重跑完成提交")
		has(t, e.mustRun("status", "--team", id), "binding=bound", "最终绑定有效")
	})
	t.Run("保存失败：非零且不 rename", func(t *testing.T) {
		e, id, _ := prepare(t)
		teams := filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "teams")
		renames := e.count("agent", "rename")
		hook := func(a *app) {
			a.hookBeforeRename = func(role string) {
				// 把 teams 目录换成普通文件：后续保存的目录创建失败。
				os.Rename(teams, teams+".bak")
				os.WriteFile(teams, []byte("x"), 0o600)
			}
		}
		err, _ := e.rebindInProc(hook, "--team", id, "--confirm-anchor", "term_new_anchor")
		os.Remove(teams)
		os.Rename(teams+".bak", teams)
		if err == nil {
			t.Fatal("保存失败应非零")
		}
		eq(t, e.count("agent", "rename"), renames, "保存失败时未 rename")
	})
}

func TestRebindDiscardWithoutPlanRefused(t *testing.T) {
	e, id := rebindSetup(t, true, false)
	restart(e, restartOpt{})
	t.Setenv("FAKE_ANCHOR_TERM", "term_new_anchor")
	e.mustFail("rebind", "--team", id, "--discard-pending")
	e.mustRun("rebind", "--team", id, "--confirm-anchor", "term_new_anchor")
	eq(t, stateAt(e, "rebinds", 0, "result"), "committed", "已提交的记录仍保留")
}

func TestRebindCLIUsage(t *testing.T) {
	e := newEnv(t)
	for _, args := range [][]string{
		{"rebind"},
		{"rebind", "--team", "x", "--preflight", "--discard-pending"},
		{"rebind", "--team", "x", "--confirm-session", "bogus=1"},
		{"rebind", "--team", "x", "--confirm-session", "owner"},
		{"rebind", "--team", "x", "--confirm-session", "owner=1", "--confirm-session", "owner=2"},
	} {
		if ok, _ := e.run(args...); ok {
			t.Fatalf("%v 应非零", args)
		}
	}
	// 不存在的编组：非零且不创建任何状态文件。
	if ok, _ := e.run("rebind", "--team", "task_missing"); ok {
		t.Fatal("不存在的编组应非零")
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "teams", "task_missing.json")); err == nil {
		t.Fatal("不应创建状态文件")
	}
}

func TestStatusAndInitHintsAreReadOnly(t *testing.T) {
	e, id := rebindSetup(t, true, false)
	restart(e, restartOpt{})
	t.Setenv("FAKE_ANCHOR_TERM", "term_new_anchor")
	before := readState(t, e)
	st := e.mustRun("status", "--team", id)
	has(t, st, "binding=candidate", "status 标注候选")
	has(t, st, "rebind --team", "status 提示可能需要 rebind")
	eq(t, string(readState(t, e)), string(before), "status 不写状态")

	ok, out := e.run("init", "--team", id)
	if ok {
		t.Fatal("候选状态下 init 应拒绝")
	}
	has(t, out, "rebind", "init 提示显式 rebind")
	eq(t, string(readState(t, e)), string(before), "init 拒绝不写状态（不标 stale）")
}

func TestRoleCandidateHintWithoutAnchorChange(t *testing.T) {
	e, id := rebindSetup(t, true, false)
	restart(e, restartOpt{}) // 锚点 terminal 不变，只有角色变化
	before := readState(t, e)
	ok, out := e.run("init", "--team", id)
	if ok {
		t.Fatal("角色候选下 init 应拒绝")
	}
	has(t, out, "可能需要显式 rebind", "角色候选提示")
	hasNot(t, out, "stale", "候选不标 stale")
	eq(t, string(readState(t, e)), string(before), "init 拒绝不写状态")
}

func TestStateSchemaCompatibility(t *testing.T) {
	t.Run("写出当前 schema", func(t *testing.T) {
		e, _ := rebindSetup(t, true, false)
		eq(t, e.str("schema_version"), "3", "新写出的状态为当前 schema")
	})
	t.Run("schema 1 无 rebind 字段仍可读取，保存后升级", func(t *testing.T) {
		e, id := rebindSetup(t, true, false)
		e.stateSet(func(m map[string]any) { m["schema_version"] = 1; delete(m, "rebinds"); delete(m, "task_dir") })
		e.mustRun("status", "--team", id)
		e.mustRun("init", "--team", id)
		eq(t, e.str("schema_version"), "3", "保存后升级")
	})
	t.Run("schema 1 携带 rebind 字段视为损坏", func(t *testing.T) {
		e, id := rebindSetup(t, true, false)
		e.stateSet(func(m map[string]any) { m["schema_version"] = 1; m["rebinds"] = []any{} })
		has(t, e.mustFail("status", "--team", id), "校验失败", "拒绝损坏状态")
	})
	t.Run("未知 schema 拒绝", func(t *testing.T) {
		e, id := rebindSetup(t, true, false)
		e.stateSet(func(m map[string]any) { m["schema_version"] = 4 })
		has(t, e.mustFail("status", "--team", id), "校验失败", "拒绝未知 schema")
	})
}
