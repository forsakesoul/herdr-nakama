package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 提示词组装：编组映射 + 项目上下文（若有）+ protocol.md + 角色模板（含团队规则块）。
// prompts/*.md 保持原文，protocol_version 是其 sha256，所以运行时读取而不 embed。
// prompts/<lang>/ 按当前语言选模板；up 与 init 须用同一语言，否则 protocol hash 不同会被当作漂移拒绝。
func (a *app) promptPath(name string) string {
	return filepath.Join(a.root, "prompts", lang(), name+".md")
}
func (a *app) protocolPath() string { return a.promptPath("protocol") }
func (a *app) protocolSha() (string, error) {
	return sha256File(a.protocolPath())
}

// 模板标记：team-rule:<variant>、mid-task 两类块；块内行仅在命中时输出。
func renderRole(tpl, variant string, incMid bool) string {
	var out strings.Builder
	blk := -1 // -1 不在块内；1 输出；0 丢弃
	lines := strings.Split(tpl, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "<!-- team-rule:"):
			name := strings.TrimSuffix(strings.TrimPrefix(line, "<!-- team-rule:"), " -->")
			blk = b2i(name == variant)
			continue
		case strings.HasPrefix(line, "<!-- /team-rule:"):
			blk = -1
			continue
		case line == "<!-- mid-task -->":
			blk = b2i(incMid)
			continue
		case line == "<!-- /mid-task -->":
			blk = -1
			continue
		}
		if blk != 0 {
			out.WriteString(line + "\n")
		}
	}
	return out.String()
}

// 只取命中 variant 的 team-rule 块正文（[TEAM_UPDATE] 用它更新分工）。
func teamRule(tpl, variant string) string {
	var out strings.Builder
	in := false
	for _, line := range strings.Split(tpl, "\n") {
		switch {
		case line == "<!-- team-rule:"+variant+" -->":
			in = true
		case strings.HasPrefix(line, "<!-- /team-rule:"):
			in = false
		case in:
			out.WriteString(line + "\n")
		}
	}
	return out.String()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (t *Team) mapping() string {
	o, w, ad := t.Roles.Owner, t.Roles.Worker, t.Roles.Advisor
	s := fmt.Sprintf(L("任务=%s；Owner=%s，pane=%s，%s；执行者=%s，pane=%s，%s；"), t.Prefix, o.Name, o.Pane, o.Engine, w.Name, w.Pane, w.Engine)
	adv := L("无")
	if ad != nil {
		s += fmt.Sprintf(L("独立 Advisor=%s，pane=%s，%s。"), ad.Name, ad.Pane, ad.Engine)
		adv = fmt.Sprintf("A%d", ad.Instance)
	} else {
		s += L("无独立 Advisor。")
	}
	return s + fmt.Sprintf(L("消息实例：Owner=O%d；Worker=W%d；Advisor=%s"), o.Instance, w.Instance, adv)
}

// 映射之后的附加行：已停用实例、任务目录与清理命令。旧组（无任务目录、无停用实例）为空，prompt 不变。
func (a *app) mappingExtras(t *Team) string {
	var b strings.Builder
	if len(t.Retired) > 0 {
		var xs []string
		for _, x := range t.Retired {
			xs = append(xs, fmt.Sprintf("%s%d=%s(pane=%s)", strings.ToUpper(x.Role[:1]), x.Instance, x.Name, x.Pane))
		}
		fmt.Fprintf(&b, L("已停用实例（不再向其派单，也不接收其回报）：%s\n"), strings.Join(xs, "；"))
	}
	if t.TaskDir != "" {
		exe, _ := os.Executable()
		cmd := "HERDR_PLUGIN_STATE_DIR=" + posixQuote(a.st.dir) + " " + posixQuote(exe) + " clean --team " + t.TeamID + " --task <task_id> --yes"
		fmt.Fprintf(&b, L("任务目录=%s。清理命令（仅在用户明确授权删除后执行）：%s\n"), t.TaskDir, cmd)
	}
	return b.String()
}

// [TEAM_UPDATE]：最新映射 + 该角色在新编组下的分工规则块。
func (a *app) teamUpdatePrompt(t *Team, rn string) (string, error) {
	variant := "none"
	if t.hasAdvisor() {
		variant = "advisor"
	}
	tpl, err := os.ReadFile(a.promptPath(rn))
	if err != nil {
		return "", a.fail(L("无法读取角色模板 %s：%v"), rn, err)
	}
	return fmt.Sprintf("[TEAM_UPDATE] team_rev=%d\n%s\n%s%s", t.TeamRev, t.mapping(), a.mappingExtras(t), teamRule(string(tpl), variant)) +
		L("这是编组控制消息：以本条为准更新编组映射与分工，不回 ACK，不改变当前交接版本、冻结范围和授权。Owner 向新实例派单前，先核对被替换的旧执行者已停止写入。"), nil
}

// 组装完整 prompt；与旧实现一样去掉尾部换行。
func (a *app) assemblePrompt(t *Team, rn string) (string, error) {
	variant := "none"
	if t.hasAdvisor() {
		variant = "advisor"
	}
	var b strings.Builder
	b.WriteString(t.mapping() + "\n" + a.mappingExtras(t) + "\n")
	if t.Context != nil && t.Context.File != "" {
		body, err := os.ReadFile(t.Context.File)
		if err != nil {
			return "", a.fail(L("声明的上下文文件不可读：%s；停止初始化。"), t.Context.File)
		}
		fmt.Fprintf(&b, L("项目上下文（来源：%s）：\n%s\n"), t.Context.File, body)
	}
	proto, err := os.ReadFile(a.protocolPath())
	if err != nil {
		return "", a.fail(L("无法读取 protocol.md：%v"), err)
	}
	b.Write(proto)
	b.WriteString("\n\n")

	incMid := rn == "advisor" && t.Roles.Owner.Inited
	tpl, err := os.ReadFile(a.promptPath(rn))
	if err != nil {
		return "", a.fail(L("无法读取角色模板 %s：%v"), rn, err)
	}
	role := renderRole(string(tpl), variant, incMid)
	b.WriteString(role)
	return strings.TrimRight(b.String(), "\n"), nil
}
