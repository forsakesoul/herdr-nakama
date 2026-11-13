package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 状态：<STATE_DIR>/teams/<safe-id>.json。只解析不执行；同目录临时文件 + rename 原子替换；目录 0700、文件 0600。
// schema 2 增加 rebind 计划与审计；schema 3 增加任务目录、停用角色记录与 TEAM_UPDATE 待投递标记；
// schema 4 把 roles 改为成员键（O1/W1/A1）到角色的 map，支持多 Worker 与多 Advisor。
// 旧二进制读到更高的 schema 即拒绝，因此不会在丢失新字段的情况下覆盖状态。
const schemaVersion = 4

// 旧 schema 仍可读取；由本版本保存时升级为 schemaVersion。
const (
	schemaVersionV1 = 1
	schemaVersionV2 = 2
	schemaVersionV3 = 3
)

// 每类在册人数上限；只限制同时在册的人数，不限制实例号的最大值。
const maxPerRole = 4

// Ident 与 Bridge identOf 同字段：terminal|pane|kind|session。身份比较只走 sameIdent。
type Ident struct {
	TerminalID string `json:"terminal_id"`
	PaneID     string `json:"pane_id"`
	Kind       string `json:"kind"`
	Session    string `json:"session"`
}

func (i Ident) complete() bool { return i.TerminalID != "" && i.PaneID != "" && i.Kind != "" }
func (i Ident) String() string {
	return i.TerminalID + "|" + i.PaneID + "|" + i.Kind + "|" + i.Session
}

// 完整比较 terminal/pane/kind/session；session 可空，但空串与非空之间的变化同样算漂移。
func sameIdent(a, b Ident) bool { return a == b }

// NullStr：空串序列化为 null（pane 尚未 split）。
type NullStr string

func (s NullStr) MarshalJSON() ([]byte, error) {
	if s == "" {
		return []byte("null"), nil
	}
	return json.Marshal(string(s))
}

type Role struct {
	Name           string  `json:"name"`
	Pane           NullStr `json:"pane"`
	Engine         string  `json:"engine"`
	Instance       int     `json:"instance"`
	Launched       bool    `json:"launched"`
	Inited         bool    `json:"inited"`
	Stale          bool    `json:"stale,omitempty"`
	TeamUpdate     bool    `json:"team_update_pending,omitempty"` // 编组变更后尚未向该角色投递 [TEAM_UPDATE]
	PaneTerminal   string  `json:"pane_terminal,omitempty"`
	LaunchIdentity *Ident  `json:"launch_identity"`
	Identity       *Ident  `json:"identity"`
}

type Anchor struct {
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	CWD         string `json:"cwd"`
	TerminalID  string `json:"terminal_id,omitempty"`
	Identity    *Ident `json:"identity,omitempty"`
}

type Context struct {
	File    string `json:"file"`
	Trigger string `json:"trigger"`
	Sha256  string `json:"sha256"`
}

// Pending：副作用前先落盘；delivery 非 SENT 即无法确认，不重发。
type Pending struct {
	OpID     string `json:"op_id"`
	Role     string `json:"role"`
	Step     string `json:"step"`
	Delivery string `json:"delivery"`
}

type Team struct {
	SchemaVersion   int              `json:"schema_version"`
	TeamID          string           `json:"team_id"`
	Prefix          string           `json:"prefix"`
	Anchor          Anchor           `json:"anchor"`
	CWD             string           `json:"cwd"`
	Roles           map[string]*Role `json:"roles"` // 成员键 -> 角色，见 memberKey
	TeamRev         int              `json:"team_rev"`
	ProtocolVersion string           `json:"protocol_version"`
	Context         *Context         `json:"context"`
	Phase           string           `json:"phase"`
	Pending         *Pending         `json:"pending"`
	RebindPending   *RebindPlan      `json:"rebind_pending,omitempty"`
	Rebinds         []RebindRecord   `json:"rebinds,omitempty"`
	TaskDir         string           `json:"task_dir,omitempty"`
	Retired         []RetiredRole    `json:"retired,omitempty"`
}

// RetiredRole：被移除或替换的角色实例。实例号不复用（请求号 req 含实例号），pane 保留给用户处理。
type RetiredRole struct {
	Role     string `json:"role"`
	Name     string `json:"name"`
	Pane     string `json:"pane"`
	Engine   string `json:"engine"`
	Instance int    `json:"instance"`
	At       string `json:"at"`
	Op       string `json:"op"` // drop | swap
}

// RebindStep：重绑计划中的一个角色。Old 是记录身份，New 是预检时观测到的身份（用户确认的对象）。
// Result：空=未开始；started=已写入 rename 意图但未确认结果（重跑时按观测对账）；done=已复核。
type RebindStep struct {
	Role    string `json:"role"`
	Pane    string `json:"pane"`
	Name    string `json:"name"`
	Current string `json:"current_name,omitempty"`
	Rename  bool   `json:"rename"`
	Old     Ident  `json:"old"`
	New     Ident  `json:"new"`
	Result  string `json:"result,omitempty"`
}

// RebindPlan：副作用前落盘的完整计划。提交前不改变 identity/anchor 的当前值。
type RebindPlan struct {
	OpID              string       `json:"op_id"`
	At                string       `json:"at"`
	Steps             []RebindStep `json:"steps"`
	AnchorOld         string       `json:"anchor_old_terminal,omitempty"`
	AnchorNew         string       `json:"anchor_new_terminal,omitempty"`
	AnchorIdentityOld *Ident       `json:"anchor_identity_old,omitempty"`
	AnchorIdentityNew *Ident       `json:"anchor_identity_new,omitempty"`
	Confirm           []string     `json:"confirm,omitempty"`
}

// RebindRecord：审计记录。只记录操作结果、身份与确认方式，不记录任何输出片段。
type RebindRecord struct {
	OpID      string       `json:"op_id"`
	At        string       `json:"at"`
	Result    string       `json:"result"` // committed | discarded
	Confirm   []string     `json:"confirm,omitempty"`
	Steps     []RebindStep `json:"steps"`
	AnchorOld string       `json:"anchor_old_terminal,omitempty"`
	AnchorNew string       `json:"anchor_new_terminal,omitempty"`
	// 仅锚点有 identity 时存在：与计划快照一致的完整旧/新身份（session 确认前后值在此可见）。
	AnchorOldIdentity *Ident `json:"anchor_old_identity,omitempty"`
	AnchorNewIdentity *Ident `json:"anchor_new_identity,omitempty"`
}

// roleOrder 是角色的 kind 顺序（owner→worker→advisor）；成员遍历请用 t.members / t.membersOf。
var roleOrder = []string{"owner", "worker", "advisor"}

// 成员键 = 消息实例号（O1/W1/A1）。kind 由首字母推导，不单独持久化。
// 有副作用或输出顺序相关的遍历一律用 members/membersOf（有序），不要直接 range Roles。
const ownerKey = "O1"

var memberKeyRe = regexp.MustCompile(`^[OWA][1-9][0-9]*$`)
var keyLetter = map[string]string{"owner": "O", "worker": "W", "advisor": "A"}
var kindByLetter = map[byte]string{'O': "owner", 'W': "worker", 'A': "advisor"}

func kindOf(key string) string {
	if key == "" {
		return ""
	}
	return kindByLetter[key[0]]
}

func memberKey(kind string, inst int) string { return fmt.Sprintf("%s%d", keyLetter[kind], inst) }

func keyInstance(key string) int {
	n, _ := strconv.Atoi(key[1:])
	return n
}

func (t *Team) role(key string) *Role { return t.Roles[key] }

func (t *Team) owner() *Role { return t.Roles[ownerKey] }

func (t *Team) hasAdvisor() bool { return len(t.membersOf("advisor")) > 0 }

func (t *Team) addRole(key string, r *Role) {
	if t.Roles == nil {
		t.Roles = map[string]*Role{}
	}
	t.Roles[key] = r
}

// members 按 owner→worker→advisor、同类按实例号升序返回成员键。
func (t *Team) members() []string {
	var out []string
	for _, kind := range roleOrder {
		out = append(out, t.membersOf(kind)...)
	}
	return out
}

// initOrder 是 init / [TEAM_UPDATE] 的投递顺序：Worker → Advisor → Owner（与既有语义一致）。
func (t *Team) initOrder() []string {
	out := append([]string{}, t.membersOf("worker")...)
	out = append(out, t.membersOf("advisor")...)
	return append(out, t.membersOf("owner")...)
}

func (t *Team) membersOf(kind string) []string {
	var keys []string
	for k := range t.Roles {
		if kindOf(k) == kind {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keyInstance(keys[i]) < keyInstance(keys[j]) })
	return keys
}

var roleSuffix = map[string]string{"owner": "_design", "worker": "_execute", "advisor": "_advisor"}

// 角色名唯一生成点：首个实例沿用原名，替换/重加的实例追加实例号（旧实例仍占用原名）。
func roleName(prefix, role string, instance int) string {
	if instance <= 1 {
		return prefix + roleSuffix[role]
	}
	return fmt.Sprintf("%s%s%d", prefix, roleSuffix[role], instance)
}

// 下一个实例号（按 kind）：高于同类在册与所有已停用实例；实例号不复用。
func (t *Team) nextInstance(kind string) int {
	n := 0
	for _, k := range t.membersOf(kind) {
		if i := keyInstance(k); i > n {
			n = i
		}
	}
	for _, x := range t.Retired {
		if x.Role == kind && x.Instance > n {
			n = x.Instance
		}
	}
	return n + 1
}

func (t *Team) setPending(role, step, delivery string) {
	t.Pending = &Pending{OpID: fmt.Sprint(time.Now().UnixNano()), Role: role, Step: step, Delivery: delivery}
}

// 旧 schema（1-3）的 roles 是 owner/worker/advisor 三槽。读入时在内存中改写为成员键，文件本身不在此改写；
// 只有之后的实际保存才写出 schema 4。映射无法确定（如引用了不存在的角色）时拒绝，不补默认值。
type legacyRoles struct {
	Owner   *Role `json:"owner"`
	Worker  *Role `json:"worker"`
	Advisor *Role `json:"advisor"`
}

func (t *Team) UnmarshalJSON(b []byte) error {
	type plain Team
	aux := struct {
		*plain
		Roles json.RawMessage `json:"roles"`
	}{plain: (*plain)(t)}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	if t.SchemaVersion >= schemaVersion {
		return json.Unmarshal(aux.Roles, &t.Roles)
	}
	var lr legacyRoles
	if err := json.Unmarshal(aux.Roles, &lr); err != nil {
		return err
	}
	return t.migrateLegacy(lr)
}

func (t *Team) migrateLegacy(lr legacyRoles) error {
	t.Roles = map[string]*Role{}
	keyOf := map[string]string{}
	for _, kr := range []struct {
		kind string
		r    *Role
	}{{"owner", lr.Owner}, {"worker", lr.Worker}, {"advisor", lr.Advisor}} {
		if kr.r == nil {
			continue
		}
		k := memberKey(kr.kind, kr.r.Instance)
		keyOf[kr.kind] = k
		t.Roles[k] = kr.r
	}
	mapKind := func(kind string) (string, error) {
		if k, ok := keyOf[kind]; ok {
			return k, nil
		}
		return "", fmt.Errorf(L("旧状态引用了不存在的角色 %s；请人工检查，不自动接管。"), kind)
	}
	if p := t.Pending; p != nil && p.Role != "" {
		k, err := mapKind(p.Role)
		if err != nil {
			return err
		}
		p.Role = k
	}
	if rp := t.RebindPending; rp != nil {
		for i := range rp.Steps {
			k, err := mapKind(rp.Steps[i].Role)
			if err != nil {
				return err
			}
			rp.Steps[i].Role = k
		}
		// 确认项形如 worker:session；anchor:* 不属于角色，原样保留。历史 Rebinds 是审计快照，不改写。
		for i, c := range rp.Confirm {
			kind, rest, ok := strings.Cut(c, ":")
			if !ok || kind == "anchor" {
				continue
			}
			k, err := mapKind(kind)
			if err != nil {
				return err
			}
			rp.Confirm[i] = k + ":" + rest
		}
	}
	return nil
}

type store struct{ dir string }

func newStore() store {
	d := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if d == "" {
		base := os.Getenv("XDG_STATE_HOME")
		if base == "" {
			base = filepath.Join(os.Getenv("HOME"), ".local", "state")
		}
		d = filepath.Join(base, "nakama")
	}
	return store{dir: d}
}

func (s store) teamsDir() string      { return filepath.Join(s.dir, "teams") }
func (s store) locksDir() string      { return filepath.Join(s.dir, "locks") }
func (s store) archiveDir() string    { return filepath.Join(s.dir, "archive") }
func (s store) file(id string) string { return filepath.Join(s.teamsDir(), safeName(id)+".json") }
func (s store) exists(id string) bool {
	st, err := os.Stat(s.file(id))
	return err == nil && st.Mode().IsRegular()
}

// MkdirAll 不会改既有目录权限，这里显式把叶子目录收敛到 0700。
func mkdirSecure(d string) error {
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	return os.Chmod(d, 0o700)
}

func (s store) list() []string {
	ents, _ := os.ReadDir(s.teamsDir())
	var ids []string
	for _, e := range ents {
		if n := e.Name(); e.Type().IsRegular() && strings.HasSuffix(n, ".json") && !strings.Contains(n, ".tmp.") {
			ids = append(ids, strings.TrimSuffix(n, ".json"))
		}
	}
	sort.Strings(ids)
	return ids
}

// 按 anchor pane 找已有编组（宽松读取：损坏文件跳过，由后续 load 报错）。
func (s store) findByAnchor(pane string) string {
	for _, id := range s.list() {
		b, err := os.ReadFile(s.file(id))
		if err != nil {
			continue
		}
		var t struct {
			Anchor struct {
				PaneID string `json:"pane_id"`
			} `json:"anchor"`
		}
		if json.Unmarshal(b, &t) == nil && t.Anchor.PaneID == pane {
			return id
		}
	}
	return ""
}

// 按锚点或任一角色 pane 找编组（宽松读取，同 findByAnchor）；仅 init 使用，up/pick 仍用 findByAnchor。
func (s store) findByPane(pane string) []string {
	var ids []string
	for _, id := range s.list() {
		b, err := os.ReadFile(s.file(id))
		if err != nil {
			continue
		}
		var t struct {
			Anchor struct {
				PaneID string `json:"pane_id"`
			} `json:"anchor"`
			Roles map[string]struct {
				Pane NullStr `json:"pane"`
			} `json:"roles"`
		}
		if json.Unmarshal(b, &t) != nil {
			continue
		}
		hit := pane != "" && t.Anchor.PaneID == pane
		for _, r := range t.Roles {
			hit = hit || string(r.Pane) == pane
		}
		if hit {
			ids = append(ids, id)
		}
	}
	return ids
}

// load = 存在性 + 文件名/team_id 一致 + schema 校验；失败信息即给用户的错误。
func (s store) load(id string) (*Team, error) {
	b, err := os.ReadFile(s.file(id))
	if err != nil {
		return nil, fmt.Errorf(L("编组不存在：%s"), id)
	}
	var head struct {
		TeamID string `json:"team_id"`
	}
	_ = json.Unmarshal(b, &head)
	base := safeName(id)
	if head.TeamID != "" && head.TeamID != base {
		return nil, fmt.Errorf(L("状态文件名与 team_id 不一致（文件名 %s，team_id %s）；请人工检查，不自动接管。"), base, head.TeamID)
	}
	if err := validateTeam(b); err != nil {
		return nil, fmt.Errorf(L("状态文件校验失败：%s（schema/字段/身份不合法，请人工检查，不自动接管）"), id)
	}
	var t Team
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf(L("状态文件校验失败：%s（schema/字段/身份不合法，请人工检查，不自动接管）"), id)
	}
	return &t, nil
}

func (s store) save(t *Team) error {
	if err := mkdirSecure(s.teamsDir()); err != nil {
		return err
	}
	// 本版本写出的状态一律为当前 schema；旧版本读到更高 schema 即拒绝，不会丢字段地覆盖。
	t.SchemaVersion = schemaVersion
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	dest := s.file(t.TeamID)
	f, err := os.CreateTemp(s.teamsDir(), filepath.Base(dest)+".tmp.*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(append(b, '\n'))
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp, 0o600)
	}
	if werr == nil {
		werr = os.Rename(tmp, dest)
	}
	if werr != nil {
		os.Remove(tmp)
		return werr
	}
	return nil
}

// ---- 校验：显式判存在性与类型，缺字段/null 不会被 Go 零值当有效 ----
var prefixStateRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

func nonEmptyStr(v any) bool { s, ok := v.(string); return ok && s != "" }

func validIdent(v any, pane any, needSessionType bool) bool {
	m, ok := v.(map[string]any)
	if !ok || !nonEmptyStr(m["terminal_id"]) || !nonEmptyStr(m["pane_id"]) || !nonEmptyStr(m["kind"]) {
		return false
	}
	if needSessionType {
		if s, has := m["session"]; has && s != nil {
			if _, isStr := s.(string); !isStr {
				return false
			}
		}
	}
	return m["pane_id"] == pane && pane != nil
}

// rebind 计划校验：字段齐备、角色名合法、结果取值受限；不校验与当前角色的一致性（由 rebind 预检负责）。
// validRoleRef：角色引用在 schema 1-3 为 owner/worker/advisor，在 schema 4 为成员键。
func validRoleRef(v any, ver float64) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	if ver >= schemaVersion {
		return memberKeyRe.MatchString(s)
	}
	return s == "owner" || s == "worker" || s == "advisor"
}

func validRebindPlan(v any, ver float64) bool {
	p, ok := v.(map[string]any)
	if !ok || !nonEmptyStr(p["op_id"]) {
		return false
	}
	steps, ok := p["steps"].([]any)
	if !ok || len(steps) == 0 {
		return false
	}
	for _, sv := range steps {
		s, ok := sv.(map[string]any)
		if !ok || !nonEmptyStr(s["pane"]) || !nonEmptyStr(s["name"]) {
			return false
		}
		if !validRoleRef(s["role"], ver) {
			return false
		}
		if _, isBool := s["rename"].(bool); !isBool || !validIdent(s["old"], s["pane"], true) || !validIdent(s["new"], s["pane"], true) {
			return false
		}
		switch s["result"] {
		case nil, "", "started", "done":
		default:
			return false
		}
	}
	if c := p["confirm"]; c != nil {
		list, ok := c.([]any)
		if !ok {
			return false
		}
		for _, x := range list {
			if _, isStr := x.(string); !isStr {
				return false
			}
		}
	}
	for _, k := range []string{"anchor_identity_old", "anchor_identity_new"} {
		if v := p[k]; v != nil {
			m, ok := v.(map[string]any)
			if !ok || !validIdent(v, m["pane_id"], true) {
				return false
			}
		}
	}
	return true
}

func validateTeam(raw []byte) error {
	bad := fmt.Errorf("invalid")
	var t map[string]any
	if json.Unmarshal(raw, &t) != nil || t == nil {
		return bad
	}
	v, ok := t["schema_version"].(float64)
	if !ok || v != float64(int(v)) || v < schemaVersionV1 || v > schemaVersion {
		return bad
	}
	// 低版本 schema 不得携带更高版本才有的字段：出现即视为损坏，而不是静默忽略。
	if v < schemaVersionV3 && (t["task_dir"] != nil || t["retired"] != nil) {
		return bad
	}
	if td, has := t["task_dir"]; has && td != nil && !nonEmptyStr(td) {
		return bad
	}
	if rt, has := t["retired"]; has && rt != nil {
		list, isList := rt.([]any)
		if !isList {
			return bad
		}
		for _, x := range list {
			m, isObj := x.(map[string]any)
			inst, isNum := m["instance"].(float64)
			if !isObj || !isNum || inst < 1 || !nonEmptyStr(m["name"]) || (m["role"] != "worker" && m["role"] != "advisor") {
				return bad
			}
		}
	}
	// schema 1 不得携带 rebind 字段：旧状态里出现它们视为损坏，而不是静默忽略。
	if v == schemaVersionV1 && (t["rebind_pending"] != nil || t["rebinds"] != nil) {
		return bad
	}
	if rp := t["rebind_pending"]; rp != nil && !validRebindPlan(rp, v) {
		return bad
	}
	if rbs, has := t["rebinds"]; has && rbs != nil {
		list, isList := rbs.([]any)
		if !isList {
			return bad
		}
		for _, rb := range list {
			m, isObj := rb.(map[string]any)
			if !isObj || !nonEmptyStr(m["op_id"]) || (m["result"] != "committed" && m["result"] != "discarded") {
				return bad
			}
		}
	}
	prefix, _ := t["prefix"].(string)
	if !nonEmptyStr(t["team_id"]) || t["team_id"] != t["prefix"] || !prefixStateRe.MatchString(prefix) {
		return bad
	}
	anchor, ok := t["anchor"].(map[string]any)
	if !ok || !nonEmptyStr(anchor["pane_id"]) {
		return bad
	}
	phase, _ := t["phase"].(string)
	switch phase {
	case "starting", "panes_ready", "launched", "initialized", "changing":
	default:
		return bad
	}
	if v < schemaVersionV3 && phase == "changing" {
		return bad
	}
	roles, ok := t["roles"].(map[string]any)
	if !ok {
		return bad
	}
	counts := map[string]int{}
	for key, rv := range roles {
		kind := key
		if v >= schemaVersion {
			if !memberKeyRe.MatchString(key) {
				return bad
			}
			kind = kindOf(key)
		} else if key != "owner" && key != "worker" && key != "advisor" {
			return bad
		}
		if rv == nil {
			// 旧 schema 只允许 advisor 为空；schema 4 的成员值一律非 null。
			if v < schemaVersion && kind == "advisor" {
				continue
			}
			return bad
		}
		r, ok := rv.(map[string]any)
		if !ok || (v < schemaVersionV3 && r["team_update_pending"] != nil) {
			return bad
		}
		inst, isNum := r["instance"].(float64)
		if !isNum || inst != float64(int64(inst)) || inst < 1 {
			return bad
		}
		if v >= schemaVersion && int(inst) != keyInstance(key) {
			return bad
		}
		if r["name"] != roleName(prefix, kind, int(inst)) {
			return bad
		}
		_, inited := r["inited"].(bool)
		_, launched := r["launched"].(bool)
		pane := r["pane"]
		if _, isStr := r["name"].(string); !isStr || !nonEmptyStr(r["engine"]) || !inited || !launched {
			return bad
		}
		if pane != nil && !nonEmptyStr(pane) {
			return bad
		}
		// changing 只允许被新增/替换的 Worker / Advisor 尚无 pane；Owner 不可变更。
		if pane == nil && phase != "starting" && (phase != "changing" || kind == "owner") {
			return bad
		}
		counts[kind]++
		if r["inited"] == true && r["identity"] == nil {
			return bad
		}
		if r["identity"] != nil && !validIdent(r["identity"], pane, true) {
			return bad
		}
		if r["launch_identity"] != nil && !validIdent(r["launch_identity"], pane, false) {
			return bad
		}
	}
	if v < schemaVersion && (roles["owner"] == nil || roles["worker"] == nil) {
		return bad
	}
	if v >= schemaVersion {
		// schema 4 严格计数：恰好一个 Owner（O1），Worker 1..4，Advisor 0..4；不先过滤坏键再修复。
		if counts["owner"] != 1 || roles[ownerKey] == nil || counts["worker"] < 1 || counts["worker"] > maxPerRole || counts["advisor"] > maxPerRole {
			return bad
		}
		// 已停用实例与在册同类实例号不得重复（实例号不复用）。
		if rt, _ := t["retired"].([]any); rt != nil {
			for _, x := range rt {
				m, _ := x.(map[string]any)
				inst, _ := m["instance"].(float64)
				if _, live := roles[memberKey(fmt.Sprint(m["role"]), int(inst))]; live {
					return bad
				}
			}
		}
	}
	if p := t["pending"]; p != nil {
		pm, ok := p.(map[string]any)
		if !ok {
			return bad
		}
		if _, isStr := pm["step"].(string); !isStr {
			return bad
		}
		switch pm["delivery"] {
		case "NOT_SENT", "SENT", "UNKNOWN":
		default:
			return bad
		}
		if role, has := pm["role"]; has && role != nil && role != "" && !validRoleRef(role, v) {
			return bad
		}
	}
	return nil
}
