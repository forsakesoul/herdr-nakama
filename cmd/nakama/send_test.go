package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sendSetup：up + init 得到已初始化的组，并准备一份正文草稿。withAdvisor 时带一个 cc Advisor。
func sendSetup(t *testing.T, withAdvisor bool) (e *tenv, id, body string) {
	t.Helper()
	e = newEnv(t)
	e.config("normal")
	if withAdvisor {
		e.mustRun("up", "--advisor", "cc")
	} else {
		e.mustRun("up")
	}
	id = e.teamID()
	e.mustRun("init", "--team", id)
	body = filepath.Join(e.root, "draft.md")
	os.WriteFile(body, []byte("正文第一行\n正文第二行\n"), 0o644)
	return e, id, body
}

func TestSendDeliversWritesFileAndLedger(t *testing.T) {
	e, id, body := sendSetup(t, false)
	owner, worker := id+"_design", id+"_execute"
	sub := filepath.Join(realPath(e.proj), ".nakama", id, "op_x")

	out := e.mustRun("send", "--team", id, "--from", "O1", "--task", "op_x", "--to", "w1", "--title", "派单：路径修复", "--file", body, "--reply", "async", "--set", "handoff=op_x_h1")
	has(t, out, "req=op_x/O1-000 delivery=SENT to=W1 file="+filepath.Join(sub, "O1-000.md"), "输出请求号与投递结果")
	p := e.lastPrompt(worker)
	has(t, p, "[O1-000｜派单：路径修复]", "消息头")
	has(t, p, "task=op_x req=op_x/O1-000 from=O1 to=W1 reply=async reply_to=O1 handoff=op_x_h1", "关联字段")
	has(t, p, "file="+filepath.Join(sub, "O1-000.md"), "只带文件路径")
	hasNot(t, p, "正文第一行", "正文不内联")
	b, _ := os.ReadFile(filepath.Join(sub, "O1-000.md"))
	has(t, string(b), "[O1-000｜派单：路径修复]\ntask=op_x", "文件以消息头开头")
	has(t, string(b), "\n\n正文第一行\n正文第二行\n", "文件含完整正文")

	st := e.mustRun("status", "--team", id)
	has(t, st, "task op_x: messages=1 open=1", "status 汇总")
	has(t, st, "WAITING  op_x/O1-000 O1→W1 派单：路径修复", "未答复请求")

	e.mustRun("send", "--team", id, "--from", "W1", "--task", "op_x", "--to", owner, "--title", "完成：路径修复", "--file", body, "--status", "DONE", "--responds-to", "op_x/O1-000")
	has(t, e.lastPrompt(owner), "req=op_x/W1-000 from=W1 to=O1 status=DONE responds_to=op_x/O1-000", "回报带状态与关联")
	has(t, e.mustRun("status", "--team", id), "task op_x: messages=2 open=0", "答复后结清")

	has(t, e.mustRun("send", "--team", id, "--from", "O1", "--task", "op_x", "--to", "W1", "--title", "修复：边界", "--file", body), "req=op_x/O1-001", "序号按本实例已有文件递增")
}

func TestSendBusyRecipientKeepsMessageForResend(t *testing.T) {
	e, id, body := sendSetup(t, false)
	worker := id + "_execute"
	before := len(e.prompts(worker))

	t.Setenv("FAKE_WORKING_SUFFIX", "_execute")
	out := e.mustFail("send", "--team", id, "--from", "O1", "--task", "op_x", "--to", "W1", "--title", "派单：x", "--file", body, "--reply", "async")
	has(t, out, "req=op_x/O1-000 delivery=NOT_SENT", "忙碌时不投递")
	has(t, out, "--resend op_x/O1-000", "提示补投")
	eq(t, len(e.prompts(worker)), before, "未向忙碌的收件人发送输入")
	eq(t, e.count("agent", "wait"), 1, "只有界等待一次")
	has(t, e.mustRun("status", "--team", id), "NOT_SENT op_x/O1-000 O1→W1 派单：x", "status 列出未送达")

	t.Setenv("FAKE_WORKING_SUFFIX", "")
	has(t, e.mustRun("send", "--team", id, "--from", "O1", "--resend", "op_x/O1-000"), "req=op_x/O1-000 delivery=SENT", "补投沿用原请求号")
	has(t, e.lastPrompt(worker), "reply=async reply_to=O1", "补投保留关联字段")
	ents, _ := os.ReadDir(filepath.Join(realPath(e.proj), ".nakama", id, "op_x"))
	eq(t, len(ents), 2, "只有一个消息文件和台账")
	hasNot(t, e.mustRun("status", "--team", id), "NOT_SENT", "补投后不再列为未送达")
}

func TestSendRelayNoDeliverAndSync(t *testing.T) {
	e, id, body := sendSetup(t, true)
	owner, advisor := id+"_design", id+"_advisor"

	e.mustRun("send", "--team", id, "--from", "W1", "--task", "op_x", "--to", "A1", "--title", "完成：x", "--file", body, "--status", "DONE", "--set", "relay_to=O1")
	has(t, e.lastPrompt(advisor), "relay_to=O1", "代收标记")
	e.mustRun("send", "--team", id, "--from", "A1", "--resend", "op_x/W1-000", "--to", "O1")
	p := e.lastPrompt(owner)
	has(t, p, "req=op_x/W1-000 from=W1 to=O1 status=DONE", "转交沿用原请求号与发件人")
	has(t, p, "relayed_by=A1", "转交者")

	before := len(e.prompts(owner))
	has(t, e.mustRun("send", "--team", id, "--from", "A1", "--task", "op_x", "--to", "O1", "--title", "答复：身份", "--file", body, "--status", "DONE", "--no-deliver"), "req=op_x/A1-000 delivery=SYNC", "同步答复只落盘")
	eq(t, len(e.prompts(owner)), before, "--no-deliver 不投递")

	e.mustRun("send", "--team", id, "--from", "O1", "--task", "op_x", "--to", "A1", "--title", "确认：身份", "--file", body, "--reply", "sync")
	argv := e.argvs()[len(e.argvs())-2] // 末尾是随后的 agent read
	eq(t, strings.Join(argv[len(argv)-3:], " "), "--wait --timeout 120000", "同步请求等待答复")
	eq(t, e.argvs()[len(e.argvs())-1][1], "read", "同步请求输出对方回复")

	t.Setenv("FAKE_PROMPT_ERROR", "timeout")
	out := e.mustFail("send", "--team", id, "--from", "O1", "--task", "op_x", "--to", "A1", "--title", "确认：状态", "--file", body, "--reply", "sync")
	has(t, out, "delivery=SENT", "超时仍是已投递")
	has(t, out, "READ_PENDING", "提示补读而非重发")
	t.Setenv("FAKE_PROMPT_ERROR", "server_unavailable")
	has(t, e.mustFail("send", "--team", id, "--from", "O1", "--task", "op_x", "--to", "A1", "--title", "确认：y", "--file", body), "delivery=UNKNOWN", "结果不明不记为未发送")
}

func TestSendGuards(t *testing.T) {
	e, id, body := sendSetup(t, false)
	base := []string{"send", "--team", id, "--task", "op_x", "--title", "派单：x", "--file", body}
	with := func(extra ...string) []string { return append(append([]string{}, base...), extra...) }
	prompts := e.count("agent", "prompt")

	has(t, e.mustFail(with("--to", "W1")...), "无法确定发件人", "拿不到 pane 时必须 --from")
	has(t, e.mustFail(with("--from", "O1", "--to", "O1")...), "不能发给自己", "不发给自己")
	has(t, e.mustFail(with("--from", "O1", "--to", "A1")...), "没有在册成员 A1", "未知收件人")
	has(t, e.mustFail(with("--from", "O1", "--to", "W1", "--set", "status=DONE")...), "--set 须为", "专用字段不经 --set")
	has(t, e.mustFail(with("--from", "O1", "--to", "W1", "--status", "OK")...), "--status 只能是", "状态取值受限")
	has(t, e.mustFail("send", "--team", id, "--from", "O1", "--task", "../x", "--to", "W1", "--title", "t", "--file", body), "task_id 只能用", "task_id 不能逃出任务目录")
	has(t, e.mustFail("send", "--team", id, "--from", "O1", "--task", "op_x", "--to", "W1", "--title", "t", "--file", filepath.Join(e.root, "none.md")), "正文文件不是普通可读文件", "正文须存在")
	has(t, e.mustFail("send", "--team", id, "--from", "O1", "--resend", "op_x/O1-009"), "任务目录不存在", "未知任务不重投")
	eq(t, e.count("agent", "prompt"), prompts, "以上拒绝均未投递")
	if _, err := os.Stat(filepath.Join(realPath(e.proj), ".nakama", id, "op_x")); err == nil {
		t.Fatal("参数校验失败不应创建任务目录")
	}

	// 发件人取自当前 pane；与 --from 不一致即拒绝。
	t.Setenv("HERDR_PANE_ID", e.str("roles", "W1", "pane"))
	has(t, e.mustFail(with("--from", "O1", "--to", "W1")...), "与当前 pane 的成员 W1 不一致", "不能冒用其他成员")
	has(t, e.mustRun(with("--to", "O1")...), "req=op_x/W1-000 delivery=SENT to=O1", "按 pane 识别发件人")
	t.Setenv("HERDR_PANE_ID", "")

	// 收件人身份漂移：不投递，消息保留。
	e.rowSet(id+"_execute", e.str("roles", "W1", "pane"), "term_other", "pi", "")
	out := e.mustFail(with("--from", "O1", "--to", "W1")...)
	has(t, out, "delivery=NOT_SENT", "身份漂移不投递")
	has(t, out, "实例身份漂移", "给出原因")
}

func TestProtocolIsScopedByRole(t *testing.T) {
	e, id, _ := sendSetup(t, true)
	owner, worker, advisor := e.lastPrompt(id+"_design"), e.lastPrompt(id+"_execute"), e.lastPrompt(id+"_advisor")
	for name, p := range map[string]string{"owner": owner, "worker": worker, "advisor": advisor} {
		hasNot(t, p, "<!-- role:", name+" 不带角色标记")
		hasNot(t, p, "<!-- /role -->", name+" 不带角色标记")
		has(t, p, "### 通信", name+" 收到通用小节")
		has(t, p, " send --team "+id+" --task <task_id>", name+" 收到发送命令")
		has(t, p, " status --team "+id, name+" 收到状态命令")
	}
	has(t, owner, "### 执行巡检（Owner）", "owner 小节")
	hasNot(t, worker, "### 执行巡检（Owner）", "worker 不收 owner 小节")
	has(t, worker, "### Worker 投递", "worker 小节")
	has(t, worker, "### reply 状态机", "worker 也发起请求")
	hasNot(t, advisor, "### Worker 投递", "advisor 不收 worker 小节")
	hasNot(t, advisor, "### reply 状态机", "advisor 不发起请求")
	has(t, advisor, "### 独立 Advisor 代收", "advisor 小节")
	hasNot(t, owner, "### 独立 Advisor 代收", "owner 不收 advisor 小节")
	if len(worker) >= len(owner) || len(advisor) >= len(worker) {
		t.Fatalf("按角色裁剪后应 owner > worker > advisor：%d %d %d", len(owner), len(worker), len(advisor))
	}
}

func TestDownArchivesAndFreesAnchor(t *testing.T) {
	e, id, body := sendSetup(t, false)
	archived := filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "archive", id+".json")
	taskDir := filepath.Join(realPath(e.proj), ".nakama", id)
	writes := e.count("agent", "prompt") + e.count("agent", "rename")
	workerPane := e.str("roles", "W1", "pane")

	has(t, e.mustFail("down"), "down 必须指定 --team", "不按 pane 猜测")
	has(t, e.mustFail("down", "--team", id), "需要 --yes", "非交互须显式确认")
	out := e.mustRun("down", "--team", id, "--yes")
	has(t, out, "已解散", "解散")
	has(t, out, "的 pane "+workerPane+" 未关闭", "提示成员 pane 保留")
	if _, err := os.Stat(archived); err != nil {
		t.Fatalf("状态应归档：%v", err)
	}
	if _, err := os.Stat(taskDir); err == nil {
		t.Fatal("空任务目录应随解散删除")
	}
	eq(t, e.count("agent", "prompt")+e.count("agent", "rename"), writes, "解散不向成员发送输入、不改名")
	has(t, e.mustRun("status"), "暂无编组", "status 不再列出")
	has(t, e.mustFail("send", "--team", id, "--from", "O1", "--task", "op_x", "--to", "W1", "--title", "t", "--file", body), "编组不存在", "解散后不能再投递")

	// 锚点释放：同一 pane 可以重新建组；有消息的任务目录保留。
	e.mustRun("up")
	id2 := e.teamID()
	if id2 == id {
		t.Fatal("应是新编组")
	}
	e.mustRun("init", "--team", id2)
	e.mustRun("send", "--team", id2, "--from", "O1", "--task", "op_y", "--to", "W1", "--title", "派单：y", "--file", body)
	has(t, e.mustRun("down", "--team", id2, "--yes"), "任务目录保留："+filepath.Join(realPath(e.proj), ".nakama", id2), "有内容的任务目录不删")
}

func TestDownArchivesUnparseableState(t *testing.T) {
	e, id, _ := sendSetup(t, false)
	os.WriteFile(e.stateFile(), []byte(`{"team_id":"`+id+`","anchor":{"pane_id":"w0:p1"}}`), 0o600)
	has(t, e.mustFail("status", "--team", id), "状态文件校验失败", "损坏的状态")
	has(t, e.mustRun("down", "--team", id, "--yes"), "按原样归档", "损坏的状态也能解散")
	e.mustRun("up")
}

func TestMenuDownAndRebind(t *testing.T) {
	e, id := rebindSetup(t, true, false)
	e.anchorEnv(t)
	withTTY(t, true)

	_, out := e.pick("\n")
	hasNot(t, out, "重绑", "绑定有效时菜单不显示重绑")
	has(t, out, "d  解散编组", "菜单显示解散")

	restart(e, restartOpt{})
	t.Setenv("FAKE_ANCHOR_TERM", "term_new_anchor")
	ok, out := e.pick("b\ny\n\n")
	if !ok {
		t.Fatalf("菜单重绑应成功：\n%s", out)
	}
	has(t, out, "锚点 terminal 已变化", "显示需要确认的观测值")
	has(t, out, "重绑完成", "提交")
	eq(t, e.str("anchor", "terminal_id"), "term_new_anchor", "锚点 terminal 已更新")
	eq(t, e.str("roles", "O1", "identity", "terminal_id"), "term_new_"+e.str("roles", "O1", "pane"), "成员 terminal 已更新")

	ok, out = e.pick("d\nn\n")
	has(t, out, "已取消", "解散需确认")
	eq(t, e.teamID(), id, "取消后编组仍在")
	if ok, out = e.pick("d\ny\n\n"); !ok {
		t.Fatalf("菜单解散应成功：\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "archive", id+".json")); err != nil {
		t.Fatalf("状态应归档：%v", err)
	}
}

func TestAutoInitChainsAfterUpAndAdd(t *testing.T) {
	e := newEnv(t)
	e.rawConfig(`{ "default_profile": { "owner": "codex/gpt-6.1-sol:high", "worker": "codex/gpt-6.1-luna:medium", "advisor": "none", "auto_init": true }, "projects": {} }`)
	has(t, e.mustRun("up"), "READY", "建组后接着初始化")
	id := e.teamID()
	eq(t, e.str("phase"), "initialized", "已初始化")
	e.mustRun("add", "--team", id, "--engine", "codex/gpt-6.1-sol:high")
	eq(t, e.str("phase"), "initialized", "加人后接着初始化")
	eq(t, e.str("roles", "A1", "inited"), "true", "新成员已初始化")
	has(t, e.lastPrompt(id+"_design"), "[TEAM_UPDATE] team_rev=2", "其余成员收到更新")
}
