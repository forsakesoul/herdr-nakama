package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 以给定 stdin 在进程内运行命令（clean 的交互选择用）。
func (e *tenv) runIn(in string, args ...string) (bool, string) {
	var buf syncBuf
	a := &app{stdout: &buf, stderr: &buf, root: e.repoRoot, st: newStore(), in: bufio.NewReader(strings.NewReader(in))}
	defer a.unlockAll()
	return a.dispatch(args) == nil, buf.String()
}

func (e *tenv) lastPrompt(name string) string {
	e.t.Helper()
	ps := e.prompts(name)
	if len(ps) == 0 {
		e.t.Fatalf("%s 没有收到 prompt", name)
	}
	return ps[len(ps)-1]
}

func TestTaskDirCreatedAndInPrompt(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	id := e.teamID()
	dir := filepath.Join(realPath(e.proj), ".nakama", id)
	eq(t, e.str("task_dir"), dir, "task_dir 落盘")
	eq(t, e.str("schema_version"), "4", "schema 3")
	if !realDir(dir) {
		t.Fatalf("任务目录未创建：%s", dir)
	}
	b, _ := os.ReadFile(filepath.Join(realPath(e.proj), ".nakama", ".gitignore"))
	eq(t, string(b), "*\n", ".nakama 整体被 git 忽略")
	e.mustRun("init", "--team", id)
	p := e.lastPrompt(id + "_design")
	has(t, p, "任务目录="+dir, "prompt 给出任务目录")
	has(t, p, "clean --team "+id+" --task <task_id> --yes", "prompt 给出清理命令")
	has(t, p, "HERDR_PLUGIN_STATE_DIR=", "清理命令带状态目录")
	has(t, e.mustRun("status", "--team", id), "task_dir="+dir+" tasks=0", "status 显示任务目录")
}

func TestAddDropSwapFlow(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	id := e.teamID()
	e.mustRun("init", "--team", id)
	owner, worker := id+"_design", id+"_execute"

	// add：split + 启动，初始化与通知留给 init
	e.mustRun("add", "--team", id, "--engine", "cc")
	eq(t, e.count("pane", "split"), 3, "add 新增 1 次 split")
	eq(t, e.str("phase"), "launched", "add 后待 init")
	eq(t, e.str("team_rev"), "2", "team_rev 加一")
	eq(t, e.str("roles", "A1", "name"), id+"_advisor", "首个 advisor 沿用原名")
	eq(t, e.str("roles", "O1", "team_update_pending"), "true", "owner 待通知")
	before := len(e.prompts(owner))
	e.mustRun("init", "--team", id)
	eq(t, e.str("phase"), "initialized", "init 后 initialized")
	has(t, e.lastPrompt(id+"_advisor"), "任务已在进行中加入", "中途加入的 advisor 带 mid-task 块")
	eq(t, len(e.prompts(owner)), before+1, "owner 只收到 1 条 TEAM_UPDATE")
	up := e.lastPrompt(owner)
	has(t, up, "[TEAM_UPDATE] team_rev=2", "TEAM_UPDATE 头")
	has(t, up, "当前有一个或多个独立 advisor", "按新编组给出分工规则块")
	hasNot(t, up, "当前没有独立 advisor", "不带另一变体")
	has(t, e.lastPrompt(worker), "[TEAM_UPDATE] team_rev=2", "worker 也收到")
	eq(t, e.str("roles", "O1", "team_update_pending"), "<nil>", "标记清除")
	e.mustRun("init", "--team", id)
	eq(t, len(e.prompts(owner)), before+1, "重复 init 不重发 TEAM_UPDATE")

	// drop：无 Herdr 写操作，立即通知
	splits := e.count("pane", "split")
	e.mustRun("drop", "--team", id)
	eq(t, e.count("pane", "split"), splits, "drop 不 split")
	eq(t, e.str("roles", "A1"), "<nil>", "advisor 移除")
	eq(t, e.state()["retired"].([]any)[0].(map[string]any)["name"], any(id+"_advisor"), "记入 retired")
	up = e.lastPrompt(owner)
	has(t, up, "[TEAM_UPDATE] team_rev=3", "drop 后通知")
	has(t, up, "已停用实例", "列出停用实例")
	has(t, up, "A1="+id+"_advisor", "停用实例含实例号")
	has(t, up, "当前没有独立 advisor", "回到无 advisor 规则")
	eq(t, e.count("agent", "rename"), 2, "不改名、不触碰已停用 pane")

	// 再次 add：实例号不复用
	e.mustRun("add", "--team", id, "--engine", "cc")
	eq(t, e.str("roles", "A2", "name"), id+"_advisor2", "新实例名带实例号")
	eq(t, e.str("roles", "A2", "instance"), "2", "A2")
	e.mustRun("init", "--team", id)

	// swap worker：旧实例停用，新实例 W2
	out := e.mustRun("swap", "--team", id, "--role", "worker")
	has(t, out, "已停用但未关闭", "提示旧 pane 保留")
	eq(t, e.str("roles", "W2", "name"), id+"_execute2", "新 worker 名")
	eq(t, e.str("roles", "W2", "engine"), "pi/model-x:low", "缺省沿用原引擎")
	e.mustRun("init", "--team", id)
	has(t, e.lastPrompt(id+"_execute2"), "Worker=W2", "新 worker 的映射实例")
	up = e.lastPrompt(owner)
	has(t, up, "W1="+worker, "owner 得知旧 worker 停用")
	has(t, up, "执行者="+id+"_execute2", "owner 得知新 worker")
	for _, v := range e.argvs() {
		if len(v) > 2 && v[0] == "agent" && v[1] == "prompt" && v[2] == worker && strings.Contains(v[3], "team_rev=5") {
			t.Fatalf("不得向已停用的旧 worker 投递")
		}
	}
	st := e.mustRun("status", "--team", id)
	has(t, st, "retired W1 name="+worker, "status 列出停用实例")
	has(t, st, "phase=initialized", "最终 initialized")
}

func TestChangeGuards(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	id := e.teamID()
	splits := e.count("pane", "split")
	has(t, e.mustFail("add", "--team", id, "--engine", "cc"), "尚未完成初始化", "init 前不可变更")
	e.mustRun("init", "--team", id)
	has(t, e.mustFail("swap", "--team", id, "--role", "owner"), "Owner 不可替换", "owner 不可替换")
	has(t, e.mustFail("swap", "--team", id, "--role", "advisor"), "没有独立 Advisor", "无 advisor 不可 swap")
	has(t, e.mustFail("drop", "--team", id), "没有独立 Advisor", "无 advisor 不可 drop")
	has(t, e.mustFail("add", "--team", id, "--engine", "bogus/x"), "无法识别", "非法引擎")
	has(t, e.mustFail("add", "--team", id), "无法识别", "profile 的 none 不是引擎")
	eq(t, e.count("pane", "split"), splits, "拒绝路径零 split")
	eq(t, e.str("team_rev"), "1", "拒绝路径不改状态")

	// protocol 漂移：新角色会收到完整 prompt，必须零副作用拒绝
	e.stateSet(func(m map[string]any) { m["protocol_version"] = "deadbeef" })
	has(t, e.mustFail("add", "--team", id, "--engine", "cc"), "protocol.md 已变化", "protocol 漂移拒绝")
	eq(t, e.count("pane", "split"), splits, "漂移零 split")
}

func TestChangeResumeAfterInterruptedLaunch(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	id := e.teamID()
	e.mustRun("init", "--team", id)
	e.mustRun("add", "--team", id, "--engine", "cc")
	pane := e.str("roles", "A1", "pane")
	// 模拟 split 完成后、启动前被中断：pane 已记录，实例未启动
	e.stateSet(func(m map[string]any) {
		m["phase"] = "changing"
		adv := dig(m, "roles", "A1").(map[string]any)
		adv["launched"], adv["launch_identity"] = false, nil
	})
	e.rowDel("@" + pane)
	has(t, e.mustFail("init", "--team", id), "phase=changing", "变更未完成时 init 拒绝")
	has(t, e.mustFail("drop", "--team", id), "add / swap 或 up", "drop 指向补齐入口")
	runs := e.count("pane", "run")
	// swap 遇到未完成变更：补齐上一次，本次不执行
	has(t, e.mustFail("swap", "--team", id, "--role", "worker"), "本次请求未执行", "不静默执行别的操作")
	eq(t, e.count("pane", "run"), runs+1, "补齐启动")
	eq(t, e.str("roles", "W1", "instance"), "1", "本次 swap 未执行")
	eq(t, e.count("pane", "split"), 3, "不重复 split")
	eq(t, e.str("phase"), "launched", "补齐后 launched")
	e.mustRun("init", "--team", id)
	eq(t, e.str("phase"), "initialized", "可继续 init")
}

func TestTeamUpdateDeferredWhileBusy(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up", "--advisor", "cc")
	id := e.teamID()
	e.mustRun("init", "--team", id)
	n := e.count("agent", "prompt")
	t.Setenv("FAKE_STATUS", "working")
	has(t, e.mustFail("drop", "--team", id), "[TEAM_UPDATE] 未投递", "忙碌时不投递")
	eq(t, e.count("agent", "prompt"), n, "不向 working 角色发 prompt")
	eq(t, e.str("roles", "A1"), "<nil>", "变更本身已落盘")
	eq(t, e.str("roles", "W1", "team_update_pending"), "true", "标记保留")
	has(t, e.mustRun("status", "--team", id), "team_update=pending", "status 显示待通知")
	t.Setenv("FAKE_STATUS", "idle")
	e.mustRun("init", "--team", id)
	eq(t, e.count("agent", "prompt"), n+2, "空闲后补投 owner + worker")
	eq(t, e.str("roles", "W1", "team_update_pending"), "<nil>", "标记清除")
}

func TestCleanTaskDirs(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	id := e.teamID()
	e.mustRun("init", "--team", id)
	dir := e.str("task_dir")
	mk := func(task string) {
		os.MkdirAll(filepath.Join(dir, task), 0o700)
		os.WriteFile(filepath.Join(dir, task, "W1-000.md"), []byte("done"), 0o600)
	}
	exists := func(p string) bool { _, err := os.Lstat(p); return err == nil }
	mk("op_1")
	mk("op_2")
	mk("op_3")
	outside := filepath.Join(e.root, "outside")
	os.MkdirAll(outside, 0o700)
	os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("x"), 0o600)
	os.Symlink(outside, filepath.Join(dir, "evil"))

	withTTY(t, false)
	out := e.mustRun("clean")
	has(t, out, id+"/op_1  files=1 size=4B", "列出任务目录")
	hasNot(t, out, "evil", "符号链接不列出")
	has(t, out, "未删除任何内容", "非交互且未选择：只列出")
	has(t, e.mustFail("clean", "--team", id, "--task", "op_1"), "需要 --yes", "非交互无 --yes 拒绝")
	has(t, e.mustFail("clean", "--task", "op_1", "--yes"), "--task 需要同时指定 --team", "--task 需 --team")
	for _, bad := range []string{"evil", "../outside", "nope"} {
		has(t, e.mustFail("clean", "--team", id, "--task", bad, "--yes"), "任务目录不存在", "拒绝 "+bad)
	}
	if !exists(filepath.Join(dir, "op_1")) || !exists(filepath.Join(outside, "keep.txt")) {
		t.Fatal("拒绝路径不得删除任何内容")
	}

	has(t, e.mustRun("clean", "--team", id, "--task", "op_1", "--yes"), "已删除", "按任务删除")
	if exists(filepath.Join(dir, "op_1")) || !exists(filepath.Join(dir, "op_2")) {
		t.Fatal("只删除选中的任务目录")
	}

	// 交互选择：编号 + 确认
	withTTY(t, true)
	ok, out := e.runIn("1\nn\n", "clean")
	if !ok || !exists(filepath.Join(dir, "op_2")) {
		t.Fatalf("未确认不得删除：\n%s", out)
	}
	ok, out = e.runIn("1 1\ny\n", "clean") // 重复编号只删一次，不报错
	if !ok || exists(filepath.Join(dir, "op_2")) || !exists(filepath.Join(dir, "op_3")) {
		t.Fatalf("确认后只删选中项：\n%s", out)
	}
	withTTY(t, false)

	// 在册角色状态查不到（Herdr 不可用）：按忙碌处理
	t.Setenv("NAKAMA_FAKE_GET_FAIL", "1")
	has(t, e.mustFail("clean", "--all", "--yes"), "无法确认", "状态未知不清理")
	t.Setenv("NAKAMA_FAKE_GET_FAIL", "")
	if !exists(filepath.Join(dir, "op_3")) {
		t.Fatal("状态未知不得删除")
	}

	// Worker 仍在执行：默认拒绝，--force 才删
	t.Setenv("FAKE_STATUS", "working")
	has(t, e.mustFail("clean", "--all", "--yes"), "任务目录仍可能在使用", "执行中默认不清理")
	if !exists(filepath.Join(dir, "op_3")) {
		t.Fatal("执行中不得删除")
	}
	e.mustRun("clean", "--all", "--yes", "--force")
	if exists(filepath.Join(dir, "op_3")) || !exists(filepath.Join(outside, "keep.txt")) || !exists(filepath.Join(dir, "evil")) {
		t.Fatal("--force 只删任务目录，不跟随符号链接")
	}
	t.Setenv("FAKE_STATUS", "idle")

	// 状态里的 task_dir 被改成别处：不列出、不删除
	mk("op_4")
	e.stateSet(func(m map[string]any) { m["task_dir"] = outside })
	has(t, e.mustRun("clean", "--all", "--yes"), "没有可清理的任务目录", "非记录路径不清理")
	if !exists(filepath.Join(outside, "keep.txt")) || !exists(filepath.Join(dir, "op_4")) {
		t.Fatal("被篡改的 task_dir 不得用于删除")
	}
}

func TestTaskDirRefusesSymlink(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	outside := filepath.Join(e.root, "outside")
	os.MkdirAll(outside, 0o700)
	os.Symlink(outside, filepath.Join(e.proj, ".nakama"))
	out := e.mustRun("up")
	has(t, out, "不是真实目录", "提示符号链接")
	eq(t, e.str("task_dir"), "<nil>", "不记录任务目录")
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Fatalf("不得写到项目外：%v", ents)
	}
}

func TestTeamMenuAddsAdvisor(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	id := e.teamID()
	e.mustRun("init", "--team", id)
	e.anchorEnv(t)
	withTTY(t, true)
	ok, out := e.pick("a\ncc\nn\n")
	if !ok || e.str("roles", "A1") != "<nil>" {
		t.Fatalf("未确认不得变更：\n%s", out)
	}
	ok, out = e.pick("a\ncc\ny\n\n")
	if !ok {
		t.Fatalf("菜单 add 应成功：\n%s", out)
	}
	has(t, out, "Advisor → cc", "确认摘要")
	eq(t, e.str("roles", "A1", "engine"), "cc", "菜单完成 add")
	ents, _ := os.ReadDir(filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "locks"))
	eq(t, len(ents), 0, "结果页前已放锁")

	// 从角色 pane 打开也进入同一菜单；替换时默认沿用原引擎
	t.Setenv("NAKAMA_ANCHOR_PANE", e.str("roles", "O1", "pane"))
	e.mustRun("init", "--team", id)
	ok, out = e.pick("w\n\ny\n\n")
	if !ok {
		t.Fatalf("菜单 swap 应成功：\n%s", out)
	}
	has(t, out, "Worker 引擎 [默认 pi/model-x:low]", "默认沿用原引擎")
	eq(t, e.str("roles", "W2", "instance"), "2", "菜单完成 swap")
	ok, out = e.pick("z\n\n")
	if ok {
		t.Fatalf("未知选项应非零：\n%s", out)
	}
}

func TestSchemaGuardsNewFields(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	id := e.teamID()
	// 低版本 schema 携带新字段视为损坏
	e.stateSet(func(m map[string]any) { m["schema_version"] = 2; toLegacy(m) })
	has(t, e.mustFail("status", "--team", id), "状态文件校验失败", "schema 2 不得带 task_dir")
	// 去掉新字段的 schema 2 仍可读，且不会凭空得到任务目录
	e.stateSet(func(m map[string]any) { delete(m, "task_dir") })
	hasNot(t, e.mustRun("status", "--team", id), "task_dir=", "旧组没有任务目录")
	for _, f := range []func(m map[string]any){
		func(m map[string]any) { m["phase"] = "changing" },
		func(m map[string]any) { dig(m, "roles", "worker").(map[string]any)["team_update_pending"] = true },
	} {
		e.stateSet(f)
		has(t, e.mustFail("status", "--team", id), "状态文件校验失败", "schema 2 不得带 schema 3 字段")
		e.stateSet(func(m map[string]any) {
			m["phase"] = "launched"
			delete(dig(m, "roles", "worker").(map[string]any), "team_update_pending")
		})
	}
	// changing 中 Owner 也必须有 pane（Owner 不可变更）
	ownerPane := e.str("roles", "owner", "pane")
	e.stateSet(func(m map[string]any) {
		m["schema_version"], m["phase"] = 3, "changing"
		dig(m, "roles", "owner").(map[string]any)["pane"] = nil
	})
	has(t, e.mustFail("status", "--team", id), "状态文件校验失败", "changing 下 Owner 无 pane 视为损坏")
	e.stateSet(func(m map[string]any) {
		m["schema_version"], m["phase"] = 2, "launched"
		dig(m, "roles", "owner").(map[string]any)["pane"] = ownerPane
	})
	e.mustRun("init", "--team", id)
	eq(t, e.str("schema_version"), "4", "保存后升级")
	hasNot(t, e.lastPrompt(id+"_design"), "任务目录=", "旧组 prompt 不提任务目录")
}

// toLegacy 把 schema 4 的成员键还原为 schema 1-3 的 owner/worker/advisor 三槽，用于伪造旧版本状态。
func toLegacy(m map[string]any) {
	out := map[string]any{}
	for k, v := range m["roles"].(map[string]any) {
		switch k[0] {
		case 'O':
			out["owner"] = v
		case 'W':
			out["worker"] = v
		case 'A':
			out["advisor"] = v
		}
	}
	m["roles"] = out
}

// PR #9 修复：替换菜单回车采用成员当前引擎（提示中的默认值），不回落到 profile 默认。
func TestTeamMenuReplaceEnterKeepsMemberEngine(t *testing.T) {
	e := newEnv(t)
	e.config("normal") // profile 的 worker 默认为 pi/model-x:low
	e.mustRun("up", "--worker", "cc")
	id := e.teamID()
	e.mustRun("init", "--team", id)
	e.anchorEnv(t)
	withTTY(t, true)
	t.Setenv("NAKAMA_ANCHOR_PANE", e.str("roles", "O1", "pane"))
	ok, out := e.pick("w\n\ny\n")
	if !ok {
		t.Fatalf("菜单替换应成功：\n%s", out)
	}
	has(t, out, "Worker 引擎 [默认 cc]", "提示默认为成员当前引擎")
	has(t, out, "替换 W1 → cc", "确认摘要使用成员当前引擎")
	eq(t, e.str("roles", "W2", "engine"), "cc", "回车后替换实例沿用成员当前引擎")
}
