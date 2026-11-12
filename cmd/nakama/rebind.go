package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// rebind：Herdr 或电脑重启后，由用户显式触发的重绑。
// 只把记录中的 terminal（以及需要时的任务名）更新为预检观测到的当前值，不改变 pane、kind 与记录的 session。
// 同 pane + 同 kind + 非空 session 只说明“用户显式接纳同一对话在新 terminal 恢复”，不是同进程或凭据证明。
// 不在 init/up 中自动发生；预检不满足时零 rename、零状态写入。

const rebindUsage = "用法：nakama rebind --team <id> [--preflight | --discard-pending] " +
	"[--confirm-session <成员键 O1|W1|A1|anchor>=<session>]... [--confirm-anchor <terminal>]\n"

type rebindOpts struct {
	team          string
	confirm       map[string]string // 成员键或 anchor -> session（仅记录 session 为空时需要）；别名在加载 Team 后规范化
	confirmAnchor string            // 无 Identity 的 shell 锚点：本次观测到的新 terminal
	preflight     bool
	discard       bool
}

// rebindKeyKnown 只做语法判断：成员键、anchor，或旧别名 owner|worker|advisor（别名须在加载 Team 后按数量解析）。
func rebindKeyKnown(k string) bool {
	switch k {
	case "owner", "worker", "advisor", "anchor":
		return true
	}
	return memberKeyRe.MatchString(k)
}

// normalizeConfirm 在加载 Team 之后、checkPlanConfirm 之前执行：旧别名仅在该类恰好一个成员时解析为其键，
// 否则要求用成员键；同一成员的别名与键同时给出且值冲突即拒，不依赖遍历顺序覆盖。
func normalizeConfirm(t *Team, o *rebindOpts) error {
	out := map[string]string{}
	for k, v := range o.confirm {
		key := k
		if k != "anchor" && !memberKeyRe.MatchString(k) {
			ms := t.membersOf(k)
			if len(ms) > 1 {
				return fmt.Errorf(L("--confirm-session %s 不唯一（编组中有 %d 个 %s），请用成员键（如 W2）"), k, len(ms), k)
			}
			if len(ms) == 1 {
				key = ms[0]
			}
		}
		if prev, dup := out[key]; dup && prev != v {
			return fmt.Errorf(L("--confirm-session %s 的别名与成员键取值冲突，拒绝"), key)
		}
		out[key] = v
	}
	o.confirm = out
	return nil
}

func parseRebindArgs(args []string) (rebindOpts, bool, error) {
	o := rebindOpts{confirm: map[string]string{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-h", "--help":
			return o, true, nil
		case "--preflight":
			o.preflight = true
			continue
		case "--discard-pending":
			o.discard = true
			continue
		}
		if i+1 >= len(args) {
			return o, false, fmt.Errorf(L("参数 %s 缺少取值"), arg)
		}
		i++
		v := args[i]
		switch arg {
		case "--team":
			if o.team != "" {
				return o, false, fmt.Errorf("%s", L("--team 重复指定"))
			}
			o.team = v
		case "--confirm-anchor":
			if o.confirmAnchor != "" {
				return o, false, fmt.Errorf("%s", L("--confirm-anchor 重复指定"))
			}
			o.confirmAnchor = v
		case "--confirm-session":
			key, sess, ok := strings.Cut(v, "=")
			if !ok || sess == "" {
				return o, false, fmt.Errorf("%s", L("--confirm-session 格式须为 <role>=<session>"))
			}
			if !rebindKeyKnown(key) {
				return o, false, fmt.Errorf(L("--confirm-session 指定了未知角色：%s"), key)
			}
			if _, dup := o.confirm[key]; dup {
				return o, false, fmt.Errorf(L("角色 %s 的 --confirm-session 重复"), key)
			}
			o.confirm[key] = sess
		default:
			return o, false, fmt.Errorf(L("未知参数：%s"), arg)
		}
	}
	if o.team == "" {
		return o, false, fmt.Errorf("%s", L("rebind 必须指定 --team <id>"))
	}
	if o.preflight && o.discard {
		return o, false, fmt.Errorf("%s", L("--preflight 与 --discard-pending 不能同时使用"))
	}
	return o, false, nil
}

func (a *app) cmdRebind(args []string) error {
	o, help, err := parseRebindArgs(args)
	if help {
		a.outf("%s", L(rebindUsage))
		return nil
	}
	if err != nil {
		return a.fail("%v", err)
	}
	if !a.st.exists(o.team) {
		return a.fail(L("编组不存在：%s"), o.team)
	}
	pre, err := a.st.load(o.team)
	if err != nil {
		return a.fail("%v", err)
	}
	// 锁序 anchor → team。预检只读，不取锁。
	if !o.preflight {
		if err := a.lock("anchor-"+pre.Anchor.PaneID, "rebind"); err != nil {
			return err
		}
		if err := a.lock("team-"+o.team, "rebind"); err != nil {
			return err
		}
	}
	t, err := a.st.load(o.team)
	if err != nil {
		return a.fail("%v", err)
	}
	if err := normalizeConfirm(t, &o); err != nil {
		return a.fail("%v", err)
	}
	if o.discard {
		return a.discardRebind(t)
	}
	if t.RebindPending != nil {
		if err := validatePlanAgainstTeam(t, t.RebindPending); err != nil {
			return a.fail(L("编组 %s 的重绑计划与编组记录不一致（%v）：视为损坏，拒绝对账与提交，不补默认值。可人工核对后用 --discard-pending 放弃。"), t.TeamID, err)
		}
		if msg := checkPlanConfirm(t.RebindPending, o); msg != "" {
			return a.fail(L("编组 %s 存在未完成的重绑计划 %s：%s"), t.TeamID, t.RebindPending.OpID, msg)
		}
		if o.preflight {
			a.outf(L("编组 %s 存在未完成的重绑计划 %s（尚未提交）；只读预检不执行对账，直接执行 rebind --team %s 即可。\n"), t.TeamID, t.RebindPending.OpID, t.TeamID)
			return nil
		}
		return a.resumeRebind(t)
	}
	return a.planRebind(t, o)
}

// checkPlanConfirm：对账已有计划时，确认参数只能回显计划中已确认的值；不同即拒绝，不会把新值套用到旧计划上。
func checkPlanConfirm(p *RebindPlan, o rebindOpts) string {
	for key, sess := range o.confirm {
		if key == "anchor" {
			if p.AnchorIdentityNew == nil || p.AnchorIdentityNew.Session != sess {
				return L("--confirm-session anchor 与计划不一致，拒绝对账（可 --discard-pending 后重新 rebind）。")
			}
			continue
		}
		matched := false
		for _, s := range p.Steps {
			if s.Role == key && s.New.Session == sess {
				matched = true
			}
		}
		if !matched {
			return fmt.Sprintf(L("--confirm-session %s 与计划不一致，拒绝对账（可 --discard-pending 后重新 rebind）。"), key)
		}
	}
	if o.confirmAnchor != "" && o.confirmAnchor != p.AnchorNew {
		return L("--confirm-anchor 与计划不一致，拒绝对账（可 --discard-pending 后重新 rebind）。")
	}
	return ""
}

// 适用范围：只支持 initialized 的稳定组。旧 UNKNOWN、stale、半初始化一律拒绝，不清空、不补发 init/prompt。
// 本函数不看 rebind_pending：计划执行前与恢复时都用它重新确认组仍是稳定组。
func rebindApplicable(t *Team) string {
	if t.Phase != "initialized" {
		return fmt.Sprintf(L("编组 phase=%s：rebind 只支持 initialized 的稳定组。"), t.Phase)
	}
	if t.Pending != nil {
		return L("存在未确认的副作用（pending），不做重绑；请先 herdr agent get/read 人工核对。")
	}
	if t.Anchor.PaneID == "" {
		return L("编组缺少锚点 pane 记录。")
	}
	for _, rn := range t.members() {
		r := t.role(rn)
		if !r.Inited || r.Identity == nil {
			return fmt.Sprintf(L("角色 %s 未完成初始化或缺少冻结身份；rebind 不清空、不补发 init/prompt。"), rn)
		}
		if r.Stale {
			return fmt.Sprintf(L("角色 %s 已标记 stale；rebind 不清除 stale，需人工处理。"), rn)
		}
	}
	return ""
}

// rebindPaneCtx 只读核对 pane：pane get 必须完整，且 workspace/tab/cwd 与编组记录一致。
// 返回 pane get 的结果（含 terminal）；缺少任何一项记录即视为证据不足。
func (a *app) rebindPaneCtx(t *Team, pane string) (paneObj, string) {
	r := a.herdr("pane", "get", pane)
	if !r.ok() {
		return paneObj{}, fmt.Sprintf(L("无法读取 pane %s"), pane)
	}
	p := parsePane(r.stdout)
	if p.PaneID != pane || p.TerminalID == "" {
		return p, fmt.Sprintf(L("pane %s 响应不完整"), pane)
	}
	if t.Anchor.WorkspaceID == "" || t.Anchor.TabID == "" || t.Anchor.CWD == "" {
		return p, L("编组缺少 workspace/tab/cwd 记录，无法证明上下文，拒绝")
	}
	if p.WorkspaceID != t.Anchor.WorkspaceID || p.TabID != t.Anchor.TabID || realPath(p.CWD) != t.Anchor.CWD {
		return p, fmt.Sprintf(L("pane %s 的 workspace/tab/cwd 与编组记录不一致"), pane)
	}
	return p, ""
}

// nameOwner 只读查询任务名归属：free（确证不存在）/ taken（返回占用 pane）/ unknown。
func (a *app) nameOwner(name string) (string, string) {
	r := a.herdrCombined("agent", "get", name)
	if !r.ok() {
		if errorCode(r.stdout) == "agent_not_found" {
			return "", "free"
		}
		return "", "unknown"
	}
	o, ok := parseAgent(r.stdout)
	if !ok {
		return "", "unknown"
	}
	return o.PaneID, "taken"
}

type rebindResult struct {
	steps          []RebindStep
	anchorOld      string
	anchorNew      string
	anchorIdent    *Ident // 观测到的新锚点 identity（仅锚点有 identity 时）
	anchorIdentOld *Ident
	anchorChanged  bool
	confirmed      []string
	problems       []string
	needNotes      []string
	needArgs       []string
	used           map[string]bool
	anchorUsed     bool
}

func (r rebindResult) changed() bool {
	if r.anchorChanged {
		return true
	}
	for _, s := range r.steps {
		if s.Rename || s.New != s.Old {
			return true
		}
	}
	return false
}

// plan 固定完整快照：锚点无论是否变化都保存旧/新 terminal 与（有 identity 时的）完整身份，恢复时据此复核。
func (r rebindResult) plan(op string) *RebindPlan {
	return &RebindPlan{
		OpID: op, At: time.Now().UTC().Format(time.RFC3339), Steps: r.steps,
		AnchorOld: r.anchorOld, AnchorNew: r.anchorNew,
		AnchorIdentityOld: r.anchorIdentOld, AnchorIdentityNew: r.anchorIdent, Confirm: r.confirmed,
	}
}

// checkRebindSession：记录 session 非空时必须与观测完全相等（确认值只能回显）；
// 记录为空时必须有与本次观测相等的 --confirm-session，否则只提示候选与精确参数。
func checkRebindSession(key, recorded, observed string, confirm map[string]string, res *rebindResult) {
	c, given := confirm[key]
	if given {
		res.used[key] = true
	}
	switch {
	case observed == "":
		res.problems = append(res.problems, fmt.Sprintf(L("%s：观测 session 为空或缺字段，拒绝。"), key))
	case recorded != "":
		// 记录非空时确认参数不能改变结论；与观测一致的确认只是回显（例如提交后的重跑），不同则拒绝。
		if given && c != observed {
			res.problems = append(res.problems, fmt.Sprintf(L("%s：--confirm-session 与记录/观测的 session 不一致，拒绝。"), key))
		}
		if observed != recorded {
			res.problems = append(res.problems, fmt.Sprintf(L("%s：session 不一致（记录与观测不同），拒绝。"), key))
		}
	case !given:
		res.needNotes = append(res.needNotes, fmt.Sprintf(L("%s 的记录 session 为空，观测到 session=%s"), key, observed))
		res.needArgs = append(res.needArgs, fmt.Sprintf("--confirm-session %s=%s", key, observed))
	case c != observed:
		res.problems = append(res.problems, fmt.Sprintf(L("%s：--confirm-session 的值与本次观测的 session 不一致，拒绝。"), key))
	default:
		res.confirmed = append(res.confirmed, key+":session")
	}
}

func (a *app) observeRebindRole(t *Team, rn string, confirm map[string]string, res *rebindResult) {
	r := t.role(rn)
	old := *r.Identity
	pane := string(r.Pane)
	p, msg := a.rebindPaneCtx(t, pane)
	if msg != "" {
		res.problems = append(res.problems, fmt.Sprintf(L("角色 %s：%s"), rn, msg))
		return
	}
	obj, ok := a.observeAgent(pane)
	if !ok {
		res.problems = append(res.problems, fmt.Sprintf(L("角色 %s：agent get 失败，无法观测。"), rn))
		return
	}
	nid := obj.ident()
	switch {
	case !nid.complete() || nid.PaneID != pane:
		res.problems = append(res.problems, fmt.Sprintf(L("角色 %s：身份字段缺失或 pane 不一致。"), rn))
		return
	case nid.TerminalID != p.TerminalID:
		res.problems = append(res.problems, fmt.Sprintf(L("角色 %s：pane get 与 agent get 的 terminal 不一致（观测不稳定），拒绝。"), rn))
		return
	case nid.Kind != old.Kind:
		res.problems = append(res.problems, fmt.Sprintf(L("角色 %s：kind 变化（记录 %s，观测 %s），拒绝。"), rn, old.Kind, nid.Kind))
		return
	}
	checkRebindSession(rn, old.Session, nid.Session, confirm, res)
	cur := obj.Name
	step := RebindStep{Role: rn, Pane: pane, Name: r.Name, Current: cur, Old: old, New: nid}
	switch {
	case cur == r.Name:
	case cur == "" || cur == bridgeAutoName(t.CWD, nid.TerminalID):
		step.Rename = true
		owner, st := a.nameOwner(r.Name)
		switch {
		case st == "unknown":
			res.problems = append(res.problems, fmt.Sprintf(L("角色 %s：无法确认任务名 %s 是否被占用，拒绝。"), rn, r.Name))
		case st == "taken" && owner != pane:
			res.problems = append(res.problems, fmt.Sprintf(L("角色 %s：目标任务名 %s 已被其它 pane 占用，拒绝（零 rename）。"), rn, r.Name))
		}
	default:
		res.problems = append(res.problems, fmt.Sprintf(L("角色 %s：当前名称 %q 既不是空也不是 Bridge 自动名（可能是他人占用），拒绝。"), rn, cur))
	}
	res.steps = append(res.steps, step)
}

func (a *app) observeRebindAnchor(t *Team, o rebindOpts, res *rebindResult) {
	an := t.Anchor
	p, msg := a.rebindPaneCtx(t, an.PaneID)
	if msg != "" {
		res.problems = append(res.problems, L("锚点：")+msg)
		return
	}
	if an.Identity != nil {
		obj, ok := a.observeAgent(an.PaneID)
		if !ok {
			res.problems = append(res.problems, L("锚点：agent get 失败，无法观测。"))
			return
		}
		nid := obj.ident()
		switch {
		case !nid.complete() || nid.PaneID != an.PaneID:
			res.problems = append(res.problems, L("锚点：身份字段缺失或 pane 不一致。"))
			return
		case nid.TerminalID != p.TerminalID:
			res.problems = append(res.problems, L("锚点：pane get 与 agent get 的 terminal 不一致（观测不稳定），拒绝。"))
			return
		case nid.Kind != an.Identity.Kind:
			res.problems = append(res.problems, L("锚点：kind 变化，拒绝。"))
			return
		}
		checkRebindSession("anchor", an.Identity.Session, nid.Session, o.confirm, res)
		ident, old := nid, *an.Identity
		res.anchorChanged = nid != old
		res.anchorOld, res.anchorNew, res.anchorIdent, res.anchorIdentOld = old.TerminalID, nid.TerminalID, &ident, &old
		return
	}
	// shell 锚点没有 identity，只能靠 terminal 连续性；记录缺失即证据不足，拒绝。
	if an.TerminalID == "" {
		res.problems = append(res.problems, L("锚点：记录缺少 terminal，无法证明连续性，拒绝（宁可新建组）。"))
		return
	}
	res.anchorOld, res.anchorNew = an.TerminalID, p.TerminalID
	if p.TerminalID == an.TerminalID {
		// 未变化时确认参数只能是当前值的回显；不同即为错误确认。
		if o.confirmAnchor != "" {
			res.anchorUsed = true
			if o.confirmAnchor != p.TerminalID {
				res.problems = append(res.problems, L("锚点：--confirm-anchor 与当前观测的 terminal 不一致，拒绝。"))
			}
		}
		return
	}
	res.anchorChanged = true
	if o.confirmAnchor == "" {
		res.needNotes = append(res.needNotes, fmt.Sprintf(L("锚点 terminal 已变化（记录 %s，观测 %s）"), an.TerminalID, p.TerminalID))
		res.needArgs = append(res.needArgs, "--confirm-anchor "+p.TerminalID)
		return
	}
	res.anchorUsed = true
	if o.confirmAnchor != p.TerminalID {
		res.problems = append(res.problems, L("锚点：--confirm-anchor 与本次观测的新 terminal 不一致，拒绝。"))
		return
	}
	res.confirmed = append(res.confirmed, "anchor:terminal")
}

// observeRebind 只读：逐角色、锚点观测，汇总问题、需要的确认与待改名步骤。
func (a *app) observeRebind(t *Team, o rebindOpts) rebindResult {
	res := rebindResult{used: map[string]bool{}}
	for _, rn := range t.members() {
		a.observeRebindRole(t, rn, o.confirm, &res)
	}
	a.observeRebindAnchor(t, o, &res)
	var unused []string
	for k := range o.confirm {
		if !res.used[k] {
			unused = append(unused, k)
		}
	}
	sort.Strings(unused)
	for _, k := range unused {
		res.problems = append(res.problems, fmt.Sprintf(L("--confirm-session %s 未被使用（角色不存在，或该记录不需要确认）。"), k))
	}
	if o.confirmAnchor != "" && !res.anchorUsed {
		res.problems = append(res.problems, L("--confirm-anchor 未被使用（锚点不需要 terminal 确认）。"))
	}
	return res
}

func (a *app) outRebindDiff(res rebindResult) {
	for _, s := range res.steps {
		extra := ""
		if s.Rename {
			extra = fmt.Sprintf(L("  任务名 %q → %s（改名）"), s.Current, s.Name)
		}
		sess := s.New.Session
		if sess == "" {
			sess = L("(空)")
		}
		a.outf("  %-7s pane=%s terminal %s → %s session=%s%s\n", s.Role, s.Pane, s.Old.TerminalID, s.New.TerminalID, sess, extra)
	}
	if res.anchorChanged {
		a.outf("  anchor  terminal %s → %s\n", res.anchorOld, res.anchorNew)
	}
}

// planRebind：预检 → 候选/确认提示 → 落盘完整计划 → 执行。预检失败与待确认时零 rename、零状态写入。
func (a *app) planRebind(t *Team, o rebindOpts) error {
	if msg := rebindApplicable(t); msg != "" {
		return a.fail("%s", msg)
	}
	res := a.observeRebind(t, o)
	a.outf(L("== rebind 预检：编组 %s ==\n"), t.TeamID)
	a.outRebindDiff(res)
	if len(res.problems) > 0 {
		for _, p := range res.problems {
			a.errf("%s", p)
		}
		return a.fail(L("拒绝重绑：预检不满足，零 rename、零状态写入。"))
	}
	if len(res.needArgs) > 0 {
		a.outf("%s", L("需要显式确认（零 rename、零状态写入）：\n"))
		for _, n := range res.needNotes {
			a.outf("  - %s\n", n)
		}
		a.outf(L("确认后重新执行：\n  nakama rebind --team %s %s\n"), t.TeamID, strings.Join(res.needArgs, " "))
		return errExit
	}
	if o.preflight {
		a.outf("%s", L("预检通过（只读，未写入）。\n"))
		return nil
	}
	if !res.changed() {
		a.outf("%s", L("无需重绑：记录身份与名称已与 Herdr 当前观测一致（未写入）。\n"))
		return nil
	}
	plan := res.plan(fmt.Sprint(time.Now().UnixNano()))
	t.RebindPending = plan
	if err := a.save(t); err != nil {
		return err
	}
	a.outf(L("重绑计划 %s 已落盘，开始执行。\n"), plan.OpID)
	return a.executeRebind(t)
}

// resumeRebind：存在未完成计划时先整组复核，再继续。已完成的改名不重发；任何漂移即停止。
func (a *app) resumeRebind(t *Team) error {
	a.outf(L("存在未完成的重绑计划 %s：先整组复核，已完成的改名不重发。\n"), t.RebindPending.OpID)
	return a.executeRebind(t)
}

// validatePlanAgainstTeam：持久化计划与编组记录的跨字段一致性。不通过即视为损坏，不补默认值，不执行、不提交。
// 规则：每个存在的角色恰好一步，无额外或重复角色；name/pane/old identity 与记录一致；session 只能是记录为空时的
// 补全且须有对应确认；kind 与 pane 不变；锚点旧/新快照与记录一致；确认项必须对应真实的变化。
func validatePlanAgainstTeam(t *Team, p *RebindPlan) error {
	if p == nil {
		return fmt.Errorf("%s", L("计划为空"))
	}
	if p.OpID == "" || p.AnchorOld == "" || p.AnchorNew == "" {
		return fmt.Errorf("%s", L("计划缺少操作 id 或锚点快照"))
	}
	confirmed := func(k string) bool {
		for _, c := range p.Confirm {
			if c == k {
				return true
			}
		}
		return false
	}
	justified := map[string]bool{}
	seen := map[string]bool{}
	for _, s := range p.Steps {
		if seen[s.Role] {
			return fmt.Errorf(L("角色 %s 重复出现"), s.Role)
		}
		seen[s.Role] = true
		r := t.role(s.Role)
		if r == nil || r.Identity == nil {
			return fmt.Errorf(L("计划包含编组中不存在或未冻结的角色 %s"), s.Role)
		}
		if s.Name != r.Name || s.Pane != string(r.Pane) {
			return fmt.Errorf(L("角色 %s 的任务名或 pane 与记录不一致"), s.Role)
		}
		if s.Old != *r.Identity {
			return fmt.Errorf(L("角色 %s 的旧身份与记录不一致"), s.Role)
		}
		if s.New.PaneID != s.Pane || s.New.Kind != s.Old.Kind {
			return fmt.Errorf(L("角色 %s 的新身份改变了 pane 或 kind"), s.Role)
		}
		if s.New.Session == "" {
			return fmt.Errorf(L("角色 %s 的新 session 为空"), s.Role)
		}
		if s.Old.Session != "" && s.New.Session != s.Old.Session {
			return fmt.Errorf(L("角色 %s 的已记录 session 被改变"), s.Role)
		}
		if s.Old.Session == "" && s.New.Session != "" {
			if !confirmed(s.Role + ":session") {
				return fmt.Errorf(L("角色 %s 的 session 补全缺少确认"), s.Role)
			}
			justified[s.Role+":session"] = true
		}
	}
	for _, rn := range t.members() {
		if !seen[rn] {
			return fmt.Errorf(L("计划缺少角色 %s"), rn)
		}
	}
	an := t.Anchor
	if an.Identity != nil {
		if p.AnchorIdentityOld == nil || p.AnchorIdentityNew == nil {
			return fmt.Errorf("%s", L("锚点有 identity 但计划缺少锚点身份快照"))
		}
		old, nw := *p.AnchorIdentityOld, *p.AnchorIdentityNew
		if old != *an.Identity || p.AnchorOld != an.Identity.TerminalID {
			return fmt.Errorf("%s", L("锚点旧身份与记录不一致"))
		}
		if nw.PaneID != an.PaneID || nw.Kind != an.Identity.Kind || p.AnchorNew != nw.TerminalID {
			return fmt.Errorf("%s", L("锚点新身份与计划快照不一致"))
		}
		if nw.Session == "" {
			return fmt.Errorf("%s", L("锚点新 session 为空"))
		}
		if an.Identity.Session != "" && nw.Session != an.Identity.Session {
			return fmt.Errorf("%s", L("锚点已记录 session 被改变"))
		}
		if an.Identity.Session == "" && nw.Session != "" {
			if !confirmed("anchor:session") {
				return fmt.Errorf("%s", L("锚点 session 补全缺少确认"))
			}
			justified["anchor:session"] = true
		}
	} else {
		if p.AnchorIdentityOld != nil || p.AnchorIdentityNew != nil {
			return fmt.Errorf("%s", L("锚点无 identity 但计划带有锚点身份快照"))
		}
		if p.AnchorOld != an.TerminalID {
			return fmt.Errorf("%s", L("锚点旧 terminal 与记录不一致"))
		}
		if p.AnchorNew != p.AnchorOld {
			if !confirmed("anchor:terminal") {
				return fmt.Errorf("%s", L("锚点 terminal 变化缺少确认"))
			}
			justified["anchor:terminal"] = true
		}
	}
	for _, c := range p.Confirm {
		if !justified[c] {
			return fmt.Errorf(L("确认项 %s 不对应计划中的真实变化"), c)
		}
	}
	return nil
}

// revalidatePlan：计划执行前与提交前的整组复核。每一步都检查（包括已 done 的步骤），锚点无论变化与否都检查。
// 当前观测必须等于计划固定的 new identity；仍为 old 时拒绝（不放行、不吸收、不把已改名的部分结果当成功）。
// final=true 为提交前复核：所有步骤的名称必须已等于计划名。
func (a *app) revalidatePlan(t *Team, final bool) error {
	p := t.RebindPending
	if msg := rebindApplicable(t); msg != "" {
		return a.fail(L("%s 计划保留，零新增 rename。"), msg)
	}
	if err := validatePlanAgainstTeam(t, p); err != nil {
		return a.fail(L("重绑计划与编组记录不一致（%v）：视为损坏，拒绝执行，不补默认值。"), err)
	}
	for i := range p.Steps {
		s := p.Steps[i]
		if _, msg := a.rebindPaneCtx(t, s.Pane); msg != "" {
			return a.fail(L("角色 %s 上下文漂移（%s）：计划保留，零新增 rename。"), s.Role, msg)
		}
		obj, ok := a.observeAgent(s.Pane)
		if !ok {
			return a.fail(L("角色 %s 无法观测：计划保留，零新增 rename。"), s.Role)
		}
		// 任何执行或恢复开始前，当前身份都必须等于计划的新身份；仍为旧身份不放行（不吸收、不视为成功）。
		if cur := obj.ident(); cur != s.New {
			if cur == s.Old {
				return a.fail(L("角色 %s 仍为旧身份（与计划新身份不同）：需重新预检或等待；计划保留，零新增 rename。"), s.Role)
			}
			return a.fail(L("角色 %s 身份与计划不符（漂移）：计划保留，零新增 rename。"), s.Role)
		}
		changeable := obj.Name == "" || obj.Name == bridgeAutoName(t.CWD, s.New.TerminalID)
		switch {
		case obj.Name == s.Name:
		case s.Result == "done" || final || !s.Rename || !changeable:
			return a.fail(L("角色 %s 任务名 %q 与计划不符（漂移）：计划保留，零新增 rename。"), s.Role, obj.Name)
		}
	}
	anchorPane, msg := a.rebindPaneCtx(t, t.Anchor.PaneID)
	if msg != "" {
		return a.fail(L("锚点上下文漂移（%s）：计划保留，零新增 rename。"), msg)
	}
	if t.Anchor.Identity == nil {
		if anchorPane.TerminalID != p.AnchorNew {
			return a.fail(L("锚点 terminal 与计划不符（漂移）：计划保留，不提交。"))
		}
	} else {
		obj, ok := a.observeAgent(t.Anchor.PaneID)
		if !ok || obj.ident() != *p.AnchorIdentityNew {
			return a.fail(L("锚点身份与计划不符（漂移）：计划保留，不提交。"))
		}
	}
	return nil
}

func (a *app) executeRebind(t *Team) error {
	if err := a.revalidatePlan(t, false); err != nil {
		return err
	}
	plan := t.RebindPending
	for i := range plan.Steps {
		s := &plan.Steps[i]
		if s.Result == "done" {
			continue
		}
		if err := a.rebindStep(t, s); err != nil {
			return err
		}
	}
	if a.hookBeforeCommit != nil {
		a.hookBeforeCommit()
	}
	if a.interrupted() {
		return errExit
	}
	if err := a.revalidatePlan(t, true); err != nil {
		return err
	}
	return a.commitRebind(t)
}

// rebindStep：改名前重读、改名、按 pane 与任务名两向复核；每一步结果落盘。
func (a *app) rebindStep(t *Team, s *RebindStep) error {
	obj, ok := a.observeAgent(s.Pane)
	if !ok || obj.ident() != s.New {
		return a.fail(L("角色 %s 执行前身份漂移（计划与观测不同）；停止，计划保留，不重发。可人工核对后用 --discard-pending 放弃计划。"), s.Role)
	}
	if obj.Name == s.Name {
		// 名称已是目标：同一身份同名即视为已完成，不重发 rename。
		s.Result = "done"
		return a.save(t)
	}
	if !s.Rename {
		return a.fail(L("角色 %s 名称在计划外变为 %q；停止，计划保留。"), s.Role, obj.Name)
	}
	if obj.Name != "" && obj.Name != bridgeAutoName(t.CWD, s.New.TerminalID) {
		return a.fail(L("角色 %s 当前名称 %q 不是可改回的名称；停止，计划保留。"), s.Role, obj.Name)
	}
	if owner, st := a.nameOwner(s.Name); st == "unknown" || (st == "taken" && owner != s.Pane) {
		return a.fail(L("角色 %s 的目标任务名 %s 当前不可用（%s）；零 rename，计划保留。"), s.Role, s.Name, st)
	}
	if a.hookBeforeRename != nil {
		a.hookBeforeRename(s.Role)
	}
	if a.interrupted() {
		return errExit
	}
	s.Result = "started"
	if err := a.save(t); err != nil {
		return err
	}
	if res := a.herdrPass("agent", "rename", s.Pane, s.Name); !res.ok() {
		return a.fail(L("角色 %s rename 失败；计划保留（started），重新执行 rebind 对账。"), s.Role)
	}
	if a.hookAfterRename != nil {
		a.hookAfterRename(s.Role)
	}
	// rename 后两向复核：pane 上的身份与名称，以及任务名反查到同一 pane。
	o2, ok := a.observeAgent(s.Pane)
	if !ok || o2.ident() != s.New || o2.Name != s.Name {
		return a.fail(L("角色 %s rename 后复核失败（身份或名称不同）；计划保留，不提交。"), s.Role)
	}
	if owner, st := a.nameOwner(s.Name); st != "taken" || owner != s.Pane {
		return a.fail(L("角色 %s rename 后任务名未指向同一 pane；计划保留，不提交。"), s.Role)
	}
	s.Result = "done"
	return a.save(t)
}

// commitRebind：只提交已通过跨字段校验的完整计划；清除计划、team_rev+1、追加审计。
func (a *app) commitRebind(t *Team) error {
	plan := t.RebindPending
	if err := validatePlanAgainstTeam(t, plan); err != nil {
		return a.fail(L("重绑计划与编组记录不一致（%v）：拒绝提交。"), err)
	}
	for _, s := range plan.Steps {
		r := t.role(s.Role)
		id := s.New
		r.Identity = &id
		if r.PaneTerminal != "" {
			r.PaneTerminal = id.TerminalID
		}
	}
	t.Anchor.TerminalID = plan.AnchorNew
	if plan.AnchorIdentityNew != nil {
		id := *plan.AnchorIdentityNew
		t.Anchor.Identity = &id
	}
	t.Rebinds = append(t.Rebinds, rebindRecord(plan, "committed"))
	t.RebindPending = nil
	t.TeamRev++
	if err := a.save(t); err != nil {
		return err
	}
	a.outf(L("重绑完成：编组 %s team_rev=%d，审计 %s。运行 nakama status 核对。\n"), t.TeamID, t.TeamRev, plan.OpID)
	return nil
}

// rebindRecord 把计划快照原样写入审计（不写输出片段）。
func rebindRecord(p *RebindPlan, result string) RebindRecord {
	return RebindRecord{
		OpID: p.OpID, At: p.At, Result: result, Confirm: p.Confirm, Steps: p.Steps,
		AnchorOld: p.AnchorOld, AnchorNew: p.AnchorNew,
		AnchorOldIdentity: p.AnchorIdentityOld, AnchorNewIdentity: p.AnchorIdentityNew,
	}
}

// discardRebind：放弃未提交的计划。只丢弃计划，不改身份记录；已完成的改名保留在 Herdr 中。
func (a *app) discardRebind(t *Team) error {
	p := t.RebindPending
	if p == nil {
		return a.fail(L("编组 %s 没有待放弃的重绑计划。"), t.TeamID)
	}
	t.Rebinds = append(t.Rebinds, rebindRecord(p, "discarded"))
	t.RebindPending = nil
	if err := a.save(t); err != nil {
		return err
	}
	a.outf(L("已放弃重绑计划 %s；身份记录未改动。重新执行 rebind 会按当前观测重新预检。\n"), p.OpID)
	return nil
}

// rebindRoleBinding 只读（status/init 共用）：
// bound = 与记录 identity 完全一致且当前任务名等于记录角色名；
// candidate = 同 pane 与 kind、记录 session 为空或相等，仅 terminal 变化（候选，不保证 rebind 可通过预检）；
// unverified = 其余（包括 identity 一致但任务名不是本角色名）。
func (a *app) rebindRoleBinding(r *Role) (string, agentObj) {
	if r.Identity == nil {
		return "unverified", agentObj{}
	}
	pane := string(r.Pane)
	o, ok := a.agentGet(pane)
	if !ok {
		return "unverified", agentObj{}
	}
	id, old := o.ident(), *r.Identity
	switch {
	case !id.complete() || id.PaneID != pane:
		return "unverified", o
	case id == old && o.Name == r.Name:
		return "bound", o
	case id == old:
		return "unverified", o
	case id.Kind == old.Kind && (old.Session == "" || id.Session == old.Session):
		return "candidate", o
	}
	return "unverified", o
}

// rebindAnchorBinding 只读：锚点 pane 的 workspace/tab/cwd 仍一致时，terminal 不同即 candidate。
func (a *app) rebindAnchorBinding(t *Team) string {
	p, msg := a.rebindPaneCtx(t, t.Anchor.PaneID)
	switch {
	case msg != "":
		return "unverified"
	case p.TerminalID == t.Anchor.TerminalID:
		return "bound"
	}
	return "candidate"
}
