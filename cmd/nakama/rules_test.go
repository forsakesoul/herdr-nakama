package main

import (
	"os"
	"testing"
)

// rules 的表驱动测试：复用 harness（fake herdr），按三语、三角色断言规则重读的内容边界。

type rulesLang struct {
	lang        string
	init        string            // 初始化段标题（三角色相同）
	mid         map[string]string // 中途加入段标题（owner 无）
	title       map[string]string
	shared      string
	ownerOnly   string
	workerOnly  string
	advisorOnly string
}

var rulesLangs = []rulesLang{
	{lang: "zh", init: "## 现在：只初始化", mid: map[string]string{"worker": "## 中途加入", "advisor": "## 中途加入"},
		title:  map[string]string{"owner": "你的角色：Owner", "worker": "你的角色：Worker", "advisor": "你的角色：Advisor"},
		shared: "### 通信", ownerOnly: "### 执行巡检（Owner）", workerOnly: "### Worker 投递", advisorOnly: "### 独立 Advisor 输出"},
	{lang: "en", init: "## Now: initialize only", mid: map[string]string{"worker": "## Joining mid-task", "advisor": "## Joining mid-task"},
		title:  map[string]string{"owner": "Your role: Owner", "worker": "Your role: Worker", "advisor": "Your role: Advisor"},
		shared: "### Communication", ownerOnly: "### Execution patrol (Owner)", workerOnly: "### Worker delivery", advisorOnly: "### Independent Advisor output"},
	{lang: "ja", init: "## 今：初期化のみ", mid: map[string]string{"worker": "## タスクの途中から参加", "advisor": "## 途中参加"},
		title:  map[string]string{"owner": "あなたのロール：Owner", "worker": "あなたのロール：Worker", "advisor": "あなたのロール：Advisor"},
		shared: "### 通信", ownerOnly: "### 実行の巡回（Owner）", workerOnly: "### Worker の送信", advisorOnly: "### 独立 Advisor の出力"},
}

var rulesKeys = map[string]string{"owner": "O1", "worker": "W1", "advisor": "A1"}
var rulesSuffix = map[string]string{"owner": "_design", "worker": "_execute", "advisor": "_advisor"}

// runSplit 分离 stdout 与 stderr，便于断言拒绝路径的 stdout 为空。
func runSplit(e *tenv, args ...string) (stdout, stderr string, ok bool) {
	var out, errb syncBuf
	a := &app{stdout: &out, stderr: &errb, root: e.repoRoot, st: newStore()}
	defer a.unlockAll()
	err := a.dispatch(args)
	return out.String(), errb.String(), err == nil
}

func TestRulesMatrixAcrossLanguages(t *testing.T) {
	for _, lc := range rulesLangs {
		t.Run(lc.lang, func(t *testing.T) {
			t.Setenv("NAKAMA_LANG", lc.lang)
			e, id, _ := sendSetup(t, true)
			// 初次 init prompt 含 init 段（owner 在 worker/advisor 之后初始化，故不含 mid-task）。
			for kind, key := range rulesKeys {
				has(t, e.prompts(id + rulesSuffix[kind])[0], lc.init, "初次 init 含 init 段: "+kind)
				_ = key
			}
			for kind, key := range rulesKeys {
				out := e.mustRun("rules", "--team", id, "--from", key)
				has(t, out, lc.title[kind], "标题: "+kind)
				has(t, out, lc.shared, "共享协议: "+kind)
				has(t, out, " rules --team "+id, "规则命令行: "+kind)
				hasNot(t, out, lc.init, "rules 不含 init 段: "+kind)
				for _, m := range lc.mid {
					hasNot(t, out, m, "rules 不含 mid-task 段: "+kind)
				}
				hasNot(t, out, "<!--", "标记行不输出: "+kind)
				for k2, sec := range map[string]string{"owner": lc.ownerOnly, "worker": lc.workerOnly, "advisor": lc.advisorOnly} {
					if k2 == kind {
						has(t, out, sec, "裁剪后保留本角色协议: "+kind)
					} else {
						hasNot(t, out, sec, "裁剪后不含他角色协议: "+kind+" 不含 "+k2)
					}
				}
			}
		})
	}
}

// 同一组内：TEAM_UPDATE 不含 init 段；mid-task 段按 owner 是否已初始化的条件出现，rules 永不出现。
func TestMidTaskAndTeamUpdateBoundaries(t *testing.T) {
	for _, lc := range rulesLangs {
		t.Run(lc.lang, func(t *testing.T) {
			t.Setenv("NAKAMA_LANG", lc.lang)
			e, id, _ := sendSetup(t, true)
			a := &app{stdout: &syncBuf{}, stderr: &syncBuf{}, root: e.repoRoot, st: newStore()}
			tm, err := a.st.load(id)
			if err != nil {
				t.Fatal(err)
			}
			tm.owner().Inited = false
			for _, kind := range []string{"worker", "advisor"} {
				p, err := a.assemblePrompt(tm, rulesKeys[kind])
				if err != nil {
					t.Fatal(err)
				}
				hasNot(t, p, lc.mid[kind], "owner 未 inited 时无 mid-task: "+kind)
			}
			tm.owner().Inited = true
			for _, kind := range []string{"worker", "advisor"} {
				p, _ := a.assemblePrompt(tm, rulesKeys[kind])
				has(t, p, lc.mid[kind], "owner inited 时 init prompt 带 mid-task: "+kind)
				r, _ := a.rulesPrompt(tm, rulesKeys[kind])
				hasNot(t, r, lc.mid[kind], "rules 不带 mid-task: "+kind)
				hasNot(t, r, lc.init, "rules 不带 init: "+kind)
			}
			for _, kind := range []string{"owner", "worker", "advisor"} {
				u, err := a.teamUpdatePrompt(tm, rulesKeys[kind])
				if err != nil {
					t.Fatal(err)
				}
				hasNot(t, u, lc.init, "TEAM_UPDATE 不含 init 段: "+kind)
				has(t, u, "[TEAM_UPDATE]", "TEAM_UPDATE 头: "+kind)
			}
		})
	}
}

// 拒绝路径：stdout 为空、状态文件字节不变、任何成员都未收到 prompt。
func TestRulesRejectionsLeaveNoTrace(t *testing.T) {
	type tc struct {
		name  string
		setup func(e *tenv, id string) (args []string, env map[string]string)
		msg   string
	}
	cases := []tc{
		{"protocol 漂移", func(e *tenv, id string) ([]string, map[string]string) {
			e.stateSet(func(m map[string]any) { m["protocol_version"] = "sha256:stale" })
			return []string{"rules", "--team", id, "--from", "W1"}, nil
		}, "当前编组的规则与已安装插件不一致"},
		{"context hash 漂移", func(e *tenv, id string) ([]string, map[string]string) {
			e.stateSet(func(m map[string]any) {
				ctx, ok := m["context"].(map[string]any)
				if !ok {
					panic("测试前提：编组应声明上下文文件")
				}
				ctx["sha256"] = "stale"
			})
			return []string{"rules", "--team", id, "--from", "W1"}, nil
		}, "当前编组的规则与已安装插件不一致"},
		{"上下文文件缺失", func(e *tenv, id string) ([]string, map[string]string) {
			os.Remove(e.ctx)
			return []string{"rules", "--team", id, "--from", "W1"}, nil
		}, "声明的上下文文件不可读"},
		{"--from 与 pane 不一致", func(e *tenv, id string) ([]string, map[string]string) {
			return []string{"rules", "--team", id, "--from", "W1"}, map[string]string{"HERDR_PANE_ID": e.str("roles", "O1", "pane")}
		}, "与当前 pane 的成员"},
		{"已停用实例", func(e *tenv, id string) ([]string, map[string]string) {
			old := e.str("roles", "W1", "pane")
			e.mustRun("swap", "--team", id, "--role", "worker")
			return []string{"rules", "--team", id}, map[string]string{"HERDR_PANE_ID": old}
		}, "在册成员"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("NAKAMA_LANG", "zh")
			e, id, _ := sendSetup(t, true)
			args, env := c.setup(e, id)
			for k, v := range env {
				t.Setenv(k, v)
			}
			before, _ := os.ReadFile(e.stateFile())
			n := 0
			for _, s := range []string{"_design", "_execute", "_advisor"} {
				n += len(e.prompts(id + s))
			}
			out, errOut, ok := runSplit(e, args...)
			if ok {
				t.Fatalf("应拒绝：\n%s", out)
			}
			eq(t, out, "", "stdout 为空")
			has(t, errOut, c.msg, "拒绝原因")
			after, _ := os.ReadFile(e.stateFile())
			eq(t, string(after), string(before), "状态文件字节不变")
			m := 0
			for _, s := range []string{"_design", "_execute", "_advisor"} {
				m += len(e.prompts(id + s))
			}
			eq(t, m, n, "未投递 prompt")
		})
	}
}

// 无任务目录的旧组：rules 实际成功，输出含规则命令行，且不输出清理命令。
func TestRulesOnLegacyTeamWithoutTaskDir(t *testing.T) {
	e, id, _ := sendSetup(t, false)
	e.stateSet(func(m map[string]any) { delete(m, "task_dir") })
	out := e.mustRun("rules", "--team", id, "--from", "W1")
	has(t, out, " rules --team "+id, "旧组输出规则命令行")
	has(t, out, "你的角色：Worker", "旧组输出本角色规则")
	hasNot(t, out, "clean --team "+id, "无任务目录时不输出清理命令行")
}

// init 的协议漂移文案保持原样；rules 的文案只在 rules 路径出现。
func TestInitDriftMessageUnchanged(t *testing.T) {
	t.Setenv("NAKAMA_LANG", "zh")
	e, id, _ := sendSetup(t, false)
	e.stateSet(func(m map[string]any) { m["protocol_version"] = "sha256:stale" })
	has(t, e.mustFail("init", "--team", id), "protocol.md 已变化", "init 漂移文案不变")
	hasNot(t, e.mustFail("init", "--team", id), "当前编组的规则与已安装插件不一致", "init 不用 rules 文案")
}
