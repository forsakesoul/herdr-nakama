package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

const pluginID = "forsakesoul.nakama"

// 测试可替换；生产见 tty_darwin.go。
var stdinIsTTY = realStdinIsTTY

// open-picker：在按键所在 pane 的 context 上取锚点并核验，再打开 overlay。
// overlay 里的进程拿不到按键时的焦点，所以锚点经 env 显式传入；不传 target/workspace/direction（overlay 会被宿主拒绝）。
func (a *app) cmdOpenPicker(args []string) error {
	if len(args) > 0 {
		return a.fail(L("未知参数：%s"), args[0])
	}
	if err := a.normalizeAnchor("", ""); err != nil {
		return err
	}
	r := a.herdr("plugin", "pane", "open", "--plugin", pluginID, "--entrypoint", "picker", "--placement", "overlay",
		"--env", "NAKAMA_ANCHOR_PANE="+a.anchor.PaneID, "--env", "NAKAMA_ANCHOR_CWD="+a.anchor.CWD)
	if !r.ok() {
		a.stderr.Write(r.stderr)
		a.stderr.Write(r.stdout)
		return a.fail(L("打开引擎选择界面失败。"))
	}
	a.outf(L("已打开引擎选择界面（锚点 pane %s）。\n"), a.anchor.PaneID)
	return nil
}

func (a *app) readLine() (string, error) {
	s, err := a.in.ReadString('\n')
	if err != nil {
		// EOF 前没读完一整行（哪怕非空）也算输入中断，不能把半截 "y" 当确认。
		return "", err
	}
	return strings.TrimSpace(s), nil
}

func (a *app) waitEnter() {
	a.outf("%s", L("按回车关闭…"))
	_, _ = a.readLine()
	a.outf("\n")
}

func (a *app) pickReject(format string, v ...any) error {
	a.errf(format, v...)
	a.waitEnter()
	return errExit
}

// 角色视角下的简写展开，稳定排序；只含引擎串，不碰 provider / env 文件等私有字段。
func aliasLines(cfg *config, role string) []string {
	m := asMap(asMap(cfg.root["default_profile"])["engine_map"])
	keys := make([]string, 0, len(m))
	for k := range m {
		if cfg.resolveEngine(k, role) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = fmt.Sprintf("  %s → %s", k, cfg.resolveEngine(k, role))
	}
	return out
}

// 逐角色询问：回车取默认，简写与完整写法都行；解析校验复用 pickEngine/parseEngine。
// 返回传给 cmdUp 的完整引擎串（advisor 为 none 时返回 "none"）。
// def 非空时覆盖 profile 默认（替换角色时默认沿用原引擎）。
func (a *app) askEngine(cfg *config, role, label string, allowNone bool, def string) (string, error) {
	if raw := cfg.profileStr(role); def == "" && raw != "" {
		if d := cfg.resolveEngine(raw, role); allowNone || d != "none" {
			def = d
		}
	}
	a.outf(L("\n%s 可用简写：\n"), label)
	for _, l := range aliasLines(cfg, role) {
		a.outf("%s\n", l)
	}
	for {
		if def != "" {
			a.outf(L("%s 引擎 [默认 %s]: "), label, def)
		} else {
			a.outf(L("%s 引擎: "), label)
		}
		in, err := a.readLine()
		if err != nil {
			return "", a.fail(L("输入结束，已取消（未创建任何内容）。"))
		}
		if in == "" && def == "" {
			a.outf("%s", L("没有默认值，请输入。\n"))
			continue
		}
		eng, err := cfg.pickEngine(role, in)
		if err != nil {
			a.outf("%v\n", err)
			continue
		}
		if eng == "none" || in == "none" {
			if allowNone {
				return "none", nil
			}
			a.outf(L("%s 不能是 none。\n"), label)
			continue
		}
		if _, ok := parseEngine(eng); !ok {
			a.outf(L("无法识别的引擎：%s（写法 <launcher>/<model>[:effort]，或上方简写）。\n"), eng)
			continue
		}
		// cmdUp 会再解析一次传入值；嵌套别名会解出不同结果，摘要就不再可信。
		if cfg.resolveEngine(eng, role) != eng {
			a.outf(L("简写不支持嵌套：%s。\n"), eng)
			continue
		}
		return eng, nil
	}
}

func (a *app) cmdPick(args []string) error {
	if len(args) > 0 {
		return a.fail(L("未知参数：%s"), args[0])
	}
	if !stdinIsTTY() {
		return a.fail(L("pick 需要交互终端（stdin 不是 TTY），拒绝执行。"))
	}
	a.outf("%s", L("nakama：选择引擎并建组（Ctrl-C 取消）\n"))
	// 锚点只信 open-picker 传入的 env；绝不回落 HERDR_PANE_ID（那是 overlay 自身）。
	pane, cwd := os.Getenv("NAKAMA_ANCHOR_PANE"), os.Getenv("NAKAMA_ANCHOR_CWD")
	if pane == "" || cwd == "" {
		return a.pickReject(L("缺少锚点信息（NAKAMA_ANCHOR_PANE / NAKAMA_ANCHOR_CWD）；请经 pick action 打开。"))
	}
	if err := a.normalizeAnchor(pane, cwd); err != nil {
		a.waitEnter()
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return a.pickReject("%v", err)
	}
	// 锚点或任一角色 pane 已属于某个编组：进入管理菜单，不新建。
	id := a.st.findByAnchor(a.anchor.PaneID)
	if id == "" {
		switch ids := a.st.findByPane(a.anchor.PaneID); {
		case len(ids) == 1:
			id = ids[0]
		case len(ids) > 1:
			return a.pickReject(L("当前 pane 同时属于多个编组（%s），请用 --team 指定。"), strings.Join(ids, ", "))
		}
	}
	if id != "" {
		return a.teamMenu(cfg, id)
	}
	a.outf(L("锚点 pane %s\n"), a.anchor.PaneID)

	owner, err := a.askEngine(cfg, "owner", "Owner", false, "")
	if err != nil {
		return err
	}
	worker, err := a.askEngine(cfg, "worker", "Worker", false, "")
	if err != nil {
		return err
	}
	advisor, err := a.askEngine(cfg, "advisor", "Advisor", true, "")
	if err != nil {
		return err
	}
	a.outf(L("\n将以 pane %s（cwd %s）为锚点建组：\n  Owner   %s\n  Worker  %s\n  Advisor %s\n确认建组？[y/N] "), a.anchor.PaneID, a.anchor.CWD, owner, worker, advisor)
	ans, err := a.readLine()
	if err != nil {
		return a.fail(L("输入结束，已取消（未创建任何内容）。"))
	}
	if l := strings.ToLower(ans); l != "y" && l != "yes" {
		a.outf("%s", L("已取消。\n"))
		return nil
	}

	a.createOnly = true
	return a.guarded(func() error {
		return a.cmdUp([]string{"--owner", owner, "--worker", worker, "--advisor", advisor, "--pane", a.anchor.PaneID, "--cwd", a.anchor.CWD})
	}, L("\n建组完成。\n"), L("\n建组失败，见上方信息。\n"))
}

// overlay 内执行有副作用的命令：执行期间才启用 stop-flag 语义；输入阶段让默认信号处理直接终止进程。
// watcher 要覆盖到锁释放之后，否则信号落在两者之间会绕过 defer 遗留锁。
func (a *app) guarded(run func() error, okMsg, failMsg string) error {
	stop := a.watchSignals()
	runErr := run()
	if a.beforeUnlock != nil {
		a.beforeUnlock()
	}
	// 结果页要等回车，先放锁：状态此时已落盘，不能让一个等待中的 overlay 卡住后续 up/init。
	a.unlockAll()
	stop()
	if a.stop.Load() {
		return a.fail(L("被信号中断：已完成的结果已落盘。"))
	}
	if runErr == nil {
		a.outf("%s", okMsg)
	} else {
		a.outf("%s", failMsg)
	}
	a.waitEnter()
	return runErr
}

// 已有编组的管理菜单：增减 Advisor、替换 Worker / Advisor、清理任务目录。引擎仍在创建时冻结，换模型即替换实例。
func (a *app) teamMenu(cfg *config, id string) error {
	t, err := a.st.load(id)
	if err != nil {
		return a.pickReject("%v", err)
	}
	a.outf(L("\n编组 %s（phase=%s，锚点 pane %s）：\n"), id, t.Phase, t.Anchor.PaneID)
	for _, rn := range roleOrder {
		if r := t.role(rn); r != nil {
			a.outf("  %-7s %s\n", rn, r.Engine)
		}
	}
	a.outf("\n")
	if t.hasAdvisor() {
		a.outf("%s", L("  x  移除 Advisor\n  v  替换 Advisor\n"))
	} else {
		a.outf("%s", L("  a  添加 Advisor\n"))
	}
	a.outf("%s", L("  w  替换 Worker（旧 pane 保留，请先确认它已停止写入）\n  c  清理任务目录\n选择 [回车关闭]: "))
	in, err := a.readLine()
	if err != nil {
		return a.fail(L("输入结束，已取消（未创建任何内容）。"))
	}
	var run func() error
	role, label := "", ""
	switch strings.ToLower(in) {
	case "":
		return nil
	case "c":
		err := a.cmdClean([]string{"--team", id})
		a.unlockAll()
		a.waitEnter()
		return err
	case "x":
		if !t.hasAdvisor() {
			return a.pickReject(L("未知选项：%s"), in)
		}
		run = func() error { return a.cmdDrop([]string{"--team", id}) }
	case "a":
		if t.hasAdvisor() {
			return a.pickReject(L("未知选项：%s"), in)
		}
		role, label = "advisor", "Advisor"
	case "v":
		if !t.hasAdvisor() {
			return a.pickReject(L("未知选项：%s"), in)
		}
		role, label = "advisor", "Advisor"
	case "w":
		role, label = "worker", "Worker"
	default:
		return a.pickReject(L("未知选项：%s"), in)
	}
	summary := L("移除 Advisor")
	if role != "" {
		def := ""
		if r := t.role(role); r != nil {
			def = r.Engine
		}
		eng, err := a.askEngine(cfg, role, label, false, def)
		if err != nil {
			return err
		}
		summary = label + " → " + eng
		run = func() error { return a.cmdSwap([]string{"--team", id, "--role", role, "--engine", eng}) }
		if t.role(role) == nil {
			run = func() error { return a.cmdAdd([]string{"--team", id, "--engine", eng}) }
		}
	}
	a.outf(L("\n将变更编组 %s：%s\n确认？[y/N] "), id, summary)
	ans, err := a.readLine()
	if err != nil {
		return a.fail(L("输入结束，已取消（未创建任何内容）。"))
	}
	if l := strings.ToLower(ans); l != "y" && l != "yes" {
		a.outf("%s", L("已取消。\n"))
		return nil
	}
	return a.guarded(run, L("\n编组变更已执行。\n"), L("\n编组变更未完成，见上方信息。\n"))
}
