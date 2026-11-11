package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 编组变更（add / drop / swap）、[TEAM_UPDATE] 投递，以及任务目录的创建、列出与清理。
// 不变量：Owner 不可变更；同一时刻只有一个 Worker 实例在册；被替换/移除的实例只记入 retired，
// 不关闭其 pane、不向其发送输入；实例号不复用。

// 取值参数可重复；开关参数出现即记为存在。help=true 表示已输出用法。
func (a *app) parseArgs(args []string, usage string, valued, switches []string) (vals map[string][]string, help bool, err error) {
	vals = map[string][]string{}
	in := func(list []string, s string) bool {
		for _, x := range list {
			if x == s {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			a.outf("%s", usage)
			return nil, true, nil
		case in(switches, arg):
			vals[arg] = []string{""}
		case !in(valued, arg):
			return nil, false, a.fail(L("未知参数：%s"), arg)
		case i+1 >= len(args):
			return nil, false, a.fail(L("参数 %s 缺少取值"), arg)
		default:
			i++
			vals[arg] = append(vals[arg], args[i])
		}
	}
	return vals, false, nil
}

func first(v []string) string {
	if len(v) > 0 {
		return v[0]
	}
	return ""
}

// 变更前置：持 team 锁重读状态，无未确认副作用、无未完成 rebind，锚点仍是记录的那一个。
func (a *app) openChange(cliTeam, op string, needVersions bool) (*Team, *config, error) {
	id, err := a.resolveTeam(cliTeam, "", "")
	if err != nil {
		return nil, nil, err
	}
	if err := a.lock("team-"+id, op); err != nil {
		return nil, nil, err
	}
	t, err := a.st.load(id)
	if err != nil {
		return nil, nil, a.fail("%v", err)
	}
	if !a.pendingGuard(t) {
		return nil, nil, a.fail(L("编组 %s 有未确认副作用（UNKNOWN），不重发；请人工 get/read 核对。"), id)
	}
	if t.RebindPending != nil {
		return nil, nil, a.fail(L("编组 %s 有未完成的 rebind 计划；先完成或放弃后再变更。"), id)
	}
	cfg, err := loadConfig()
	if err != nil {
		return nil, nil, a.fail("%v", err)
	}
	a.piProvider, a.ccEnvFile = cfg.piProvider(), cfg.ccEnvFile()
	if needVersions {
		// 新角色会收到完整初始化 prompt：protocol/context 漂移时零副作用拒绝。
		if err := a.checkVersions(t); err != nil {
			return nil, nil, err
		}
	}
	if err := a.checkAnchorServer(t); err != nil {
		return nil, nil, err
	}
	a.anchor = Anchor{PaneID: t.Anchor.PaneID, WorkspaceID: t.Anchor.WorkspaceID, TabID: t.Anchor.TabID, CWD: t.CWD}
	return t, cfg, nil
}

func (t *Team) retire(rn, op string) {
	r := t.role(rn)
	t.Retired = append(t.Retired, RetiredRole{Role: rn, Name: r.Name, Pane: string(r.Pane), Engine: r.Engine, Instance: r.Instance, At: time.Now().UTC().Format(time.RFC3339), Op: op})
	t.setRole(rn, nil)
}

// 编组发生变更：版本加一，在册且已初始化的角色待投递 [TEAM_UPDATE]。
func (t *Team) markUpdate() {
	t.TeamRev++
	for _, rn := range roleOrder {
		if r := t.role(rn); r != nil && r.Inited {
			r.TeamUpdate = true
		}
	}
}

func (a *app) cmdAdd(args []string) error {
	v, help, err := a.parseArgs(args, L("用法：nakama add --team <id> [--engine E]\n"), []string{"--team", "--engine"}, nil)
	if help || err != nil {
		return err
	}
	t, cfg, err := a.openChange(first(v["--team"]), "add", true)
	if err != nil {
		return err
	}
	if t.Phase == "changing" {
		return a.finishPrevious(t)
	}
	if t.Phase != "initialized" {
		return a.fail(L("编组 %s 尚未完成初始化（phase=%s）；先 init 再变更。"), t.TeamID, t.Phase)
	}
	if t.hasAdvisor() {
		return a.fail(L("编组 %s 已有独立 Advisor；替换请用 swap --role advisor。"), t.TeamID)
	}
	return a.changeRole(t, cfg, "advisor", first(v["--engine"]))
}

func (a *app) cmdSwap(args []string) error {
	v, help, err := a.parseArgs(args, L("用法：nakama swap --team <id> --role worker|advisor [--engine E]\n"), []string{"--team", "--role", "--engine"}, nil)
	if help || err != nil {
		return err
	}
	rn := first(v["--role"])
	if rn != "worker" && rn != "advisor" {
		return a.fail(L("--role 只能是 worker 或 advisor（Owner 不可替换）。"))
	}
	t, cfg, err := a.openChange(first(v["--team"]), "swap", true)
	if err != nil {
		return err
	}
	if t.Phase == "changing" {
		return a.finishPrevious(t)
	}
	if t.Phase != "initialized" {
		return a.fail(L("编组 %s 尚未完成初始化（phase=%s）；先 init 再变更。"), t.TeamID, t.Phase)
	}
	if t.role(rn) == nil {
		return a.fail(L("编组 %s 没有独立 Advisor；请用 add。"), t.TeamID)
	}
	return a.changeRole(t, cfg, rn, first(v["--engine"]))
}

// 新增或替换一个角色实例：全部校验通过后才写状态，随后 split + 启动；初始化仍由 init 完成。
func (a *app) changeRole(t *Team, cfg *config, rn, cliEngine string) error {
	old := t.role(rn)
	raw := cliEngine
	if raw == "" && old != nil {
		raw = old.Engine
	}
	eng, err := cfg.pickEngine(rn, raw)
	if err != nil {
		return a.fail("%v", err)
	}
	if _, ok := parseEngine(eng); !ok || eng == "none" {
		return a.fail(L("无法识别 %s 引擎：%s"), rn, eng)
	}
	if !a.requireEngine(eng) {
		return a.fail(L("%s 引擎前置不满足，停止（未创建任何 pane）。"), rn)
	}
	inst := t.nextInstance(rn)
	name := roleName(t.Prefix, rn, inst)
	if !nameValid(name) {
		return a.fail(L("角色名不符合 Herdr NAME 规则（小写字母开头、[a-z0-9_-]、1-32 字符）：%s"), name)
	}
	if old != nil {
		a.warnf(L("旧 %s（%s，pane %s）已停用但未关闭：请确认它已停止写入后自行处理该 pane。"), rn, old.Name, old.Pane)
		t.retire(rn, "swap")
	}
	t.markUpdate()
	t.setRole(rn, &Role{Name: name, Engine: eng, Instance: inst})
	t.Phase = "changing"
	if err := a.save(t); err != nil {
		return err
	}
	return a.resumeChange(t)
}

// 上一次 add / swap 中断：先补齐它，本次参数不执行，补齐后由用户重跑。
func (a *app) finishPrevious(t *Team) error {
	a.warnf(L("编组 %s 有未完成的变更，先补齐它；本次请求未执行，补齐后请重跑。"), t.TeamID)
	if err := a.resumeChange(t); err != nil {
		return err
	}
	return errExit
}

// 补齐变更中缺失的 split / 启动步骤；失败后重跑同一命令或 up 继续。Owner 不可变更，不在补齐范围。
func (a *app) resumeChange(t *Team) error {
	for _, rn := range []string{"worker", "advisor"} {
		if r := t.role(rn); r != nil && (r.Pane == "" || !r.Launched) {
			if err := a.ensureRole(t, rn); err != nil {
				a.errf(L("编组变更未完成；核对后重跑同一命令或 up 继续。"))
				return err
			}
		}
	}
	t.Phase = "launched"
	if err := a.save(t); err != nil {
		return err
	}
	a.outf(L("编组 %s 已变更（team_rev=%d）。\n"), t.TeamID, t.TeamRev)
	a.printNext(t)
	return nil
}

func (a *app) cmdDrop(args []string) error {
	v, help, err := a.parseArgs(args, L("用法：nakama drop --team <id>\n"), []string{"--team"}, nil)
	if help || err != nil {
		return err
	}
	t, _, err := a.openChange(first(v["--team"]), "drop", false)
	if err != nil {
		return err
	}
	if t.Phase == "changing" {
		return a.fail(L("编组 %s 的变更尚未完成（phase=changing）；先重跑 add / swap 或 up 补齐，再 init。"), t.TeamID)
	}
	if t.Phase != "initialized" && t.Phase != "launched" {
		return a.fail(L("编组 %s 尚未完成初始化（phase=%s）；先 init 再变更。"), t.TeamID, t.Phase)
	}
	if !t.hasAdvisor() {
		return a.fail(L("编组 %s 没有独立 Advisor；请用 add。"), t.TeamID)
	}
	old := t.Roles.Advisor
	a.warnf(L("旧 %s（%s，pane %s）已停用但未关闭：请确认它已停止写入后自行处理该 pane。"), "advisor", old.Name, old.Pane)
	t.retire("advisor", "drop")
	t.markUpdate()
	if err := a.save(t); err != nil {
		return err
	}
	a.outf(L("编组 %s 已变更（team_rev=%d）。\n"), t.TeamID, t.TeamRev)
	if !a.deliverTeamUpdates(t) {
		return errExit
	}
	return nil
}

// 向待通知的在册角色各投递一次 [TEAM_UPDATE]。只投给身份核验通过且 idle/done 的角色；
// 其余保留标记，由下一次 init 再投。返回 false 表示仍有未投递项。
func (a *app) deliverTeamUpdates(t *Team) bool {
	all := true
	for _, rn := range []string{"worker", "advisor", "owner"} {
		r := t.role(rn)
		if r == nil || !r.TeamUpdate {
			continue
		}
		if !r.Inited || r.Stale || r.Identity == nil {
			all = false
			continue
		}
		obj, msg := a.verifyIdentity(t, rn, r.Identity, false)
		if msg != "" {
			if filled, ok := fillSession(*r.Identity, obj.ident()); ok {
				r.Identity, msg = &filled, ""
			}
		}
		if msg == "" && obj.AgentStatus != "idle" && obj.AgentStatus != "done" {
			msg = fmt.Sprintf(L("%s 当前状态为 %s"), r.Name, obj.AgentStatus)
		}
		if msg != "" {
			all = false
			a.errf(L("%s；[TEAM_UPDATE] 未投递，待其空闲后重跑 init。"), msg)
			continue
		}
		prompt, err := a.teamUpdatePrompt(t, rn)
		if err != nil || a.interrupted() {
			return false
		}
		t.setPending(rn, "team_update", "NOT_SENT")
		if a.save(t) != nil {
			return false
		}
		if res := a.herdrPass("agent", "prompt", r.Name, prompt); !res.ok() {
			if errorCode(res.stderr) == "agent_blocked" {
				// 对话框前被拒，输入未发出：清 pending，保留标记。
				t.Pending = nil
				if a.save(t) != nil {
					return false
				}
				all = false
				a.errf(L("%s；[TEAM_UPDATE] 未投递，待其空闲后重跑 init。"), fmt.Sprintf(L("%s 当前状态为 %s"), r.Name, "blocked"))
				continue
			}
			t.Pending.Delivery = "UNKNOWN"
			_ = a.save(t)
			return false
		}
		r.TeamUpdate, t.Pending = false, nil
		if a.save(t) != nil {
			return false
		}
		a.outf(L("已向 %s 投递 [TEAM_UPDATE]（team_rev=%d）。\n"), r.Name, t.TeamRev)
	}
	return all
}

// ---- 任务目录 ----
// <cwd>/.nakama/<team_id>/<task_id>/：编组层由插件创建并记录在状态里，任务层由 Owner 创建。
// .nakama/.gitignore 写 "*"，整个目录不进 git status / diff。

func taskDirFor(t *Team) string { return filepath.Join(t.CWD, ".nakama", t.TeamID) }

func (a *app) createTaskDir(t *Team) error {
	dir := taskDirFor(t)
	// 仓库里可能预置 .nakama 符号链接：跟随会把目录和 .gitignore 建到项目外，且之后 clean 也删不了。
	for _, p := range []string{filepath.Dir(dir), dir} {
		if _, err := os.Lstat(p); err == nil && !realDir(p) {
			a.warnf(L("%s 不是真实目录（可能是符号链接）；本编组不使用任务目录，正文仍内联在消息里。"), p)
			return nil
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return a.fail(L("无法创建任务目录 %s：%v"), dir, err)
	}
	gi := filepath.Join(t.CWD, ".nakama", ".gitignore")
	if _, err := os.Lstat(gi); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(gi, []byte("*\n"), 0o600); err != nil {
			return a.fail(L("无法创建任务目录 %s：%v"), dir, err)
		}
	}
	t.TaskDir = dir
	return a.save(t)
}

type taskInfo struct {
	name  string
	files int
	size  int64
	mod   time.Time
}

var taskNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// 是真实目录（符号链接不算）。
func realDir(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.IsDir()
}

// 只认状态里记录、且等于 <cwd>/.nakama/<team_id> 的目录；路径上任何一级是符号链接都不列出。
func (a *app) listTasks(t *Team) []taskInfo {
	if t.TaskDir == "" || t.TaskDir != taskDirFor(t) || !realDir(filepath.Dir(t.TaskDir)) || !realDir(t.TaskDir) {
		return nil
	}
	ents, _ := os.ReadDir(t.TaskDir)
	var out []taskInfo
	for _, e := range ents {
		if !e.IsDir() || !taskNameRe.MatchString(e.Name()) {
			continue
		}
		k := taskInfo{name: e.Name()}
		_ = filepath.WalkDir(filepath.Join(t.TaskDir, e.Name()), func(_ string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if fi, err := d.Info(); err == nil {
				if fi.ModTime().After(k.mod) {
					k.mod = fi.ModTime()
				}
				if fi.Mode().IsRegular() {
					k.files++
					k.size += fi.Size()
				}
			}
			return nil
		})
		out = append(out, k)
	}
	return out
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%dB", n)
}

// clean：列出任务目录并按选择删除。只删 listTasks 列出的条目；不按时间自动清理。
func (a *app) cmdClean(args []string) error {
	v, help, err := a.parseArgs(args, L("用法：nakama clean [--team <id>] [--task <task_id>]... [--all] [--yes] [--force]\n"),
		[]string{"--team", "--task"}, []string{"--all", "--yes", "--force"})
	if help || err != nil {
		return err
	}
	_, all := v["--all"]
	_, yes := v["--yes"]
	_, force := v["--force"]
	ids := a.st.list()
	if id := first(v["--team"]); id != "" {
		if !a.st.exists(id) {
			return a.fail(L("编组不存在：%s"), id)
		}
		ids = []string{id}
	} else if len(v["--task"]) > 0 {
		return a.fail(L("--task 需要同时指定 --team。"))
	}

	type row struct{ team, task string }
	var rows []row
	for _, id := range ids {
		t, err := a.st.load(id)
		if err != nil {
			a.errf("%v", err)
			continue
		}
		for _, k := range a.listTasks(t) {
			rows = append(rows, row{id, k.name})
			a.outf("%3d  %s/%s  files=%d size=%s modified=%s\n", len(rows), id, k.name, k.files, humanSize(k.size), k.mod.Format("2006-01-02 15:04"))
		}
	}
	if len(rows) == 0 {
		a.outf("%s", L("（没有可清理的任务目录）\n"))
		return nil
	}

	var picked []row
	switch {
	case len(v["--task"]) > 0:
		for _, name := range v["--task"] {
			found := false
			for _, r := range rows {
				if r.task == name {
					picked, found = append(picked, r), true
				}
			}
			if !found {
				return a.fail(L("任务目录不存在：%s"), name)
			}
		}
	case all:
		picked = rows
	case !stdinIsTTY():
		a.outf("%s", L("未删除任何内容：用 --team <id> --task <task_id> 或 --all 选择，并加 --yes 确认。\n"))
		return nil
	default:
		a.outf("%s", L("输入要清理的编号（空格分隔，all 为全部，回车取消）: "))
		in, err := a.readLine()
		if err != nil || in == "" {
			a.outf("%s", L("已取消。\n"))
			return nil
		}
		if in == "all" {
			picked = rows
			break
		}
		for _, f := range strings.Fields(strings.ReplaceAll(in, ",", " ")) {
			n, err := strconv.Atoi(f)
			if err != nil || n < 1 || n > len(rows) {
				return a.fail(L("未知选项：%s"), f)
			}
			picked = append(picked, rows[n-1])
		}
	}

	if !yes {
		if !stdinIsTTY() {
			return a.fail(L("非交互环境需要 --yes 确认；未删除任何内容。"))
		}
		for _, r := range picked {
			a.outf("  %s/%s\n", r.team, r.task)
		}
		a.outf("%s", L("将永久删除以上任务目录，确认？[y/N] "))
		ans, err := a.readLine()
		if l := strings.ToLower(ans); err != nil || (l != "y" && l != "yes") {
			a.outf("%s", L("已取消。\n"))
			return nil
		}
	}

	failed := false
	locked := map[string]*Team{}
	done := map[row]bool{}
	for _, r := range picked {
		if done[r] {
			continue
		}
		done[r] = true
		t, seen := locked[r.team]
		if !seen {
			// 锁内重读：与正在 init / 变更的进程互斥；Worker / Advisor 仍在执行时目录是恢复依据，默认不删。
			if a.lock("team-"+r.team, "clean") == nil {
				if t, err = a.st.load(r.team); err != nil {
					a.errf("%v", err)
					t = nil
				}
			}
			if t != nil && !force && !a.tasksIdle(t) {
				t = nil
			}
			locked[r.team] = t
		}
		if t == nil {
			failed = true
			continue
		}
		ok := false
		for _, k := range a.listTasks(t) {
			ok = ok || k.name == r.task
		}
		p := filepath.Join(t.TaskDir, r.task)
		if !ok || !realDir(p) {
			a.errf(L("任务目录不存在：%s"), r.task)
			failed = true
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			a.errf("%v", err)
			failed = true
			continue
		}
		a.outf(L("已删除 %s\n"), p)
	}
	if failed {
		return errExit
	}
	return nil
}

// 任务目录的写入者（在册与已停用的 Worker / Advisor）都不在执行才可清理。在册角色状态查不到（Herdr 不可用、
// 改名丢失）按忙碌处理；已停用实例的 pane 可能已被用户关闭，查不到视为已停止。Owner 正是执行 clean 的一方，不检查。
func (a *app) tasksIdle(t *Team) bool {
	type w struct {
		name    string
		retired bool
	}
	var ws []w
	for _, rn := range []string{"worker", "advisor"} {
		if r := t.role(rn); r != nil {
			ws = append(ws, w{r.Name, false})
		}
	}
	for _, x := range t.Retired {
		ws = append(ws, w{x.Name, true})
	}
	for _, x := range ws {
		o, ok := a.agentGet(x.name)
		switch {
		case !ok && !x.retired:
			a.errf(L("无法确认 %s 的状态；任务目录仍可能在使用，未清理编组 %s（确需清理加 --force）。"), x.name, t.TeamID)
			return false
		case ok && (o.AgentStatus == "working" || o.AgentStatus == "blocked"):
			a.errf(L("%s 当前状态为 %s；任务目录仍可能在使用，未清理编组 %s（确需清理加 --force）。"), x.name, o.AgentStatus, t.TeamID)
			return false
		}
	}
	return true
}
