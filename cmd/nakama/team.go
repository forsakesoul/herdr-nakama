package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func (a *app) save(t *Team) error {
	if err := a.st.save(t); err != nil {
		return a.fail(L("状态写入失败：%v"), err)
	}
	return nil
}

// ---- 项目识别与命名 ----
// 项目短名 ≤8 + 时间戳/随机后缀，未匹配时使用 task。合法性统一在 validateNames 校验。
func (a *app) detectProject(cfg *config, cwd string) (project, error) {
	out, _ := exec.Command("git", "-C", cwd, "remote", "get-url", "origin").Output()
	repo := strings.TrimRight(string(out), "\n")
	if i := strings.LastIndex(repo, "/"); i >= 0 {
		repo = repo[i+1:]
	}
	repo = strings.TrimSuffix(repo, ".git")
	if repo == "" {
		repo = baseName(cwd)
	}
	p, _ := cfg.project(repo)
	if p.short != "" && !prefixValid(p.short) {
		return p, a.fail(L("项目短名非法（须 ^[a-z][a-z0-9]{0,7}$）：%s"), p.short)
	}
	return p, nil
}

func baseName(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// 唯一的名称生成点：未匹配项目时补 task 缺省。
func genPrefix(short string) (string, error) {
	if short == "" {
		short = "task"
	}
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// 混入创建时间，保留 12hex 格式以兼容 Bridge 的任务识别规则。
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d:%x", time.Now().UnixNano(), b)))
	return short + "_" + hex.EncodeToString(hash[:6]), nil
}

// parseCount 解析 1..max（或 lo..max）的人数参数；空串返回 def。
func parseCount(raw string, def, lo, hi int, flag string) (int, error) {
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf(L("%s 须为 %d-%d 的整数：%s"), flag, lo, hi, raw)
	}
	return n, nil
}

// 在任何 split 之前校验全部角色名（Herdr NAME 规则 + ≤32）；失败零副作用。
func (a *app) validateNames(prefix string, nWorkers, nAdvisors int) error {
	names := []string{roleName(prefix, "owner", 1)}
	for i := 1; i <= nWorkers; i++ {
		names = append(names, roleName(prefix, "worker", i))
	}
	for i := 1; i <= nAdvisors; i++ {
		names = append(names, roleName(prefix, "advisor", i))
	}
	for _, n := range names {
		if !nameValid(n) {
			return a.fail(L("角色名不符合 Herdr NAME 规则（小写字母开头、[a-z0-9_-]、1-32 字符）：%s"), n)
		}
	}
	return nil
}

// 引擎运行前置：pi 需 provider、ccenv 需可读普通 env 文件。
func (a *app) requireEngine(raw string) bool {
	eng, ok := parseEngine(raw)
	if !ok {
		return false
	}
	switch eng.launcher {
	case "pi":
		if a.piProvider == "" {
			a.errf(L("pi 引擎缺少 provider：请在 config.json 的 default_profile.pi_provider 填写（私有，勿入库）。"))
			return false
		}
	case "ccenv":
		if !readableFile(a.ccEnvFile) {
			a.errf(L("ccenv 的 env 文件（default_profile.cc_env_file）不是普通可读文件：%s"), a.ccEnvFile)
			return false
		}
	}
	return true
}

// ---- 副作用：split / launch，均先写 pending ----
func (a *app) runSplit(t *Team, rn, base, dir string) (string, error) {
	if a.interrupted() {
		return "", errExit
	}
	// 新 pane 只能开在锚点当前 tab 内：split 前核对 base（零副作用拒绝），split 后再核对新 pane（UNKNOWN 停止）。
	if b := a.herdr("pane", "get", base); !b.ok() {
		a.errf(L("无法读取 base pane %s 的 workspace/tab；拒绝 split。"), base)
		return "", errExit
	} else if msg := tabMismatch(t, parsePane(b.stdout)); msg != "" {
		a.errf("%s", msg)
		return "", errExit
	}
	t.setPending(rn, "split", "NOT_SENT")
	if err := a.save(t); err != nil {
		return "", err
	}
	unknown := func() error { t.Pending.Delivery = "UNKNOWN"; _ = a.save(t); return errExit }
	r := a.herdr("pane", "split", "--pane", base, "--direction", dir, "--cwd", a.anchor.CWD, "--no-focus")
	if !r.ok() {
		return "", unknown()
	}
	pane := parsePane(r.stdout).PaneID
	if pane == "" {
		return "", unknown()
	}
	// pane ID 已发生但尚未证实同 tab：保持 UNKNOWN 落盘，中断后的重跑由 pendingGuard 停止，不会把它当作已确认的 SENT。
	t.role(rn).Pane = NullStr(pane)
	t.Pending.Delivery = "UNKNOWN"
	if err := a.save(t); err != nil {
		return "", err
	}
	if a.hookAfterSplitPersist != nil && a.hookAfterSplitPersist(pane) {
		return "", errExit
	}
	// split 后记录该 pane 的 terminal_id，作为首次冻结的连续性证据，同时核对 tab。
	g := a.herdr("pane", "get", pane)
	if !g.ok() {
		a.errf(L("split 后无法读取 pane %s 的 terminal_id"), pane)
		return "", unknown()
	}
	if msg := tabMismatch(t, parsePane(g.stdout)); msg != "" {
		a.errf("%s", msg)
		return "", unknown()
	}
	term := parsePane(g.stdout).TerminalID
	if term == "" {
		a.errf(L("split 后 pane %s 缺少 terminal_id"), pane)
		return "", unknown()
	}
	t.role(rn).PaneTerminal = term
	t.Pending = nil
	return pane, a.save(t)
}

// tabMismatch：pane 必须与锚点记录处于同一 workspace/tab；记录缺失即拒绝（不因无记录而放行）。空串表示通过。
func tabMismatch(t *Team, p paneObj) string {
	if t.Anchor.WorkspaceID == "" || t.Anchor.TabID == "" {
		return L("编组缺少锚点 workspace/tab 记录，无法证明新 pane 位于锚点 tab 内；拒绝。")
	}
	if p.WorkspaceID != t.Anchor.WorkspaceID || p.TabID != t.Anchor.TabID {
		return fmt.Sprintf(L("pane %s 不在锚点所在的 workspace/tab 内（记录 %s/%s，当前 %s/%s）；拒绝。"),
			p.PaneID, t.Anchor.WorkspaceID, t.Anchor.TabID, p.WorkspaceID, p.TabID)
	}
	return ""
}

// 仅「启动前 agent_pane_busy」可有界重试（最多 6 次）。
// agent_not_ready 表示启动时卡在授权对话框：名字已保留，视为已启动，由取证阶段的 authGate 等待。
func (a *app) startAgent(name, kind, pane string, extra []string) error {
	for attempt := 1; ; attempt++ {
		if attempt > 1 && a.interrupted() {
			return errExit
		}
		args := append([]string{"agent", "start", name, "--kind", kind, "--pane", pane, "--"}, extra...)
		r := a.herdrPass(args...)
		if r.ok() || errorCode(r.stderr) == "agent_not_ready" {
			return nil
		}
		if attempt >= 6 || errorCode(r.stderr) != "agent_pane_busy" {
			return errExit
		}
		if attempt == 1 {
			fmt.Fprintln(a.stderr, L("目标 shell 暂不可用，最多等待重试 5 秒。"))
		}
		sleepFor(a.poll)
	}
}

// 启动后的授权等待上限（NAKAMA_START_WAIT_SECONDS，缺省 600 秒）。
func startWait() time.Duration {
	if n, err := strconv.Atoi(os.Getenv("NAKAMA_START_WAIT_SECONDS")); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return 600 * time.Second
}

func (a *app) launch(raw, name, pane string) error {
	eng, _ := parseEngine(raw)
	switch eng.launcher {
	case "codex", "qoder":
		return a.startAgent(name, eng.kind(), pane, eng.startArgs())
	}
	// env 文件路径会出现在 pane 命令文本中（不是密钥）；其内容绝不拼入命令/状态/prompt/日志。
	if r := a.herdrPass("pane", "run", pane, eng.paneCommand(a.piProvider, a.ccEnvFile)); !r.ok() {
		return errExit
	}
	return nil
}

func (a *app) runLaunch(t *Team, rn string) error {
	if a.interrupted() {
		return errExit
	}
	r := t.role(rn)
	t.setPending(rn, "run", "NOT_SENT")
	if err := a.save(t); err != nil {
		return err
	}
	unknown := func() error { t.Pending.Delivery = "UNKNOWN"; _ = a.save(t); return errExit }
	if a.launch(r.Engine, r.Name, string(r.Pane)) != nil {
		return unknown()
	}
	r.Launched = true
	t.Pending.Step, t.Pending.Delivery = "identity", "NOT_SENT"
	if err := a.save(t); err != nil {
		return err
	}
	if !a.captureIdentity(t, rn) {
		// 取证失败：角色未冻结，标 UNKNOWN 并停止（不重发）。
		return unknown()
	}
	t.Pending = nil
	return a.save(t)
}

// pending 非空且 delivery != SENT → 无法确认副作用，标 UNKNOWN，停止不重发。返回 true=可继续。
func (a *app) pendingGuard(t *Team) bool {
	if t.Pending == nil || t.Pending.Step == "" {
		return true
	}
	if t.Pending.Delivery == "SENT" {
		t.Pending = nil
		return a.save(t) == nil
	}
	// 启动后的取证只读、不是副作用：上次没取到（识别慢、对话框超时）就再取一次，取到即可继续。
	if t.Pending.Step == "identity" && t.role(t.Pending.Role) != nil && a.captureIdentity(t, t.Pending.Role) {
		t.Pending = nil
		return a.save(t) == nil
	}
	d := t.Pending.Delivery
	if d == "" {
		d = "UNKNOWN"
	}
	a.warnf(L("检测到未确认的副作用：role=%s step=%s delivery=%s。不重发；请人工 get/read 核对后处理。"), t.Pending.Role, t.Pending.Step, d)
	t.Pending.Delivery = "UNKNOWN"
	_ = a.save(t)
	return false
}

func (a *app) printNext(t *Team) {
	a.outf("NEXT:\n")
	for _, key := range t.members() {
		r := t.role(key)
		pane := string(r.Pane)
		if pane == "" {
			pane = "?"
		}
		a.outf("- %s %s pane %s：%s\n", kindOf(key), key, pane, a.hint(r.Engine))
	}
	a.outf("%s", L("- 运行 nakama init 完成绑定与初始化\n"))
}

func (a *app) hint(raw string) string {
	if eng, ok := parseEngine(raw); ok {
		return eng.hint()
	}
	return ""
}

// ---- up ----
func (a *app) cmdUp(args []string) error {
	var cliOwner, cliWorker, cliAdvisor, cliWorkers, cliAdvisors, cliPane, cliCwd string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		dst := map[string]*string{"--owner": &cliOwner, "--worker": &cliWorker, "--advisor": &cliAdvisor,
			"--workers": &cliWorkers, "--advisors": &cliAdvisors, "--pane": &cliPane, "--cwd": &cliCwd}[arg]
		switch {
		case arg == "-h" || arg == "--help":
			a.outf("%s", L("用法：nakama up [--owner E] [--worker E] [--workers N] [--advisor E|none] [--advisors N] [--pane P] [--cwd C]\n"))
			return nil
		case dst == nil:
			return a.fail(L("未知参数：%s"), arg)
		case i+1 >= len(args):
			return a.fail(L("参数 %s 缺少取值"), arg)
		default:
			i++
			*dst = args[i]
		}
	}
	cfg, err := loadConfig()
	if err != nil {
		return a.fail("%v", err)
	}
	if err := a.normalizeAnchor(cliPane, cliCwd); err != nil {
		return err
	}
	proj, err := a.detectProject(cfg, a.anchor.CWD)
	if err != nil {
		return err
	}
	a.piProvider, a.ccEnvFile = cfg.piProvider(), cfg.ccEnvFile()

	if existing := a.st.findByAnchor(a.anchor.PaneID); existing != "" {
		if a.createOnly {
			return a.fail(L("该 pane 已有编组 %s，选择界面不恢复/不换模型。"), existing)
		}
		return a.upExisting(existing)
	}
	// 声明了上下文文件却读不到：在任何 split/run 之前拒绝，不留下注定 init 失败的半成品组。
	if proj.contextFile != "" && !readableFile(proj.contextFile) {
		return a.fail(L("声明的上下文文件不可读：%s；停止（未创建任何 pane）。"), proj.contextFile)
	}
	// 锁序统一 anchor → team：新建组也必须在发布可发现状态前持有 team 锁。
	if err := a.lock("anchor-"+a.anchor.PaneID, "up"); err != nil {
		return err
	}
	if existing := a.st.findByAnchor(a.anchor.PaneID); existing != "" {
		if a.createOnly {
			return a.fail(L("该 pane 已有编组 %s，选择界面不恢复/不换模型。"), existing)
		}
		return a.upExisting(existing)
	}

	ownerEng, err := cfg.pickEngine("owner", cliOwner)
	if err != nil {
		return a.fail("%v", err)
	}
	workerEng, err := cfg.pickEngine("worker", cliWorker)
	if err != nil {
		return a.fail("%v", err)
	}
	if _, ok := parseEngine(ownerEng); !ok {
		return a.fail(L("无法识别 Owner 引擎：%s"), ownerEng)
	}
	if _, ok := parseEngine(workerEng); !ok {
		return a.fail(L("无法识别 Worker 引擎：%s"), workerEng)
	}
	nWorkers, err := parseCount(cliWorkers, 1, 1, maxPerRole, "--workers")
	if err != nil {
		return a.fail("%v", err)
	}
	// 数量解析先于 advisor 引擎：--advisors 0 与 --advisor none 都不解析 advisor 引擎。
	nAdvisors, err := parseCount(cliAdvisors, -1, 0, maxPerRole, "--advisors")
	if err != nil {
		return a.fail("%v", err)
	}
	if cliAdvisor == "none" && nAdvisors > 0 {
		return a.fail(L("--advisor none 与 --advisors %d 不能同时使用。"), nAdvisors)
	}
	advisorEng := ""
	if cliAdvisor != "none" && nAdvisors != 0 {
		if advisorEng, err = cfg.pickEngine("advisor", cliAdvisor); err != nil {
			return a.fail("%v", err)
		}
		if advisorEng == "none" {
			advisorEng = ""
		}
		if _, ok := parseEngine(advisorEng); advisorEng != "" && !ok {
			return a.fail(L("无法识别 Advisor 引擎：%s"), advisorEng)
		}
	}
	if nAdvisors < 0 {
		// 未指定时：advisor 引擎非 none 缺省 1 个，否则 0 个。
		nAdvisors = 0
		if advisorEng != "" {
			nAdvisors = 1
		}
	}
	if nAdvisors > 0 && advisorEng == "" {
		return a.fail(L("--advisors %d 需要 Advisor 引擎：请用 --advisor 指定，或在 config.json 配置 default_profile.advisor。"), nAdvisors)
	}

	// 名称先行：唯一生成点 + 任何 split 之前的 NAME 校验（失败零副作用）。
	prefix, err := genPrefix(proj.short)
	if err != nil {
		return a.fail(L("无法生成 run id。"))
	}
	if err := a.validateNames(prefix, nWorkers, nAdvisors); err != nil {
		return err
	}
	for _, c := range []struct{ label, eng string }{{"Owner", ownerEng}, {"Worker", workerEng}, {"Advisor", advisorEng}} {
		if c.eng != "" && !a.requireEngine(c.eng) {
			return a.fail(L("%s 引擎前置不满足，停止（未创建任何 pane）。"), c.label)
		}
	}
	if err := a.lock("team-"+prefix, "up"); err != nil {
		return err
	}

	protoSha, err := a.protocolSha()
	if err != nil {
		return a.fail(L("无法读取 protocol.md：%v"), err)
	}
	t := &Team{
		SchemaVersion: schemaVersion, TeamID: prefix, Prefix: prefix,
		Anchor: Anchor{PaneID: a.anchor.PaneID, WorkspaceID: a.anchor.WorkspaceID, TabID: a.anchor.TabID, CWD: a.anchor.CWD},
		CWD:    a.anchor.CWD, TeamRev: 1, ProtocolVersion: protoSha, Phase: "starting",
	}
	t.addRole(ownerKey, &Role{Name: roleName(prefix, "owner", 1), Engine: ownerEng, Instance: 1})
	for i := 1; i <= nWorkers; i++ {
		t.addRole(memberKey("worker", i), &Role{Name: roleName(prefix, "worker", i), Engine: workerEng, Instance: i})
	}
	for i := 1; i <= nAdvisors; i++ {
		t.addRole(memberKey("advisor", i), &Role{Name: roleName(prefix, "advisor", i), Engine: advisorEng, Instance: i})
	}
	if proj.contextFile != "" {
		ctxSha := ""
		if readableFile(proj.contextFile) {
			ctxSha, _ = sha256File(proj.contextFile)
		}
		t.Context = &Context{File: proj.contextFile, Trigger: proj.trigger, Sha256: ctxSha}
	}
	if err := a.save(t); err != nil {
		return err
	}
	if err := a.captureAnchorIdentity(t); err != nil {
		return err
	}
	if err := a.createTaskDir(t); err != nil {
		return err
	}

	adv := advisorEng
	if adv == "" {
		adv = L("无")
	}
	a.outf("== Team: owner=%s worker=%s advisor=%s ==\n", ownerEng, workerEng, adv)

	if _, err := a.runSplit(t, ownerKey, a.anchor.PaneID, "right"); err != nil {
		return a.fail(L("Owner pane 创建失败。核对后重跑 up。"))
	}
	for _, key := range t.membersOf("worker") {
		if err := a.splitMember(t, key); err != nil {
			return a.fail(L("Worker pane 创建失败。核对后重跑 up。"))
		}
	}
	for _, key := range t.membersOf("advisor") {
		if err := a.splitMember(t, key); err != nil {
			return a.fail(L("Advisor pane 创建失败。核对后重跑 up。"))
		}
	}
	t.Phase = "panes_ready"
	if err := a.save(t); err != nil {
		return err
	}

	if err := a.runLaunch(t, ownerKey); err != nil {
		return a.fail(L("Owner 启动失败。核对 get/read 后重跑 up 继续。"))
	}
	for _, key := range t.membersOf("advisor") {
		if err := a.runLaunch(t, key); err != nil {
			return a.fail(L("Advisor 启动失败。核对 get/read 后重跑 up 继续。"))
		}
	}
	for _, key := range t.membersOf("worker") {
		if err := a.runLaunch(t, key); err != nil {
			return a.fail(L("Worker 启动失败。核对 get/read 后重跑 up 继续。"))
		}
	}
	t.Phase = "launched"
	if err := a.save(t); err != nil {
		return err
	}

	a.outf("\nNEXT:\n")
	for _, key := range t.members() {
		a.outf("- %s %s pane %s：%s\n", kindOf(key), key, t.role(key).Pane, a.hint(t.role(key).Engine))
	}
	a.outf("%s", L("- cc/ccenv/pi 需等 Herdr 识别（herdr agent list）后运行 nakama init\n"))
	return a.autoInit(cfg, t)
}

// auto_init 打开时接着初始化。init 自己取 team 锁，而本插件的锁不可重入，所以先放掉建组 / 变更持有的锁。
func (a *app) autoInit(cfg *config, t *Team) error {
	if !cfg.autoInit() {
		return nil
	}
	a.unlockAll()
	return a.cmdInit([]string{"--team", t.TeamID})
}

// 已存在编组：在 team 锁内重读状态后再决策。
func (a *app) upExisting(id string) error {
	if err := a.lock("team-"+id, "up"); err != nil {
		return err
	}
	t, err := a.st.load(id)
	if err != nil {
		return a.fail("%v", err)
	}
	if err := a.checkAnchorServer(t); err != nil {
		return err
	}
	if !a.pendingGuard(t) {
		return a.fail(L("编组 %s 有未确认副作用（UNKNOWN），停止；请人工 get/read 核对，不重发。"), id)
	}
	if t.Phase == "starting" || t.Phase == "panes_ready" {
		// 恢复会产生新的 split/启动，先核对冻结的 protocol/context hash，漂移则零副作用拒绝。
		if err := a.checkVersions(t); err != nil {
			return err
		}
		return a.resumeHalf(t)
	}
	if t.Phase == "changing" {
		if err := a.checkVersions(t); err != nil {
			return err
		}
		return a.resumeChange(t)
	}
	a.outf(L("已存在本 anchor 的编组 %s（phase=%s）；不重复创建。\n"), id, t.Phase)
	a.printNext(t)
	return nil
}

// 已建且已启动的角色身份核验；任一失败返回 false。
func (a *app) verifyBuiltRoles(t *Team) bool {
	ok := true
	for _, rn := range t.members() {
		r := t.role(rn)
		if r.Pane == "" || !r.Launched {
			continue
		}
		eng, _ := parseEngine(r.Engine)
		var msg string
		if eng.needsRename() {
			msg = a.verifyFirstFreeze(t, rn)
		} else {
			_, msg = a.verifyIdentity(t, rn, r.Identity, false)
		}
		if msg != "" {
			a.errf("%s", msg)
			ok = false
		}
	}
	return ok
}

// 半组恢复（phase=starting）：已建角色身份核验通过 → 只补缺失步骤；否则拒绝。
// 仅确证 agent_not_found 才视 idle；occupied/unknown 在 run/start 前拒绝并保留原始分类。
func (a *app) resumeHalf(t *Team) error {
	if !a.verifyBuiltRoles(t) {
		return a.fail(L("半组状态不安全（已建角色身份核验失败）。人工核验步骤：对每个角色执行 herdr agent get/read，确认 pane/实例后重建或重新 up；不自动继续。"))
	}
	for _, key := range t.members() {
		if err := a.ensureRole(t, key); err != nil {
			return err
		}
	}
	t.Phase = "launched"
	if err := a.save(t); err != nil {
		return err
	}
	a.outf("%s", L("半组恢复完成（继续缺失步骤，不重复已建角色）。\n"))
	a.printNext(t)
	return nil
}

// 补齐单个角色的缺失步骤（split / 启动）；半组恢复与编组变更（add / swap）共用。
// splitMember 为 Worker / Advisor 成员创建 pane。布局：Worker 从锚点（或同类中键序在其之前、最后一个已有 pane 的成员）向下；
// Advisor 从 Owner（或同类前一个已有 pane 的成员）向下。Owner 固定从锚点右侧创建。
func (a *app) splitMember(t *Team, key string) error {
	kind := kindOf(key)
	base := a.anchor.PaneID
	if kind == "advisor" {
		if base = string(t.owner().Pane); base == "" {
			return a.fail(L("半组恢复失败：advisor 需要先有 owner pane。人工核验后重建。"))
		}
	}
	for _, k := range t.membersOf(kind) {
		if keyInstance(k) >= keyInstance(key) {
			break
		}
		if p := string(t.role(k).Pane); p != "" {
			base = p
		}
	}
	if !a.requireEngine(t.role(key).Engine) {
		return a.fail(L("半组恢复失败：%s 引擎前置不满足。"), key)
	}
	dir := "down"
	if kind == "owner" {
		dir = "right"
	}
	_, err := a.runSplit(t, key, base, dir)
	return err
}

// 补齐单个成员的缺失步骤（split / 启动）；半组恢复与编组变更（add / swap）共用。key 为成员键。
func (a *app) ensureRole(t *Team, key string) error {
	r := t.role(key)
	created := false
	if r.Pane == "" {
		if err := a.splitMember(t, key); err != nil {
			return a.fail(L("半组恢复：%s split 失败。核对后重跑 up。"), key)
		}
		created = true
	}
	if !r.Launched {
		if !created {
			if state, code := a.paneOccupancy(string(r.Pane)); state != "idle" {
				if code == "" {
					code = "n/a"
				}
				return a.fail(L("半组恢复：%s pane %s 占用状态=%s（原始分类 %s）；拒绝启动，请人工核对。"), key, r.Pane, state, code)
			}
		}
		if !a.requireEngine(r.Engine) {
			return a.fail(L("半组恢复失败：%s 引擎前置不满足。"), key)
		}
		if err := a.runLaunch(t, key); err != nil {
			return a.fail(L("半组恢复：%s 启动失败。核对 get/read 后重跑 up。"), key)
		}
	}
	return nil
}

// ---- 版本核对（init 前）：protocol 与 context 的 sha256 必须等于记录值；不自动改 hash ----
func (a *app) checkVersions(t *Team) error {
	cur, err := a.protocolSha()
	if err != nil {
		return a.fail(L("无法读取 protocol.md：%v"), err)
	}
	if cur != t.ProtocolVersion {
		return a.fail(L("protocol.md 已变化（记录 %s，当前 %s）；请重新 up 新组或人工确认，不自动改 hash。"), t.ProtocolVersion, cur)
	}
	if t.Context != nil && t.Context.File != "" {
		if !readableFile(t.Context.File) {
			return a.fail(L("声明的上下文文件不可读：%s；停止初始化。"), t.Context.File)
		}
		cur, _ = sha256File(t.Context.File)
		if cur != t.Context.Sha256 {
			return a.fail(L("上下文文件已变化（记录 %s，当前 %s）；请重新 up 新组或人工确认，不自动改 hash。"), t.Context.Sha256, cur)
		}
	}
	return nil
}

// ---- init ----
func (a *app) cmdInit(args []string) error {
	var cliTeam, cliPane, cliCwd string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		dst := map[string]*string{"--team": &cliTeam, "--pane": &cliPane, "--cwd": &cliCwd}[arg]
		switch {
		case arg == "-h" || arg == "--help":
			a.outf("%s", L("用法：nakama init --team <id>\n"))
			return nil
		case dst == nil:
			return a.fail(L("未知参数：%s"), arg)
		case i+1 >= len(args):
			return a.fail(L("参数 %s 缺少取值"), arg)
		default:
			i++
			*dst = args[i]
		}
	}
	id, err := a.resolveTeam(cliTeam, cliPane, cliCwd)
	if err != nil {
		return err
	}
	if err := a.lock("team-"+id, "init"); err != nil {
		return err
	}
	t, err := a.st.load(id)
	if err != nil {
		return a.fail("%v", err)
	}
	if !a.pendingGuard(t) {
		return a.fail(L("编组 %s 有未确认副作用（UNKNOWN），不重发；请人工 get/read 核对。"), id)
	}
	if t.Phase == "changing" {
		return a.fail(L("编组 %s 的变更尚未完成（phase=changing）；先重跑 add / swap 或 up 补齐，再 init。"), id)
	}
	if err := a.checkVersions(t); err != nil {
		return err
	}
	if err := a.checkAnchorServer(t); err != nil {
		if a.rebindAnchorBinding(t) == "candidate" {
			a.errf(L("锚点 terminal 已变化（同 workspace/tab/cwd，可能是 Herdr 重启后）；候选（不保证可重绑）：可能需要显式 rebind --team %s，请先 --preflight 核对。未写入状态。"), id)
		}
		return err
	}
	// 从角色 pane 自动选组时，先证明调用者仍是该组的对应角色实例；任何角色的 rename/prompt 之前只读拒绝。
	if cliTeam == "" && t.Anchor.PaneID != a.anchor.PaneID {
		if msg := a.verifyCallerRole(t, a.anchor.PaneID); msg != "" {
			return a.fail("%s", msg)
		}
	}
	a.herdr("agent", "list") // 触发 Herdr 刷新识别，结果不用

	overall := true
	for _, rn := range t.initOrder() {
		r := t.role(rn)
		if r.Inited {
			// 已 inited 角色跳过投递前先做只读身份复核；不一致标 stale，不重发。
			// stale 只在此置位、从不自动清除：身份碰巧再次匹配也不能证明中间没被替换过。
			if r.Stale {
				overall = false
				a.errf(L("%s 已标记 stale；不重发，请人工核对（重新 up 新组）。"), rn)
				continue
			}
			obj, msg := a.verifyIdentity(t, rn, r.Identity, false)
			if msg != "" {
				if filled, ok := fillSession(*r.Identity, obj.ident()); ok {
					r.Identity, msg = &filled, ""
				}
			}
			if msg != "" {
				// 重启后的候选只提示、不标 stale：stale 会阻断 rebind，且 stale 从不自动清除。
				if label, _ := a.rebindRoleBinding(r); label == "candidate" {
					overall = false
					a.errf(L("%s 的 pane 与 kind 仍匹配，但 terminal 或任务名已变化（可能是 Herdr 重启后）；候选（不保证可重绑）：可能需要显式 rebind --team %s，请先 --preflight 核对。未写入状态，不重发。"), rn, id)
					continue
				}
				overall = false
				r.Stale = true
				a.errf(L("%s 已初始化但身份复核不一致（stale）；不重发，请人工核对。"), rn)
			}
			if err := a.save(t); err != nil {
				return err
			}
			continue
		}
		// 首个未初始化角色失败即中止：不对其余角色继续投递（与旧 set -e 行为一致）。
		switch a.initRole(t, rn) {
		case 0:
		case 1:
			return a.fail(L("%s 初始化已投递未确认（UNKNOWN）。核对后处理，不重复初始化。"), rn)
		default:
			return errExit
		}
	}

	// 新角色就绪后才通知其余角色；未能投递的保留标记，下次 init 再投。
	if !a.deliverTeamUpdates(t) {
		overall = false
	}

	ready := overall
	for _, rn := range t.members() {
		if r := t.role(rn); !r.Inited || r.Stale {
			ready = false
		}
	}
	if ready {
		t.Phase = "initialized"
		if err := a.save(t); err != nil {
			return err
		}
		a.outf("%s", L("\nREADY: 编组初始化完成。点击 Owner pane 开始任务。\n"))
	} else {
		a.outf("%s", L("\nPARTIAL: 仍有角色未确认初始化或身份不一致；见上方提示或 nakama status。\n"))
	}
	if !overall {
		return errExit
	}
	return nil
}

// --team 优先；否则按锚点或角色 pane 定位，多个命中须用 --team 消歧，避免误选。
func (a *app) resolveTeam(cliTeam, cliPane, cliCwd string) (string, error) {
	id := cliTeam
	if id == "" {
		if err := a.normalizeAnchor(cliPane, cliCwd); err != nil {
			return "", err
		}
		ids := a.st.findByPane(a.anchor.PaneID)
		switch {
		case len(ids) == 0:
			return "", a.fail(L("未指定 --team，且当前 pane 不属于任何编组。"))
		case len(ids) > 1:
			return "", a.fail(L("当前 pane 同时属于多个编组（%s），请用 --team 指定。"), strings.Join(ids, ", "))
		}
		id = ids[0]
	}
	if !a.st.exists(id) {
		return "", a.fail(L("编组不存在：%s"), id)
	}
	return id, nil
}

// 角色 pane 自动选组的调用者核验（只读）：已冻结角色沿用 verifyIdentity（inited 且 session 为空时允许 fillSession 补全），
// 未冻结的手动角色沿用 verifyFirstFreeze，其余拒绝。返回非空表示拒绝。
func (a *app) verifyCallerRole(t *Team, pane string) string {
	rn := ""
	for _, n := range t.members() {
		if string(t.role(n).Pane) == pane {
			rn = n
			break
		}
	}
	if rn == "" {
		return fmt.Sprintf(L("当前 pane %s 不再是编组 %s 的角色 pane；拒绝自动定位，请用 --team 明确选择。"), pane, t.TeamID)
	}
	r := t.role(rn)
	eng, _ := parseEngine(r.Engine)
	var msg string
	switch {
	case r.Identity != nil:
		obj, m := a.verifyIdentity(t, rn, r.Identity, false)
		if m != "" && r.Inited {
			if _, ok := fillSession(*r.Identity, obj.ident()); ok {
				m = ""
			}
		}
		msg = m
	case eng.needsRename():
		msg = a.verifyFirstFreeze(t, rn)
	default:
		msg = fmt.Sprintf(L("角色 %s 缺少冻结身份（native）"), rn)
	}
	if msg != "" {
		return fmt.Sprintf(L("%s；调用者 pane 属于该组的角色 %s，拒绝自动定位，请人工核对或使用 --team 明确选择。"), msg, rn)
	}
	return ""
}

// init 单角色：pane 占用者复核 → rename（如需）→ 身份复核/落盘 → prompt。0=成功，1=UNKNOWN，2=失败。
func (a *app) initRole(t *Team, rn string) int {
	r := t.role(rn)
	eng, _ := parseEngine(r.Engine)
	// expected：所有后续核验共用的同一基准（已冻结 identity，或首次冻结时的启动观测）。
	var expected Ident
	firstFreeze := false
	switch {
	case r.Identity != nil:
		expected = *r.Identity
	case !eng.needsRename():
		a.errf(L("角色 %s 缺少冻结身份（native）；拒绝认领当前实例，请人工核对后处理。"), rn)
		return 2
	case r.LaunchIdentity == nil:
		a.errf(L("角色 %s 缺少启动观测（launch_identity），无法核对 rename 后指纹；拒绝 rename，停止。"), rn)
		return 2
	default:
		// 首次冻结：先证明实例属于本次启动，再 rename。
		if msg := a.verifyFirstFreeze(t, rn); msg != "" {
			a.errf("%s", msg)
			return 2
		}
		expected, firstFreeze = *r.LaunchIdentity, true
		if _, taken := a.agentGet(r.Name); !taken {
			if a.interrupted() {
				return 2
			}
			t.setPending(rn, "rename", "NOT_SENT")
			if a.save(t) != nil {
				return 2
			}
			if res := a.herdrPass("agent", "rename", string(r.Pane), r.Name); !res.ok() {
				t.Pending.Delivery = "UNKNOWN"
				_ = a.save(t)
				return 2
			}
			t.Pending = nil
			if a.save(t) != nil {
				return 2
			}
		}
	}

	// rename 之后（或名称原已绑定）再核一次完整指纹，通过才冻结并允许 prompt。
	obj, msg := a.verifyIdentity(t, rn, &expected, firstFreeze)
	if msg != "" {
		a.errf("%s", msg)
		return 2
	}
	frozen := obj.ident()
	r.Identity = &frozen
	if a.save(t) != nil {
		return 2
	}

	if a.startupDialog(string(r.Pane)) {
		a.errf(L("%s 的 pane %s 停在启动对话框（输入会被对话框吞掉）；先到该 pane 处理，再重跑 init。"), rn, string(r.Pane))
		return 2
	}
	a.outf("== Init %s (%s) ==\n", rn, r.Engine)
	prompt, err := a.assemblePrompt(t, rn)
	if err != nil {
		return 2
	}
	if a.interrupted() {
		return 2
	}
	t.setPending(rn, "prompt", "NOT_SENT")
	if a.save(t) != nil {
		return 2
	}
	if res := a.herdrPass("agent", "prompt", r.Name, prompt, "--wait", "--timeout", "60000"); !res.ok() {
		switch errorCode(res.stderr) {
		case "timeout", "agent_prompt_stalled":
			// 提交后超时：视为已投递未确认，不重发。
			a.warnf(L("%s 初始化已提交但未确认完成（%s）；不要重发，read 核对其回复。"), r.Name, errorCode(res.stderr))
			t.Pending.Delivery = "UNKNOWN"
			_ = a.save(t)
			return 1
		case "agent_blocked":
			// Herdr 在对话框前拒绝且未发送任何输入：确证未送达，清掉 pending，处理对话框后可重试。
			t.Pending = nil
			if a.save(t) != nil {
				return 2
			}
			a.errf(L("%s 在 pane %s 仍在等待授权或确认，输入未发出。处理后重按 prefix+i（或 init）。"), r.Name, string(r.Pane))
			return 2
		}
		return 2
	}
	r.Inited = true
	t.Pending = nil
	if a.save(t) != nil {
		return 2
	}
	a.herdr("agent", "read", r.Name, "--source", "recent-unwrapped", "--lines", "120")
	return 0
}

// ---- status（只读，不取锁；单个 agent get 失败按 unknown 继续）----
func (a *app) cmdStatus(args []string) error {
	var cliTeam string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-h" || args[i] == "--help":
			a.outf("%s", L("用法：nakama status [--team <id>]\n"))
			return nil
		case args[i] == "--team" && i+1 < len(args):
			i++
			cliTeam = args[i]
		default:
			return a.fail(L("未知参数：%s"), args[i])
		}
	}
	ids := a.st.list()
	if cliTeam != "" {
		ids = []string{cliTeam}
	}
	any := false
	for _, id := range ids {
		if !a.st.exists(id) {
			a.errf(L("编组不存在：%s"), id)
			continue
		}
		t, err := a.st.load(id)
		if err != nil {
			return a.fail("%v", err)
		}
		any = true
		a.statusTeam(t)
	}
	if !any {
		a.outf("%s", L("（暂无编组）\n"))
	}
	return nil
}

func (a *app) statusTeam(t *Team) {
	a.outf(L("== 编组 %s（phase=%s）==\n"), t.TeamID, t.Phase)
	a.outf("anchor=%s cwd=%s binding=%s\n", t.Anchor.PaneID, t.Anchor.CWD, a.rebindAnchorBinding(t))
	a.outf("protocol_sha256=%s\n", t.ProtocolVersion)
	// status 只读：生命周期 status 取自记录 pane 的观测；binding 说明记录身份是否仍有效（bound/candidate/unverified）。
	candidate := a.rebindAnchorBinding(t) == "candidate"
	for _, rn := range t.members() {
		r := t.role(rn)
		pane := string(r.Pane)
		if pane == "" {
			pane = "?"
		}
		st := "unknown"
		label, o := a.rebindRoleBinding(r)
		if o.AgentStatus != "" {
			st = o.AgentStatus
		}
		candidate = candidate || label == "candidate"
		upd := ""
		if r.TeamUpdate {
			upd = " team_update=pending"
		}
		a.outf("  %-7s %-3s name=%s pane=%s engine=%s inited=%t stale=%t status=%s binding=%s%s\n", kindOf(rn), rn, r.Name, pane, r.Engine, r.Inited, r.Stale, st, label, upd)
	}
	for _, x := range t.Retired {
		a.outf("  retired %s%d name=%s pane=%s engine=%s op=%s\n", strings.ToUpper(x.Role[:1]), x.Instance, x.Name, x.Pane, x.Engine, x.Op)
	}
	if t.TaskDir != "" {
		tasks := a.listTasks(t)
		var files int
		var size int64
		for _, k := range tasks {
			files, size = files+k.files, size+k.size
		}
		a.outf("  task_dir=%s tasks=%d files=%d size=%s\n", t.TaskDir, len(tasks), files, humanSize(size))
		a.statusTasks(t)
	}
	if candidate {
		a.outf(L("  提示：存在 candidate（同 pane/kind 的候选，不保证可重绑）：可能需要显式 rebind --team %s，请先 --preflight 核对；status 不改动状态。\n"), t.TeamID)
	}
	if p := t.Pending; p != nil {
		a.outf("  pending: step=%s delivery=%s\n", p.Step, p.Delivery)
		if p.Delivery == "UNKNOWN" || p.Delivery == "NOT_SENT" {
			role := p.Role
			if role == "" {
				role = "?"
			}
			a.warnf(L("存在未确认副作用（role=%s step=%s delivery=%s）：先 herdr agent get/read 人工核对，不重发。"), role, p.Step, p.Delivery)
			if resolvable[p.Step] {
				a.outf(L("  人工核对后用 nakama resolve --team %s --delivered 或 --not-delivered 处理。\n"), t.TeamID)
			}
		}
	}
}
