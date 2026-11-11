package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// 多 Worker / 多 Advisor：建组布局、数量守卫、中途变更守卫、映射格式与 schema 4 严格校验。全部在现有假 herdr 上验证。

// splitLayout 返回每次 pane split 的 (--pane, --direction)，按调用顺序。
func splitLayout(e *tenv) [][2]string {
	var out [][2]string
	for _, v := range e.argvs() {
		if len(v) > 1 && v[0] == "pane" && v[1] == "split" {
			var p [2]string
			for i := 2; i+1 < len(v); i++ {
				switch v[i] {
				case "--pane":
					p[0] = v[i+1]
				case "--direction":
					p[1] = v[i+1]
				}
			}
			out = append(out, p)
		}
	}
	return out
}

func TestUpMultiMembersLayoutAndInit(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up", "--workers", "2", "--advisor", "cc", "--advisors", "2")
	id := e.teamID()
	// owner 锚点右侧；W1 从锚点下；W2 从 W1 下；A1 从 owner 下；A2 从 A1 下。
	eq(t, fmt.Sprint(splitLayout(e)), fmt.Sprint([][2]string{
		{"w0:p1", "right"}, {"w0:p1", "down"}, {"w0:p11", "down"}, {"w0:p10", "down"}, {"w0:p13", "down"},
	}), "split 次数与布局基准 pane")
	eq(t, e.str("roles", "W2", "name"), id+"_execute2", "W2 名称")
	eq(t, e.str("roles", "A2", "name"), id+"_advisor2", "A2 名称")
	eq(t, e.str("roles", "W2", "pane"), "w0:p12", "W2 pane")
	e.mustRun("init", "--team", id)
	eq(t, e.count("agent", "prompt"), 5, "5 个成员各收 1 次初始化")
	for _, name := range []string{id + "_design", id + "_execute", id + "_execute2", id + "_advisor", id + "_advisor2"} {
		eq(t, len(e.prompts(name)), 1, "每个成员恰好一次 prompt："+name)
	}
	has(t, e.prompts(id + "_execute2")[0], "你的实例：W2", "成员得知自己的实例键")
	has(t, e.prompts(id + "_execute2")[0], "Worker=W1="+id+"_execute/W2="+id+"_execute2", "多 Worker 映射")
	eq(t, e.str("phase"), "initialized", "全部初始化")
}

func TestUpCountFlagsRejectedWithoutSideEffects(t *testing.T) {
	e := newEnv(t)
	e.config("normal") // 默认 advisor 为 none
	teams := filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "teams")
	noSide := func(msg string) {
		t.Helper()
		eq(t, e.count("pane", "split"), 0, msg+"：零 split")
		ents, _ := os.ReadDir(teams)
		eq(t, len(ents), 0, msg+"：零状态")
	}
	e.mustFail("up", "--advisor", "none", "--advisors", "2")
	noSide("--advisor none 与 --advisors 互斥")
	e.mustFail("up", "--workers", "5")
	noSide("--workers 超上限")
	e.mustFail("up", "--workers", "0")
	noSide("--workers 0")
	e.mustFail("up", "--advisor", "cc", "--advisors", "5")
	noSide("--advisors 超上限")
	e.mustFail("up", "--advisors", "2")
	noSide("默认 advisor 引擎为 none 时不能凭空建 Advisor")
	e.mustRun("up", "--advisors", "0")
	eq(t, e.count("pane", "split"), 2, "--advisors 0：仅 owner+worker")
	eq(t, e.str("roles", "A1"), "<nil>", "--advisors 0 不建 Advisor")
}

func TestMemberMutationGuards(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up", "--workers", "2", "--advisor", "cc")
	id := e.teamID()
	e.mustRun("init", "--team", id)

	has(t, e.mustFail("drop", "--team", id, "--role", "worker"), "--instance", "多 Worker 缺 --instance 拒绝")
	e.mustRun("drop", "--team", id, "--role", "worker", "--instance", "2")
	eq(t, e.str("roles", "W2"), "<nil>", "W2 移出在册表")
	has(t, e.mustFail("drop", "--team", id, "--role", "worker", "--instance", "1"), "只剩一个 Worker", "唯一 Worker 不可移除")
	e.mustRun("add", "--team", id, "--role", "worker")
	eq(t, e.str("roles", "W3", "name"), id+"_execute3", "实例号不复用，新 Worker 为 W3")
	e.mustRun("init", "--team", id)
	has(t, e.mustFail("swap", "--team", id, "--role", "worker"), "--instance", "多 Worker 替换缺 --instance 拒绝")
	has(t, e.mustFail("swap", "--team", id, "--role", "worker", "--instance", "2"), "没有在册的", "替换已移除实例拒绝")
}

func TestMappingSingleMatchesLegacyAndMultiListsEachMember(t *testing.T) {
	single := &Team{Prefix: "dm_x", Roles: map[string]*Role{
		"O1": {Name: "dm_x_design", Pane: "p1", Engine: "cc", Instance: 1},
		"W1": {Name: "dm_x_execute", Pane: "p2", Engine: "cc", Instance: 1},
	}}
	eq(t, single.mapping(), "任务=dm_x；Owner=dm_x_design，pane=p1，cc；执行者=dm_x_execute，pane=p2，cc；无独立 Advisor。消息实例：Owner=O1；Worker=W1；Advisor=无",
		"单成员映射与单槽实现逐字一致")

	multi := &Team{Prefix: "dm_x", Roles: map[string]*Role{
		"O1": {Name: "dm_x_design", Pane: "p1", Engine: "cc", Instance: 1},
		"W1": {Name: "dm_x_execute", Pane: "p2", Engine: "cc", Instance: 1},
		"W2": {Name: "dm_x_execute2", Pane: "p3", Engine: "cc", Instance: 2},
		"A1": {Name: "dm_x_advisor", Pane: "p4", Engine: "cc", Instance: 1},
		"A2": {Name: "dm_x_advisor2", Pane: "p5", Engine: "cc", Instance: 2},
	}}
	got := multi.mapping()
	has(t, got, "执行者=dm_x_execute，pane=p2，cc；执行者=dm_x_execute2，pane=p3，cc；", "每个 Worker 一段")
	has(t, got, "独立 Advisor=dm_x_advisor，pane=p4，cc；独立 Advisor=dm_x_advisor2，pane=p5，cc。", "每个 Advisor 一段，末尾句号")
	has(t, got, "Worker=W1=dm_x_execute/W2=dm_x_execute2", "多 Worker 实例行带名称")
	has(t, got, "Advisor=A1=dm_x_advisor/A2=dm_x_advisor2", "多 Advisor 实例行带名称")
}

func TestV4StrictValidation(t *testing.T) {
	cases := map[string]func(m map[string]any){
		"旧键混入":        func(m map[string]any) { m["roles"].(map[string]any)["worker"] = map[string]any{"name": "x"} },
		"成员为 null":    func(m map[string]any) { m["roles"].(map[string]any)["W1"] = nil },
		"Worker 数为 0": func(m map[string]any) { delete(m["roles"].(map[string]any), "W1") },
		"key 与实例不一致":  func(m map[string]any) { dig(m, "roles", "W1").(map[string]any)["instance"] = 2 },
		"未知成员键":       func(m map[string]any) { m["roles"].(map[string]any)["X1"] = map[string]any{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.config("normal")
			e.mustRun("up")
			id := e.teamID()
			e.stateSet(mutate)
			has(t, e.mustFail("status", "--team", id), "状态文件校验失败", "严格拒绝")
		})
	}
}
