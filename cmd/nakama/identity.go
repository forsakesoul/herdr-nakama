package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// 锚点：来自注入上下文/显式参数，并用只读 pane get/current 核对；不得以插件根 $PWD 当项目目录。
func (a *app) normalizeAnchor(cliPane, cliCwd string) error {
	pane := cliPane
	if pane == "" {
		var ctx struct {
			Pane string `json:"focused_pane_id"`
		}
		_ = json.Unmarshal([]byte(os.Getenv("HERDR_PLUGIN_CONTEXT_JSON")), &ctx)
		pane = ctx.Pane
	}
	if pane == "" {
		pane = os.Getenv("HERDR_PANE_ID")
	}
	var r result
	if pane != "" {
		if r = a.herdr("pane", "get", pane); !r.ok() {
			return a.fail(L("无法读取锚点 pane（%s）。拒绝继续。"), pane)
		}
	} else if r = a.herdr("pane", "current", "--current"); !r.ok() {
		return a.fail(L("无法确定锚点 pane（无注入上下文且 pane current 失败）。拒绝继续。"))
	}
	p := parsePane(r.stdout)
	if p.PaneID == "" {
		return a.fail(L("锚点 pane 不可用（响应缺少 pane_id）。拒绝继续。"))
	}
	if p.CWD == "" {
		return a.fail(L("锚点 pane 缺少 cwd；不得以插件根目录当项目目录。拒绝继续。"))
	}
	if cliCwd != "" && realPath(cliCwd) != realPath(p.CWD) {
		return a.fail(L("--cwd（%s）与锚点 pane 的 cwd（%s）不一致。拒绝继续。"), cliCwd, p.CWD)
	}
	a.anchor = Anchor{PaneID: p.PaneID, CWD: realPath(p.CWD), WorkspaceID: p.WorkspaceID, TabID: p.TabID}
	return nil
}

// 首次建组：记录 anchor 的 terminal/workspace，以及可得的完整 agent 身份；get 失败或缺字段 → 拒绝。
func (a *app) captureAnchorIdentity(t *Team) error {
	r := a.herdr("pane", "get", a.anchor.PaneID)
	if !r.ok() {
		return a.fail(L("无法读取 anchor pane %s 的 terminal_id；拒绝继续。"), a.anchor.PaneID)
	}
	p := parsePane(r.stdout)
	if p.TerminalID == "" {
		return a.fail(L("anchor pane %s 缺少 terminal_id；拒绝继续。"), a.anchor.PaneID)
	}
	t.Anchor.TerminalID = p.TerminalID
	if p.WorkspaceID != "" {
		t.Anchor.WorkspaceID = p.WorkspaceID
	}
	if o, ok := a.agentGet(a.anchor.PaneID); ok && o.ident().complete() {
		id := o.ident()
		t.Anchor.Identity = &id
	}
	return a.save(t)
}

// 已有组：anchor workspace/terminal/身份指纹必须与记录一致；get 失败或缺字段 → 拒绝（不放行）。
func (a *app) checkAnchorServer(t *Team) error {
	an := t.Anchor
	if an.PaneID == "" {
		return nil
	}
	r := a.herdr("pane", "get", an.PaneID)
	if !r.ok() {
		return a.fail(L("无法读取 anchor pane %s；拒绝继续。"), an.PaneID)
	}
	p := parsePane(r.stdout)
	if p.TerminalID == "" {
		return a.fail(L("anchor pane %s 缺少 terminal_id；拒绝继续。"), an.PaneID)
	}
	if an.WorkspaceID != "" && p.WorkspaceID != an.WorkspaceID {
		return a.fail(L("anchor workspace 不一致（记录 %s，当前 %s）；请人工核对，不继续。"), an.WorkspaceID, p.WorkspaceID)
	}
	if an.TerminalID != "" && p.TerminalID != an.TerminalID {
		return a.fail(L("anchor terminal_id 不一致（记录 %s，当前 %s）；请人工核对，不继续。"), an.TerminalID, p.TerminalID)
	}
	if an.Identity != nil {
		o, ok := a.agentGet(an.PaneID)
		if !ok {
			return a.fail(L("anchor pane agent get 失败；拒绝继续。"))
		}
		if !o.ident().complete() {
			return a.fail(L("anchor 身份字段缺失；拒绝继续。"))
		}
		if !sameIdent(o.ident(), *an.Identity) {
			return a.fail(L("anchor 身份指纹不一致（terminal/session 变化）；拒绝继续。"))
		}
	}
	return nil
}

// codex 在首次 prompt 开始会话后才上报 session，所以已冻结 session 为空时，允许在 inited 复核中
// 单向补全为观测值；terminal/pane/kind 必须完全相同，补全后 session 再变化即漂移。
// 只用于 inited 角色的重入复核，首次冻结、rename 后与 prompt 前仍走严格比较。
func fillSession(frozen, observed Ident) (Ident, bool) {
	if frozen.Session != "" || observed.Session == "" {
		return frozen, false
	}
	filled := frozen
	filled.Session = observed.Session
	return filled, sameIdent(filled, observed)
}

// 核验角色身份：观测字段齐备、pane 一致，且完整指纹等于 expected（nil 时只核 pane）。
// firstFreeze 只影响报错措辞：此时 expected 是启动观测（launch_identity），而非已冻结 identity。
// 返回非空错误信息表示失败，由调用方决定是否输出。
func (a *app) verifyIdentity(t *Team, rn string, expected *Ident, firstFreeze bool) (agentObj, string) {
	r := t.role(rn)
	obj, ok := a.observeAgent(r.Name)
	if !ok {
		return obj, fmt.Sprintf(L("角色 %s 无法观测（agent get 失败）：%s"), rn, r.Name)
	}
	id := obj.ident()
	if !id.complete() {
		return obj, fmt.Sprintf(L("角色 %s 身份字段缺失（terminal/pane/kind）：%s"), rn, r.Name)
	}
	if id.PaneID != string(r.Pane) {
		return obj, fmt.Sprintf(L("角色 %s pane 漂移：记录 %s，观测 %s"), rn, r.Pane, id.PaneID)
	}
	if expected != nil && !sameIdent(id, *expected) {
		if firstFreeze {
			return obj, fmt.Sprintf(L("角色 %s 实例指纹与启动观测不一致（rename 后 session/terminal 变化）；拒绝 prompt，不冻结。"), rn)
		}
		return obj, fmt.Sprintf(L("角色 %s 实例身份漂移：记录 %s，观测 %s"), rn, expected, id)
	}
	return obj, ""
}

var bridgeSlugInvalid = regexp.MustCompile(`[^a-z0-9_-]`)
var bridgeSlugDashes = regexp.MustCompile(`-+`)

// 与 remote-herdr-bridge src/runtime/targets.ts 的 autoNameFor 保持一致；只兼容精确自动名。
func bridgeAutoName(cwd, terminal string) string {
	slug := bridgeSlugInvalid.ReplaceAllString(strings.ToLower(baseName(cwd)), "-")
	slug = strings.Trim(bridgeSlugDashes.ReplaceAllString(slug, "-"), "-")
	if slug == "" {
		slug = "agent"
	}
	if slug[0] < 'a' || slug[0] > 'z' {
		slug = "a-" + slug
	}
	if len(slug) > 27 {
		slug = slug[:27]
	}
	slug = strings.TrimRight(slug, "-")
	hash := sha256.Sum256([]byte(terminal))
	return fmt.Sprintf("%s-%x", slug, hash[:2])
}

// 手动启动器首次冻结取证：证明实例属于本次启动，且未被其他用途具名。
func (a *app) verifyFirstFreeze(t *Team, rn string) string {
	r := t.role(rn)
	pane := string(r.Pane)
	if r.PaneTerminal == "" {
		return fmt.Sprintf(L("角色 %s 缺少 split 时的 terminal 记录，无法证明实例连续性；停止（UNKNOWN）。"), rn)
	}
	eng, ok := parseEngine(r.Engine)
	if !ok {
		return fmt.Sprintf(L("角色 %s 引擎种类未知：%s"), rn, r.Engine)
	}
	obj, ok := a.observeAgent(pane)
	if !ok {
		return fmt.Sprintf(L("角色 %s pane %s 无法观测；停止（UNKNOWN）。"), rn, pane)
	}
	id := obj.ident()
	if !id.complete() {
		return fmt.Sprintf(L("角色 %s pane %s 身份字段不完整；停止（UNKNOWN）。"), rn, pane)
	}
	// 已记录的完整启动指纹：任一字段（含 session）漂移都不认领。
	if r.LaunchIdentity != nil && !sameIdent(id, *r.LaunchIdentity) {
		return fmt.Sprintf(L("角色 %s 实例指纹与启动观测不一致（session/terminal 变化）；拒绝，停止（UNKNOWN）。"), rn)
	}
	if id.TerminalID != r.PaneTerminal {
		return fmt.Sprintf(L("角色 %s pane %s terminal 不连续（split=%s 观测=%s）；停止（UNKNOWN）。"), rn, pane, r.PaneTerminal, id.TerminalID)
	}
	if want := eng.kind(); id.Kind != want {
		return fmt.Sprintf(L("角色 %s kind 不符（期望 %s，观测 %s）；停止（UNKNOWN）。"), rn, want, id.Kind)
	}
	if obj.Name != "" && obj.Name != r.Name {
		// 自动名不代表身份：只有完整启动指纹仍严格一致时才允许替换为角色名。
		if r.LaunchIdentity == nil || obj.Name != bridgeAutoName(t.CWD, id.TerminalID) {
			return fmt.Sprintf(L("角色 %s pane %s 实例已有其他名字（%s）；拒绝，停止（UNKNOWN）。"), rn, pane, obj.Name)
		}
	}
	return ""
}

// 启动后落盘启动观测（launch_identity）；native 已具名时同时落盘 identity。
// 取证失败返回 false（不得 identity=null 却当成功）。
func (a *app) captureIdentity(t *Team, rn string) bool {
	r := t.role(rn)
	eng, _ := parseEngine(r.Engine)
	// 一次取证只有一个授权预算；每次观察（含首次）都经过 settle，冻结前的观测不是 blocked/unknown。
	gate := &authGate{a: a, rn: rn, pane: string(r.Pane), deadline: time.Now().Add(startWait())}
	var obj agentObj
	var ok bool
	if eng.needsRename() {
		obj, ok = a.observeLaunch(string(r.Pane))
	} else if obj, ok = a.observeAgent(r.Name); !ok {
		// native 身份一律用有界 agent get（结构见 tests/fixtures/agent_get.json）。
		obj, ok = a.observeAgent(string(r.Pane))
	}
	if ok {
		obj, ok = gate.settle(obj)
	}
	// cc/ccenv/pi 的 session 由 SessionStart hook 在进程被识别后才上报，qoder 也在启动几秒后才上报，
	// 而首次冻结严格比较 session：有界等它出现，避免记成空 session、init 时被判指纹漂移。始终没有则按空记录。
	// codex 要到首次 prompt 才有 session，不等。授权等待不计入这个轮询窗口，由 gate 的同一预算约束。
	for tries := 1; eng.launcher != "codex" && ok && obj.ident().Session == "" && tries < pollTries(); tries++ {
		sleepFor(a.poll)
		if o, got := a.agentGet(string(r.Pane)); got {
			obj = o
		}
		obj, ok = gate.settle(obj)
	}
	if !ok || !obj.ident().complete() {
		a.errf(L("角色 %s 启动后无法取得完整身份（terminal|pane|kind|session）；不冻结，停止。"), rn)
		return false
	}
	id := obj.ident()
	r.LaunchIdentity = &id
	if !eng.needsRename() {
		frozen := id
		r.Identity = &frozen
	}
	return a.save(t) == nil
}

// authGate 承载一次启动取证的授权等待：deadline 与提示只属于这一次取证，不随观测次数重置。
type authGate struct {
	a         *app
	rn, pane  string
	deadline  time.Time
	announced bool
}

// settle 在观测为 blocked（授权对话框）或 unknown（启动中尚未分类）时，按同一 deadline 重新观察，
// 直到离开这两种状态；返回的对象不是 blocked/unknown。不发送任何输入。
func (g *authGate) settle(obj agentObj) (agentObj, bool) {
	for obj.AgentStatus == "blocked" || obj.AgentStatus == "unknown" {
		if !g.announced {
			g.a.outf(L("%s 在 pane %s 等待授权或启动完成，请到该 pane 查看；Ctrl-C 取消。\n"), g.rn, g.pane)
			g.announced = true
		}
		if g.a.interrupted() {
			return obj, false
		}
		if time.Now().After(g.deadline) {
			g.a.errf(L("%s 在 pane %s 等待授权超时（上限 NAKAMA_START_WAIT_SECONDS=%.0f 秒）；停止。"), g.rn, g.pane, startWait().Seconds())
			return obj, false
		}
		sleepFor(g.a.poll)
		o, ok := g.a.agentGet(g.pane)
		if !ok {
			g.a.errf(L("%s 在 pane %s 等待授权时无法读取状态；停止。"), g.rn, g.pane)
			return obj, false
		}
		obj = o
	}
	return obj, true
}
