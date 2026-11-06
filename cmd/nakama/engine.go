package main

import (
	"fmt"
	"regexp"
	"strings"
)

// 引擎写法：<launcher>/<model>[:effort]。provider（pi）与 ccenv 的 env 文件路径由私有 profile 提供。
type engine struct{ launcher, model, effort string }

var (
	modelRe  = regexp.MustCompile(`^[A-Za-z0-9._-]*(\[[0-9]+[A-Za-z]\])?$`)
	effortRe = regexp.MustCompile(`^[a-z]*$`)
	solRe    = regexp.MustCompile(`^gpt-.*-sol(\[.*\])?$`)
)

func parseEngine(e string) (engine, bool) {
	var g engine
	g.launcher = e
	rest := ""
	if i := strings.IndexByte(e, '/'); i >= 0 {
		g.launcher, rest = e[:i], e[i+1:]
	}
	g.model = rest
	if i := strings.IndexByte(rest, ':'); i >= 0 {
		g.model, g.effort = rest[:i], rest[i+1:]
	}
	if !modelRe.MatchString(g.model) || !effortRe.MatchString(g.effort) {
		return g, false
	}
	switch g.launcher {
	case "codex":
		if g.model == "" {
			return g, false
		}
		if g.effort == "" {
			g.effort = "medium"
			if solRe.MatchString(g.model) {
				g.effort = "high"
			}
		}
	case "pi":
		if g.model == "" {
			return g, false
		}
		if g.effort == "" {
			g.effort = "medium"
		}
	case "qoder", "ccenv":
		if g.model == "" || g.effort != "" {
			return g, false
		}
	case "cc":
		// 裸 cc 默认 Opus 5.5 + medium；只给 effort 的写法非法。写了 model 而未写 effort 时不传 --effort。
		if g.model == "" {
			if g.effort != "" {
				return g, false
			}
			g.model, g.effort = "claude-opus-5-5", "medium"
		}
	default:
		return g, false
	}
	return g, true
}

// 引擎对应的 Herdr agent 种类（集中一处）：pi→pi，cc/ccenv→claude，codex→codex，qoder→qodercli。
func (g engine) kind() string {
	switch g.launcher {
	case "codex":
		return "codex"
	case "qoder":
		return "qodercli"
	case "cc", "ccenv":
		return "claude"
	}
	return "pi"
}

// cc/ccenv/pi 由 pane run 手动启动，init 时再 rename 绑定。
func (g engine) needsRename() bool {
	return g.launcher == "cc" || g.launcher == "ccenv" || g.launcher == "pi"
}

// 引擎启动提示（供 NEXT 输出）。
func (g engine) hint() string {
	switch g.launcher {
	case "codex":
		return fmt.Sprintf(L("/model 核对 %s + %s，并确认完全访问权限"), g.model, g.effort)
	case "qoder":
		return fmt.Sprintf(L("确认 %s 模型与 bypass_permissions 完全访问权限"), g.model)
	case "cc":
		m := g.model
		if g.effort != "" {
			m += " + " + g.effort
		}
		return fmt.Sprintf(L("确认走 Claude 官方订阅（无 ANTHROPIC_BASE_URL 覆盖）、模型为 %s，并已跳过权限检查"), m)
	case "ccenv":
		return fmt.Sprintf(L("确认实际为 %s 并已跳过权限检查"), g.model)
	case "pi":
		// 提示会进入 Herdr 的 action log，不得带 provider 等私有配置值。
		return fmt.Sprintf(L("确认 Pi 的模型为 %s、思考级别 %s，provider 与 profile 一致；已信任项目资源，工具按当前用户权限执行"), g.model, g.effort)
	}
	return ""
}

// pane run 的命令文本会进入目标 shell：env 文件内容绝不拼入，路径与参数一律 POSIX 单引号编码。
func (g engine) paneCommand(piProvider, ccEnv string) string {
	switch g.launcher {
	case "cc":
		c := "cc --model " + posixQuote(g.model)
		if g.effort != "" {
			c += " --effort " + posixQuote(g.effort)
		}
		return c
	case "ccenv":
		return "set -a; . " + posixQuote(ccEnv) + " || exit 1; set +a; cc --model " + posixQuote(g.model)
	}
	return "pi --approve --provider " + posixQuote(piProvider) + " --model " + posixQuote(g.model) + " --thinking " + posixQuote(g.effort)
}

// agent start 的参数（codex/qoder）。
func (g engine) startArgs() []string {
	if g.launcher == "codex" {
		return []string{"--no-daemon", "--model", g.model, "-c", `model_reasoning_effort="` + g.effort + `"`,
			"--dangerously-bypass-approvals-and-sandbox", "--no-alt-screen"}
	}
	return []string{"--model", g.model, "--permission-mode", "bypass_permissions"}
}
