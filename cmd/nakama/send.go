package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// send：成员之间的业务消息投递。分配请求号、把正文落盘到任务目录、核对收件人身份与空闲状态、投递并记台账。
// 不取 team 锁：只读编组状态；请求号靠硬链接的排他语义在任务目录内无锁分配。
// send 由 agent 高频调用且随时可能被中断，而本插件的锁不自动接管，持锁会让一次中断卡死整个编组。

// message 是台账（<任务目录>/<task_id>/ledger.jsonl）的一行；同一 req 以最后一行的 to / delivery 为准。
type message struct {
	At       string            `json:"at"`
	Req      string            `json:"req"`
	From     string            `json:"from"`
	To       string            `json:"to"`
	Title    string            `json:"title"`
	Fields   map[string]string `json:"fields,omitempty"`
	File     string            `json:"file"`
	Delivery string            `json:"delivery"` // NOT_SENT | SENT | UNKNOWN | SYNC（同步答复，不反向投递）
	Note     string            `json:"note,omitempty"`
	Via      string            `json:"via,omitempty"` // 代收转交者
}

const ledgerName = "ledger.jsonl"

var (
	msgIDRe    = regexp.MustCompile(`^[OWA][1-9][0-9]*-[0-9]{3,}$`)
	fieldKeyRe = regexp.MustCompile(`^[a-z][a-z_]{0,31}$`)
	// 由专用参数或命令本身决定的字段，不允许经 --set 覆盖。
	reservedFields = map[string]bool{"task": true, "req": true, "from": true, "to": true, "file": true, "status": true, "responds_to": true, "reply": true, "relayed_by": true}
	fieldOrder     = []string{"status", "responds_to", "reply", "reply_to"}
)

// 消息头：收件人看到的 prompt 与正文文件的开头一致，只用语言无关的 key=value。
func (m message) header() string {
	task, inst, _ := strings.Cut(m.Req, "/")
	kv := []string{"task=" + task, "req=" + m.Req, "from=" + m.From, "to=" + m.To}
	seen := map[string]bool{}
	for _, k := range fieldOrder {
		if v := m.Fields[k]; v != "" {
			kv, seen[k] = append(kv, k+"="+v), true
		}
	}
	var rest []string
	for k := range m.Fields {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		kv = append(kv, k+"="+m.Fields[k])
	}
	if m.Via != "" {
		kv = append(kv, "relayed_by="+m.Via)
	}
	return fmt.Sprintf("[%s｜%s]\n%s", inst, m.Title, strings.Join(kv, " "))
}

func readLedger(sub string) []message {
	b, _ := os.ReadFile(filepath.Join(sub, ledgerName))
	var out []message
	for _, l := range strings.Split(string(b), "\n") {
		var m message
		if json.Unmarshal([]byte(l), &m) == nil && m.Req != "" {
			out = append(out, m)
		}
	}
	return out
}

func (a *app) appendLedger(sub string, m message) error {
	p := filepath.Join(sub, ledgerName)
	if st, err := os.Lstat(p); err == nil && !st.Mode().IsRegular() {
		return a.fail(L("台账 %s 不是普通文件；拒绝写入。"), p)
	}
	m.At = time.Now().UTC().Format(time.RFC3339)
	line, _ := json.Marshal(m)
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_, err = f.Write(append(line, '\n'))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		return a.fail(L("台账写入失败：%v"), err)
	}
	return nil
}

// 任务子目录：只认状态里记录的任务目录，路径上任何一级是符号链接都拒绝（与 listTasks 一致）。
func (a *app) taskSubdir(t *Team, task string, create bool) (string, error) {
	if t.TaskDir == "" || t.TaskDir != taskDirFor(t) || !realDir(filepath.Dir(t.TaskDir)) || !realDir(t.TaskDir) {
		return "", a.fail(L("编组 %s 没有可用的任务目录；send 不可用。"), t.TeamID)
	}
	if !taskNameRe.MatchString(task) {
		return "", a.fail(L("task_id 只能用字母、数字、点、下划线和连字符：%s"), task)
	}
	sub := filepath.Join(t.TaskDir, task)
	if create {
		if err := os.Mkdir(sub, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", a.fail(L("无法创建任务目录 %s：%v"), sub, err)
		}
	}
	if !realDir(sub) {
		return "", a.fail(L("任务目录不存在：%s"), task)
	}
	return sub, nil
}

// 成员引用：成员键（不分大小写）或角色名。
func (a *app) memberRef(t *Team, s string) (string, error) {
	if k := strings.ToUpper(s); t.role(k) != nil {
		return k, nil
	}
	for _, k := range t.members() {
		if t.role(k).Name == s {
			return k, nil
		}
	}
	return "", a.fail(L("编组 %s 没有在册成员 %s（已停用的实例不再收发消息）。"), t.TeamID, s)
}

// 发件人：当前 pane 对应的成员；拿不到 pane 时才用 --from，两者都有则必须一致。
func (a *app) senderKey(t *Team, cliFrom string) (string, error) {
	byPane, pane := "", os.Getenv("HERDR_PANE_ID")
	for _, k := range t.members() {
		if pane != "" && string(t.role(k).Pane) == pane {
			byPane = k
		}
	}
	// 有 pane 却不是在册成员（如已停用的实例）：不能靠 --from 借用其他成员的名义。
	if pane != "" && byPane == "" {
		return "", a.fail(L("当前 pane %s 不是编组 %s 的在册成员，不能发送。"), pane, t.TeamID)
	}
	if cliFrom == "" {
		if byPane == "" {
			return "", a.fail(L("无法确定发件人：当前 pane 不是编组 %s 的成员，请用 --from 指定成员键。"), t.TeamID)
		}
		return byPane, nil
	}
	from, err := a.memberRef(t, cliFrom)
	if err != nil {
		return "", err
	}
	if byPane != "" && byPane != from {
		return "", a.fail(L("--from %s 与当前 pane 的成员 %s 不一致。"), from, byPane)
	}
	return from, nil
}

// 分配请求号并落盘：序号取本实例已有文件的最大值加一，硬链接已存在就换下一个号重试。
func (a *app) allocMessage(sub string, m *message, task string, body []byte) error {
	next := 0
	ents, _ := os.ReadDir(sub)
	for _, e := range ents {
		id := strings.TrimSuffix(e.Name(), ".md")
		if inst, seq, ok := strings.Cut(id, "-"); ok && inst == m.From && msgIDRe.MatchString(id) {
			if n, _ := strconv.Atoi(seq); n >= next {
				next = n + 1
			}
		}
	}
	for tries := 0; tries < 100; tries, next = tries+1, next+1 {
		id := fmt.Sprintf("%s-%03d", m.From, next)
		m.Req, m.File = task+"/"+id, filepath.Join(sub, id+".md")
		tmp, err := os.CreateTemp(sub, "."+id+".tmp.*")
		if err != nil {
			return a.fail(L("无法写入消息文件：%v"), err)
		}
		_, err = tmp.Write(append([]byte(m.header()+"\n\n"), body...))
		if cerr := tmp.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Link(tmp.Name(), m.File)
		}
		os.Remove(tmp.Name())
		if err == nil {
			return nil
		}
		if !errors.Is(err, os.ErrExist) {
			return a.fail(L("无法写入消息文件：%v"), err)
		}
	}
	return a.fail(L("无法分配请求号：%s 的序号持续冲突。"), m.From)
}

// deliver 向在册收件人投递一次：只有 idle/done 才投递，working 有界等待一次；返回 delivery 与原因。
// 投递前先记 UNKNOWN：进程在 prompt 途中被杀时，台账不会把一次可能已送达的投递记成未发送。
func (a *app) deliver(t *Team, sub string, m *message, sync bool) (string, string) {
	r := t.role(m.To)
	if !r.Inited || r.Stale || r.Identity == nil {
		return "NOT_SENT", fmt.Sprintf(L("%s 未完成初始化或身份待核对"), m.To)
	}
	// 本次发送的身份基准：空 session 首次补全后固定下来，之后的观测都与它比较。
	base := *r.Identity
	check := func() (string, string) {
		obj, msg := a.verifyIdentity(t, m.To, &base, false)
		if msg != "" {
			if filled, ok := fillSession(base, obj.ident()); ok {
				base, msg = filled, ""
			}
		}
		return obj.AgentStatus, msg
	}
	// 空闲要连续观测到两次才算数：Herdr 对一轮进行中的 agent 偶尔会短暂报出 idle/done。
	settled := func() (string, string) {
		st, msg := check()
		if msg == "" && (st == "idle" || st == "done") {
			sleepFor(a.poll)
			st, msg = check()
		}
		return st, msg
	}
	st, msg := settled()
	if msg == "" && st == "working" {
		a.herdr("agent", "wait", r.Name, "--timeout", "60000")
		st, msg = settled()
	}
	switch {
	case msg != "":
		return "NOT_SENT", msg
	case st != "idle" && st != "done":
		return "NOT_SENT", fmt.Sprintf(L("%s 当前状态为 %s"), r.Name, st)
	case a.interrupted():
		return "NOT_SENT", "interrupted"
	}
	// 等待期间成员可能被移除或替换（send 不取锁）：投递前重读在册状态。只缩小窗口，重读与投递之间仍可能变更。
	if cur, err := a.st.load(t.TeamID); err != nil || cur.role(m.To) == nil || cur.role(m.To).Name != r.Name || cur.role(m.To).Pane != r.Pane {
		return "NOT_SENT", fmt.Sprintf(L("编组 %s 没有在册成员 %s（已停用的实例不再收发消息）。"), t.TeamID, m.To)
	}
	m.Delivery, m.Note = "UNKNOWN", "in-flight"
	if a.appendLedger(sub, *m) != nil {
		return "NOT_SENT", "ledger"
	}
	args := []string{"agent", "prompt", r.Name, m.header() + "\nfile=" + m.File}
	if sync {
		args = append(args, "--wait", "--timeout", "120000")
	}
	res := a.herdr(args...)
	code := errorCode(res.stderr)
	switch {
	case res.ok():
		return "SENT", ""
	case code == "agent_blocked":
		// Herdr 在对话框前拒绝且未发送任何输入：确证未送达。
		return "NOT_SENT", fmt.Sprintf(L("%s 当前状态为 %s"), r.Name, "blocked")
	case sync && code == "timeout":
		return "SENT", "read_pending"
	}
	if code == "" {
		code = "error"
	}
	return "UNKNOWN", code
}

func (a *app) finishSend(t *Team, sub string, m *message, sync bool) error {
	m.Delivery, m.Note = a.deliver(t, sub, m, sync)
	if err := a.appendLedger(sub, *m); err != nil {
		return err
	}
	a.outf("req=%s delivery=%s to=%s file=%s\n", m.Req, m.Delivery, m.To, m.File)
	switch {
	case m.Delivery == "SENT" && m.Note == "read_pending":
		return a.fail(L("已投递，但 120 秒内未取得答复（READ_PENDING）：按协议 get/read 补读，不重发。"))
	case m.Delivery == "SENT":
		if sync {
			a.herdrPass("agent", "read", t.role(m.To).Name, "--source", "recent-unwrapped", "--lines", "120")
		}
		return nil
	case m.Delivery == "UNKNOWN":
		return a.fail(L("投递结果不明（%s）：先 get/read 核对是否已送达，确认未送达再 --resend %s。"), m.Note, m.Req)
	}
	return a.fail(L("未投递（%s）：正文与请求号已保留，稍后用 --resend %s 补投。"), m.Note, m.Req)
}

func (a *app) cmdSend(args []string) error {
	v, help, err := a.parseArgs(args,
		L("用法：nakama send --team <id> --task <task_id> --to <成员键> --title <动作：对象> --file <正文文件>\n            [--status DONE|BLOCKED|CONTEXT_READY] [--responds-to <req>] [--reply sync|async] [--set key=value]... [--no-deliver] [--from <成员键>]\n       nakama send --team <id> --resend <req> [--to <成员键>] [--from <成员键>]\n"),
		[]string{"--team", "--from", "--to", "--task", "--title", "--file", "--status", "--responds-to", "--reply", "--set", "--resend"}, []string{"--no-deliver"})
	if help || err != nil {
		return err
	}
	id, err := a.resolveTeam(first(v["--team"]), "", "")
	if err != nil {
		return err
	}
	t, err := a.st.load(id)
	if err != nil {
		return a.fail("%v", err)
	}
	from, err := a.senderKey(t, first(v["--from"]))
	if err != nil {
		return err
	}
	// 发件人也按冻结身份核验：pane 被别的实例占用后，不能再以原成员名义写台账、发消息。
	sr := t.role(from)
	if !sr.Inited || sr.Stale || sr.Identity == nil {
		return a.fail(L("发件人 %s 尚未初始化。"), from)
	}
	if obj, msg := a.verifyIdentity(t, from, sr.Identity, false); msg != "" {
		if _, ok := fillSession(*sr.Identity, obj.ident()); !ok {
			return a.fail("%s", msg)
		}
	}
	if req := first(v["--resend"]); req != "" {
		for k := range v {
			if k != "--team" && k != "--from" && k != "--to" && k != "--resend" {
				return a.fail(L("--resend 只能与 --team / --from / --to 同用。"))
			}
		}
		return a.resend(t, from, req, first(v["--to"]))
	}

	task, title, file := first(v["--task"]), first(v["--title"]), first(v["--file"])
	if task == "" || title == "" || file == "" || first(v["--to"]) == "" {
		return a.fail(L("send 需要 --task、--to、--title 和 --file。"))
	}
	if strings.ContainsAny(title, "\r\n") {
		return a.fail(L("--title 不能换行。"))
	}
	to, err := a.memberRef(t, first(v["--to"]))
	if err != nil {
		return err
	}
	if to == from {
		return a.fail(L("不能发给自己（%s）。"), from)
	}
	fields := map[string]string{}
	if s := first(v["--status"]); s != "" {
		if s != "DONE" && s != "BLOCKED" && s != "CONTEXT_READY" {
			return a.fail(L("--status 只能是 DONE、BLOCKED 或 CONTEXT_READY。"))
		}
		fields["status"] = s
	}
	if s := first(v["--reply"]); s != "" {
		if s != "sync" && s != "async" {
			return a.fail(L("--reply 只能是 sync 或 async。"))
		}
		// 回报不是请求：带 --status 的消息若再要求答复，会在状态视图里变成一条永远等不到答复的请求。
		if fields["status"] != "" {
			return a.fail(L("--status 与 --reply 不能同用：回报不要求对方再答复。"))
		}
		fields["reply"], fields["reply_to"] = s, from
	}
	if s := first(v["--responds-to"]); s != "" {
		if tk, inst, _ := strings.Cut(s, "/"); !taskNameRe.MatchString(tk) || !msgIDRe.MatchString(inst) {
			return a.fail(L("--responds-to 须为完整请求号（如 op_x/O1-001）：%s"), s)
		}
		fields["responds_to"] = s
	}
	for _, kv := range v["--set"] {
		k, val, ok := strings.Cut(kv, "=")
		if !ok || !fieldKeyRe.MatchString(k) || val == "" || strings.ContainsAny(val, " \t\r\n") || (reservedFields[k]) {
			return a.fail(L("--set 须为 key=value（key 为小写字母和下划线，value 不含空白，且不是专用参数已有的字段）：%s"), kv)
		}
		fields[k] = val
	}
	if !readableFile(file) {
		return a.fail(L("正文文件不是普通可读文件：%s"), file)
	}
	body, err := os.ReadFile(file)
	if err != nil || strings.TrimSpace(string(body)) == "" {
		return a.fail(L("正文文件为空或不可读：%s"), file)
	}
	sub, err := a.taskSubdir(t, task, true)
	if err != nil {
		return err
	}
	_, noDeliver := v["--no-deliver"]
	m := message{From: from, To: to, Title: title, Fields: fields, Delivery: "NOT_SENT"}
	if noDeliver {
		m.Delivery = "SYNC"
	}
	if err := a.allocMessage(sub, &m, task, body); err != nil {
		return err
	}
	if err := a.appendLedger(sub, m); err != nil {
		return err
	}
	if noDeliver {
		a.outf("req=%s delivery=%s to=%s file=%s\n", m.Req, m.Delivery, m.To, m.File)
		return nil
	}
	return a.finishSend(t, sub, &m, fields["reply"] == "sync")
}

// resend：沿用原请求号重投（重试、RESULT_REPLAY 补报）；发件人不是原作者时即代收转交，原 from 不变。
func (a *app) resend(t *Team, from, req, cliTo string) error {
	task, inst, _ := strings.Cut(req, "/")
	if !msgIDRe.MatchString(inst) {
		return a.fail(L("--resend 须为完整请求号（如 op_x/W1-003）：%s"), req)
	}
	sub, err := a.taskSubdir(t, task, false)
	if err != nil {
		return err
	}
	var m *message
	for _, e := range readLedger(sub) {
		if e.Req == req {
			m = &e
		}
	}
	if m == nil || m.File != filepath.Join(sub, inst+".md") || !readableFile(m.File) {
		return a.fail(L("台账中没有可重投的消息：%s"), req)
	}
	if cliTo != "" {
		if m.To, err = a.memberRef(t, cliTo); err != nil {
			return err
		}
	} else if t.role(m.To) == nil {
		return a.fail(L("编组 %s 没有在册成员 %s（已停用的实例不再收发消息）。"), t.TeamID, m.To)
	}
	if m.To == from {
		return a.fail(L("不能发给自己（%s）。"), from)
	}
	m.Via = ""
	if from != m.From {
		m.Via = from
	}
	return a.finishSend(t, sub, m, m.Via == "" && m.Fields["reply"] == "sync")
}

// status 的任务视图：按台账推导未答复请求与未送达消息。只读，是给人和压缩后的角色看的线索；
// 请求是否结清仍由各角色按协议裁决。
func (a *app) statusTasks(t *Team) {
	for _, k := range a.listTasks(t) {
		last, order := map[string]message{}, []string{}
		for _, e := range readLedger(filepath.Join(t.TaskDir, k.name)) {
			if _, ok := last[e.Req]; !ok {
				order = append(order, e.Req)
			}
			last[e.Req] = e
		}
		if len(order) == 0 {
			continue
		}
		var lines []string
		for _, req := range order {
			m := last[req]
			if m.Delivery == "NOT_SENT" || m.Delivery == "UNKNOWN" {
				lines = append(lines, fmt.Sprintf("    %-8s %s %s→%s %s", m.Delivery, req, m.From, m.To, m.Title))
			}
			// 确证没送出去的请求只列为未送达，不再同时算作等待答复。
			if m.Fields["reply"] == "" || m.Delivery == "NOT_SENT" {
				continue
			}
			state, event, cancel := "WAITING", "", ""
			for _, other := range order {
				x := last[other].Fields
				if d := last[other].Delivery; d == "NOT_SENT" || d == "UNKNOWN" {
					continue
				}
				switch {
				case state == "REPLIED" || state == "SUPERSEDED" || state == "CANCELLED":
				case x["replaces"] == req:
					state = "SUPERSEDED"
				case x["cancels"] == req:
					cancel = other
				case cancel != "" && x["responds_to"] == cancel && x["status"] == "DONE":
					state = "CANCELLED"
				case x["responds_to"] == req && x["status"] == "DONE":
					state = "REPLIED"
				case x["responds_to"] == req && x["status"] == "BLOCKED":
					state, event = "BLOCKED", other
				case x["responds_to"] == req && x["status"] == "CONTEXT_READY":
					state, event = "PAUSED", other
				case event != "" && x["resumes"] == event:
					state, event = "WAITING", ""
				}
			}
			if state == "WAITING" || state == "BLOCKED" || state == "PAUSED" {
				lines = append(lines, fmt.Sprintf("    %-8s %s %s→%s %s", state, req, m.From, m.To, m.Title))
			}
		}
		a.outf("  task %s: messages=%d open=%d\n", k.name, len(order), len(lines))
		for _, l := range lines {
			a.outf("%s\n", l)
		}
	}
}
