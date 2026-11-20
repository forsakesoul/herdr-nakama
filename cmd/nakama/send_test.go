package main

import (
	"fmt"
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
	has(t, e.lastPrompt(advisor), "relay_to=O1", "转交标记")
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
	has(t, e.mustFail(with("--from", "W1", "--to", "O1", "--status", "DONE", "--reply", "async")...), "不能同用", "回报不能同时要求答复")
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
	has(t, advisor, "## 审查输出", "advisor 小节")
	hasNot(t, owner, "## 审查输出", "owner 不收 advisor 小节")
	hasNot(t, worker, "## 审查输出", "worker 不收 advisor 小节")
	hasNot(t, advisor, "### 独立 Advisor 代收", "代收小节已删除")
	// 上下文策略：Worker 专属整理句不进 Advisor；Owner/Advisor 自行压缩句不进 Worker。
	hasNot(t, advisor, "到处理点或下一单元可能吃掉余量时", "advisor 不收 CONTEXT_READY 整理句")
	hasNot(t, worker, "仅当本任务已有用户明确的自动压缩授权", "worker 不收 Owner/Advisor 自行压缩句")
	has(t, advisor, "仅当本任务已有用户明确的自动压缩授权", "advisor 收自行压缩句")
	// h5：快照定义三角色可见；调度（S_A/S_AB）只给 Owner，写保护与预热只读例外给 Worker，快照审查来源给 Advisor。
	for name, p := range map[string]string{"owner": owner, "worker": worker, "advisor": advisor} {
		has(t, p, "审查快照（所有角色共用定义）", name+" 收快照定义")
	}
	has(t, owner, "S_AB", "owner 收并行与修复调度")
	hasNot(t, worker, "S_AB", "worker 不收 Owner 调度")
	hasNot(t, advisor, "S_AB", "advisor 不收 Owner 调度")
	has(t, worker, "在审快照的改动路径禁止写", "worker 收写保护")
	has(t, worker, "只读预热", "worker 收只读预热例外")
	has(t, owner, "只读预热请求", "owner 收预热派发")
	has(t, advisor, "不可变快照", "advisor 收快照审查来源")
	// h6：预热回报不要求 handoff、快照仅按交接包要求生成；全局通过点要求全部累计差异。
	for name, p := range map[string]string{"owner": owner, "worker": worker} {
		has(t, p, "只读预热的回报按原 req 回 DONE/BLOCKED", name+" 收预热回报规则")
	}
	hasNot(t, advisor, "只读预热的回报按原 req 回 DONE/BLOCKED", "advisor 不收 Worker 回报条")
	has(t, worker, "若 Owner 在交接包要求阶段或最终快照审查", "worker 快照仅按交接要求生成")
	has(t, owner, "仅当快照截至该点的全部累计差异都已审通过时才推进全局点", "owner 全局通过点条件")
	// 规则重读失败停点在恢复信息（所有角色可见），不只在 Owner/Worker 的职责保全里。
	for name, p := range map[string]string{"owner": owner, "worker": worker, "advisor": advisor} {
		has(t, p, "规则重读失败则停在只读补证", name+" 收规则重读失败停点")
	}
	has(t, worker, "到处理点或下一单元可能吃掉余量时", "worker 收整理句")
	for name, p := range map[string]string{"owner": owner, "worker": worker, "advisor": advisor} {
		has(t, p, "一次整理只压缩一次", name+" 收共享整理句")
		has(t, p, "准备点=min(15 万 token, 有效窗口 50%)", name+" 收阈值句")
	}
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

func TestSendBoundaries(t *testing.T) {
	e, id, body := sendSetup(t, true)
	owner, worker := id+"_design", id+"_execute"
	send := func(args ...string) []string { return append([]string{"send", "--team", id}, args...) }

	// RESULT_REPLAY：只落盘的同步答复之后可以沿用原请求号补投。
	e.mustRun(send("--from", "W1", "--task", "op_x", "--to", "O1", "--title", "答复：身份", "--file", body, "--status", "DONE", "--no-deliver")...)
	has(t, e.mustRun(send("--from", "W1", "--resend", "op_x/W1-000")...), "req=op_x/W1-000 delivery=SENT to=O1", "同步答复可补投")
	has(t, e.lastPrompt(owner), "req=op_x/W1-000 from=W1 to=O1 status=DONE", "补投内容不变")

	// 台账里混入坏行：跳过，不影响后续读写。
	sub := filepath.Join(realPath(e.proj), ".nakama", id, "op_x")
	f, _ := os.OpenFile(filepath.Join(sub, ledgerName), os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("not json\n{\"req\":\"\"}\n")
	f.Close()
	has(t, e.mustRun("status", "--team", id), "task op_x: messages=1 open=0", "坏行被忽略")

	// 收件人在投递前被移除：原收件人不再接收，须显式改投。
	t.Setenv("FAKE_WORKING_SUFFIX", "_advisor")
	has(t, e.mustFail(send("--from", "O1", "--task", "op_x", "--to", "A1", "--title", "审查：diff", "--file", body, "--reply", "async")...), "delivery=NOT_SENT", "advisor 忙碌")
	st := e.mustRun("status", "--team", id)
	has(t, st, "NOT_SENT op_x/O1-000 O1→A1", "列为未送达")
	hasNot(t, st, "WAITING", "没送出去的请求不算等待答复")
	t.Setenv("FAKE_WORKING_SUFFIX", "")
	e.mustRun("drop", "--team", id)
	has(t, e.mustFail(send("--from", "O1", "--resend", "op_x/O1-000")...), "没有在册成员 A1", "不向已停用实例补投")
	has(t, e.mustRun(send("--from", "O1", "--resend", "op_x/O1-000", "--to", "W1")...), "req=op_x/O1-000 delivery=SENT to=W1", "改投在册成员")
	hasNot(t, e.lastPrompt(worker), "relayed_by", "作者自己改投不是代收")

	// 新成员初始化前既不能发也收不到。
	e.mustRun("add", "--team", id, "--role", "worker", "--engine", "cc")
	has(t, e.mustFail(send("--from", "W2", "--task", "op_x", "--to", "O1", "--title", "t", "--file", body)...), "发件人 W2 尚未初始化", "未初始化不能发")
	has(t, e.mustFail(send("--from", "O1", "--task", "op_x", "--to", "W2", "--title", "t", "--file", body)...), "W2 未完成初始化", "未初始化收不到")
	eq(t, len(e.prompts(id+"_execute2")), 0, "未初始化成员没有收到输入")

	// 任务目录被换成符号链接：拒绝写入。
	dir := filepath.Join(realPath(e.proj), ".nakama", id)
	os.Rename(dir, dir+".real")
	os.Symlink(dir+".real", dir)
	has(t, e.mustFail(send("--from", "O1", "--task", "op_x", "--to", "W1", "--title", "t", "--file", body)...), "没有可用的任务目录", "不跟随符号链接")
}

func TestStatusViewFollowsLedger(t *testing.T) {
	e, id, body := sendSetup(t, false)
	send := func(from, to, title string, extra ...string) {
		t.Helper()
		e.mustRun(append([]string{"send", "--team", id, "--from", from, "--task", "op", "--to", to, "--title", title, "--file", body}, extra...)...)
	}
	open := func() string {
		out := e.mustRun("status", "--team", id)
		return out[strings.Index(out, "  task op:"):]
	}

	send("O1", "W1", "派单：a", "--reply", "async") // O1-000
	send("W1", "O1", "停点：a", "--status", "CONTEXT_READY", "--responds-to", "op/O1-000")
	has(t, open(), "PAUSED   op/O1-000", "停点 → PAUSED")
	send("O1", "W1", "恢复：a", "--set", "resumes=op/W1-000")
	has(t, open(), "WAITING  op/O1-000", "恢复 → WAITING")
	send("W1", "O1", "阻塞：a", "--status", "BLOCKED", "--responds-to", "op/O1-000")
	has(t, open(), "BLOCKED  op/O1-000", "阻塞 → BLOCKED")

	send("O1", "W1", "派单：a 新版", "--reply", "async", "--set", "replaces=op/O1-000") // O1-002
	out := open()
	hasNot(t, out, "op/O1-000", "被替代的请求不再列出")
	has(t, out, "WAITING  op/O1-002", "新请求等待中")

	send("O1", "W1", "取消：a", "--reply", "sync", "--set", "cancels=op/O1-002") // O1-003
	has(t, open(), "WAITING  op/O1-002", "取消未确认前仍未结")
	send("W1", "O1", "取消确认：a", "--status", "DONE", "--responds-to", "op/O1-003", "--no-deliver")
	has(t, open(), "open=0", "取消确认后两条都结清")

	send("O1", "W1", "派单：b", "--reply", "async") // O1-004
	send("W1", "O1", "完成：b", "--status", "DONE", "--responds-to", "op/O1-004")
	send("W1", "O1", "晚到：b", "--status", "BLOCKED", "--responds-to", "op/O1-004")
	has(t, open(), "open=0", "晚到的阻塞不重开已答复的请求")
}

// 真机上见过 Codex 在一轮进行中被短暂报成空闲：单次空闲观测不足以投递。
func TestSendRequiresStableIdle(t *testing.T) {
	e, id, body := sendSetup(t, false)
	worker := id + "_execute"
	before := len(e.prompts(worker))
	t.Setenv("NAKAMA_FAKE_STATUS_SEQ", "idle,working")
	out := e.mustFail("send", "--team", id, "--from", "O1", "--task", "op_x", "--to", "W1", "--title", "派单：x", "--file", body)
	has(t, out, "delivery=NOT_SENT", "空闲一闪而过不投递")
	eq(t, len(e.prompts(worker)), before, "未发送输入")
	eq(t, e.count("agent", "wait"), 1, "转入有界等待")
}

func TestDownReportsArchiveFailure(t *testing.T) {
	e, id, _ := sendSetup(t, false)
	// archive 位置被普通文件占住：建目录失败。
	os.WriteFile(filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "archive"), nil, 0o600)
	has(t, e.mustFail("down", "--team", id, "--yes"), "归档失败", "不得报告成功")
	eq(t, e.teamID(), id, "编组仍在")
}

func TestSendVerifiesSenderAndRoster(t *testing.T) {
	e, id, body := sendSetup(t, false)
	args := []string{"send", "--team", id, "--task", "op", "--to", "W1", "--title", "t", "--file", body}
	n := e.count("agent", "prompt")

	t.Setenv("HERDR_PANE_ID", "w0:p99") // 有 pane 但不是成员：--from 不能借名
	has(t, e.mustFail(append(args, "--from", "O1")...), "不是编组", "拒绝")
	t.Setenv("HERDR_PANE_ID", "")

	// 发件人的 pane 换了实例。
	e.rowSet(id+"_design", e.str("roles", "O1", "pane"), "term_other", "codex", "")
	has(t, e.mustFail(append(args, "--from", "O1")...), "实例身份漂移", "发件人身份漂移时拒绝")
	eq(t, e.count("agent", "prompt"), n, "均未投递")
	if _, err := os.Stat(filepath.Join(realPath(e.proj), ".nakama", id, "op")); err == nil {
		t.Fatal("未通过发件人核验不应写入任务目录")
	}
}

func TestUndeliveredControlMessagesDoNotSettle(t *testing.T) {
	e, id, body := sendSetup(t, false)
	send := func(ok bool, from, to, title string, extra ...string) {
		t.Helper()
		a := append([]string{"send", "--team", id, "--from", from, "--task", "op", "--to", to, "--title", title, "--file", body}, extra...)
		if ok {
			e.mustRun(a...)
		} else {
			e.mustFail(a...)
		}
	}
	view := func() string {
		out := e.mustRun("status", "--team", id)
		return out[strings.Index(out, "  task op:"):]
	}
	send(true, "O1", "W1", "派单：a", "--reply", "async")
	send(true, "W1", "O1", "停点：a", "--status", "CONTEXT_READY", "--responds-to", "op/O1-000")

	t.Setenv("FAKE_WORKING_SUFFIX", "_execute")
	send(false, "O1", "W1", "恢复：a", "--set", "resumes=op/W1-000")
	has(t, view(), "PAUSED   op/O1-000", "未送达的恢复不生效")
	send(false, "O1", "W1", "新版：a", "--reply", "async", "--set", "replaces=op/O1-000")
	has(t, view(), "PAUSED   op/O1-000", "未送达的替代不结清原请求")
	t.Setenv("FAKE_WORKING_SUFFIX", "_design")
	send(false, "W1", "O1", "完成：a", "--status", "DONE", "--responds-to", "op/O1-000")
	has(t, view(), "PAUSED   op/O1-000", "未送达的回报不结清原请求")
	t.Setenv("FAKE_WORKING_SUFFIX", "")

	// 同步请求首次未送达，作者重投时仍等待答复。
	t.Setenv("FAKE_WORKING_SUFFIX", "_execute")
	send(false, "O1", "W1", "确认：b", "--reply", "sync")
	t.Setenv("FAKE_WORKING_SUFFIX", "")
	e.mustRun("send", "--team", id, "--from", "O1", "--resend", "op/O1-003")
	argv := e.argvs()[len(e.argvs())-2]
	eq(t, strings.Join(argv[len(argv)-3:], " "), "--wait --timeout 120000", "重投保留同步等待")
}

// rules：只读重读本角色规则；与初始化 prompt 相比去掉 init 段与 mid-task 段，保留裁剪后的协议与映射。
func TestRulesRereadsRoleRulesWithoutInitOrMidTask(t *testing.T) {
	e, id, _ := sendSetup(t, true)
	statePath := e.stateFile()
	before, _ := os.ReadFile(statePath)
	n := e.count("agent", "prompt")

	t.Setenv("HERDR_PANE_ID", e.str("roles", "W1", "pane"))
	out := e.mustRun("rules", "--team", id)
	has(t, out, "你的角色：Worker", "输出本角色模板")
	has(t, out, "### reply 状态机", "保留协议正文")
	has(t, out, "消息实例：Owner=O1", "保留映射")
	has(t, out, "规则命令", "映射含规则命令行")
	hasNot(t, out, "现在：只初始化", "去掉 init 段")
	hasNot(t, out, "任务已在进行中加入", "去掉 mid-task 段")
	hasNot(t, out, "<!--", "标记行不输出")
	hasNot(t, out, "### 独立 Advisor 代收", "协议不含已删除的代收段")
	eq(t, e.count("agent", "prompt"), n, "不投递 prompt")
	after, _ := os.ReadFile(statePath)
	eq(t, string(after), string(before), "不写状态")

	t.Setenv("HERDR_PANE_ID", "")
	has(t, e.mustFail("rules", "--team", id), "无法确定发件人", "无 pane 且无 --from 时拒绝")
	t.Setenv("HERDR_PANE_ID", "w0:p99")
	has(t, e.mustFail("rules", "--team", id, "--from", "W1"), "不是编组", "pane 不是在册成员时拒绝")
	t.Setenv("HERDR_PANE_ID", "")
	e.rowSet(id+"_execute", e.str("roles", "W1", "pane"), "term_other", "codex", "")
	has(t, e.mustFail("rules", "--team", id, "--from", "W1"), "实例身份漂移", "身份不一致时拒绝")
}

// 协议或上下文漂移：整体失败，stdout 不输出半份规则，不写状态。
func TestRulesRejectsDriftWithoutPartialOutput(t *testing.T) {
	e, id, _ := sendSetup(t, false)
	e.stateSet(func(m map[string]any) { m["protocol_version"] = "sha256:stale" })
	before, _ := os.ReadFile(e.stateFile())
	out := e.mustFail("rules", "--team", id, "--from", "W1")
	has(t, out, "当前编组的规则与已安装插件不一致", "协议漂移被拒绝")
	hasNot(t, out, "你的角色：Worker", "不输出半份规则")
	after, _ := os.ReadFile(e.stateFile())
	eq(t, string(after), string(before), "不写状态")
}

// 旧组（无任务目录）的映射也带规则命令行。
func TestMappingExtrasRulesLineForLegacyTeams(t *testing.T) {
	a := &app{st: newStore()}
	has(t, a.mappingExtras(&Team{TeamID: "t1"}), " rules --team t1", "无任务目录时也输出规则命令")
}

// 大正文按 16KB 边界提示；任务目录只在本次新建且数量 ≥5 时提示一次；提示均不阻断投递。
func TestSendHintsForLargeBodyAndManyTasks(t *testing.T) {
	e, id, body := sendSetup(t, false)
	root := filepath.Join(realPath(e.proj), ".nakama", id)
	send := func(task, title, file string) string {
		return e.mustRun("send", "--team", id, "--from", "O1", "--task", task, "--to", "W1", "--title", title, "--file", file)
	}
	// 首次新建 op_x：数量 1，不提示。
	hasNot(t, send("op_x", "派单：首次", body), "本编组已有", "新建但数量不足不提示")
	for _, c := range []struct {
		n    int
		warn bool
	}{{16384, false}, {16385, true}} {
		f := filepath.Join(e.root, fmt.Sprintf("body%d.md", c.n))
		os.WriteFile(f, []byte(strings.Repeat("a", c.n)), 0o644)
		out := send("op_x", "派单：正文边界", f)
		if c.warn {
			has(t, out, "超过 16KB", "16385 字节提示")
		} else {
			hasNot(t, out, "超过 16KB", "16384 字节不提示")
		}
		has(t, out, "delivery=SENT", "提示不阻断投递")
	}
	b, _ := os.ReadFile(filepath.Join(root, "op_x", "O1-002.md"))
	eq(t, len(b) > 16385, true, "正文未截断")

	for _, k := range []string{"t1", "t2", "t3", "t4"} {
		os.MkdirAll(filepath.Join(root, k), 0o700)
	}
	// 已存在任务再次 send 不提示（即使已有 ≥5 个任务目录）。
	hasNot(t, send("op_x", "派单：已存在", body), "本编组已有", "已存在任务不提示")
	hasNot(t, send("t1", "派单：已存在目录", body), "本编组已有", "已存在目录不提示")
	// 本次新建且数量达到 5：提示一次。
	out := send("op_new", "派单：新建", body)
	has(t, out, "本编组已有 6 个任务目录", "新建后数量 ≥5 时提示")
	has(t, out, "delivery=SENT", "提示不阻断投递")
	hasNot(t, send("op_new", "派单：再次", body), "本编组已有", "同一任务再次 send 不提示")
}

// 投递 prompt 末尾带规则命令行；消息文件与台账不带；resend 的投递同样带。
func TestDeliveryPromptCarriesRulesCommand(t *testing.T) {
	e, id, body := sendSetup(t, false)
	worker := id + "_execute"
	e.mustRun("send", "--team", id, "--from", "O1", "--task", "op_x", "--to", "W1", "--title", "派单：规则", "--file", body)
	p := e.lastPrompt(worker)
	has(t, p, "规则原文不在上下文时先运行", "投递 prompt 带提示")
	has(t, p, " rules --team "+id, "投递 prompt 带规则命令行")
	sub := filepath.Join(realPath(e.proj), ".nakama", id, "op_x")
	for _, f := range []string{"O1-000.md", ledgerName} {
		b, _ := os.ReadFile(filepath.Join(sub, f))
		hasNot(t, string(b), "rules --team", f+" 不带规则命令")
	}
	e.mustRun("send", "--team", id, "--from", "O1", "--resend", "op_x/O1-000")
	has(t, e.lastPrompt(worker), " rules --team "+id, "resend 的投递 prompt 同样带规则命令行")
}
