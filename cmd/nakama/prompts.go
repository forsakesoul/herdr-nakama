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

// 组装完整 prompt；与旧实现一样去掉尾部换行。
func (a *app) assemblePrompt(t *Team, rn string) (string, error) {
	variant := "none"
	if t.hasAdvisor() {
		variant = "advisor"
	}
	var b strings.Builder
	b.WriteString(t.mapping() + "\n\n")
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
