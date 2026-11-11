package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestUpInitIdempotent2and3(t *testing.T) { // A: 2 方 / 3 方，重复 up/init 幂等
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	eq(t, e.count("pane", "split"), 2, "2 方应 2 次 split")
	eq(t, e.count("agent", "start"), 1, "唯一 codex 走 agent start")
	eq(t, e.count("pane", "run"), 1, "pi worker 走 pane run")
	eq(t, e.str("phase"), "launched", "phase")
	eq(t, e.str("roles", "O1", "instance"), "1", "owner 实例")
	eq(t, e.str("roles", "W1", "pane"), "w0:p11", "worker pane")
	eq(t, e.str("roles", "W1", "launch_identity", "terminal_id"), "term_w0:p11", "启动观测落盘")
	id := e.teamID()
	e.mustRun("init", "--team", id)
	eq(t, e.count("agent", "prompt"), 2, "init 初始化 owner+worker")
	eq(t, e.count("agent", "rename"), 1, "pi worker 需 rename 绑定")
	eq(t, e.str("phase"), "initialized", "phase")
	eq(t, e.str("roles", "W1", "identity", "terminal_id"), "term_w0:p11", "identity 落盘")
	e.mustRun("up")
	eq(t, e.count("pane", "split"), 2, "重复 up 不新增 split")
	e.mustRun("init", "--team", id)
	eq(t, e.count("agent", "prompt"), 2, "重复 init 不新增 prompt")

	e = newEnv(t)
	e.config("normal")
	e.mustRun("up", "--advisor", "cc")
	eq(t, e.count("pane", "split"), 3, "3 方应 3 次 split")
	eq(t, e.count("pane", "run"), 2, "worker pi + advisor cc")
	e.mustRun("init", "--team", e.teamID())
	eq(t, e.count("agent", "prompt"), 3, "三角色初始化")
	eq(t, e.count("agent", "rename"), 2, "pi+cc rename")
	has(t, e.mustRun("status", "--team", e.teamID()), "phase=initialized", "status")
}

func TestStaleAnchorLockBlocksUp(t *testing.T) { // B
	e := newEnv(t)
	e.config("normal")
	lock := filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "locks", "anchor-w0_p1.lock")
	os.MkdirAll(lock, 0o755)
	has(t, e.mustFail("up"), "锁", "提示锁")
	eq(t, e.count("pane", "split"), 0, "有锁不 split")
	if _, err := os.Stat(lock); err != nil {
		t.Fatal("遗留锁不得被自动接管/删除")
	}
	os.RemoveAll(lock)
	e.mustRun("up")
	eq(t, e.count("pane", "split"), 2, "清锁后正常建组")
}

func TestPromptTimeoutIsUnknownNoResend(t *testing.T) { // C
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	id := e.teamID()
	t.Setenv("FAKE_PROMPT_ERROR", "timeout")
	has(t, e.mustFail("init", "--team", id), "未确认完成", "提示已投递未确认")
	eq(t, e.str("pending", "delivery"), "UNKNOWN", "pending=UNKNOWN")
	before := e.count("agent", "prompt")
	has(t, e.mustFail("init", "--team", id), "UNKNOWN", "重入报 UNKNOWN")
	eq(t, e.count("agent", "prompt"), before, "重入不重发")
	_, st := e.run("status", "--team", id)
	has(t, st, "未确认", "status 提示 UNKNOWN")
}

func TestMissingContextRejected(t *testing.T) { // 声明的上下文缺失：up 在任何副作用前拒绝；init 路径（文件事后消失）仍拒绝
	e := newEnv(t)
	e.config("badctx")
	has(t, e.mustFail("up"), "上下文文件不可读", "up 拒绝提示")
	eq(t, e.count("pane", "split"), 0, "0 split")
	eq(t, e.count("agent", "start"), 0, "0 start")
	eq(t, e.count("pane", "run"), 0, "0 run")

	e = newEnv(t)
	e.config("normal")
	e.mustRun("up")
	os.Remove(e.ctx)
	has(t, e.mustFail("init", "--team", e.teamID()), "上下文文件不可读", "init 拒绝提示")
	eq(t, e.count("agent", "prompt"), 0, "缺上下文不发 prompt")
}

func TestHalfTeamResumeRejectsContextDrift(t *testing.T) { // starting 半组 + context 变化 → up 拒绝，0 新启动
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	e.stateSet(func(m map[string]any) {
		m["phase"] = "starting"
		w := dig(m, "roles", "W1").(map[string]any)
		w["pane"], w["launched"], w["launch_identity"], w["identity"] = nil, false, nil, nil
	})
	f, _ := os.OpenFile(e.ctx, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("改动后的上下文\n")
	f.Close()
	bs, br, bp := e.count("agent", "start"), e.count("pane", "run"), e.count("pane", "split")
	has(t, e.mustFail("up"), "上下文文件已变化", "拒绝漂移")
	eq(t, e.count("agent", "start"), bs, "0 新 start")
	eq(t, e.count("pane", "run"), br, "0 新 run")
	eq(t, e.count("pane", "split"), bp, "0 新 split")
}

// 信号只置停止标记：在途 split 子进程照常完成并落盘，之后不再开始任何副作用，锁由 defer 释放。
func TestSignalLetsInFlightSplitFinishThenStops(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	exe, _ := os.Executable()
	barrier := filepath.Join(e.root, "split-barrier")
	os.MkdirAll(barrier, 0o755)
	t.Setenv("NAKAMA_FAKE_SPLIT_BARRIER", barrier)
	cmd := exec.Command(exe, "--hr-run-main", "up")
	cmd.Env = append(os.Environ(), "HERDR_PLUGIN_ROOT="+e.repoRoot)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitFile(t, filepath.Join(barrier, "ready"))
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(barrier, "release"), nil, 0o644)
	err := cmd.Wait()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("父进程应在子进程完成后以 1 退出，得到 %v", err)
	}
	eq(t, e.count("pane", "split"), 1, "仅在途的那次 split")
	eq(t, e.str("roles", "O1", "pane"), "w0:p10", "在途 split 的结果已落盘")
	eq(t, e.count("agent", "start"), 0, "之后无 start")
	eq(t, e.count("pane", "run"), 0, "之后无 run")
	left, _ := filepath.Glob(filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "locks", "*.lock"))
	eq(t, len(left), 0, "锁已释放")
}

func TestAnchorCwdSource(t *testing.T) { // D: cwd 以锚点 pane 为准 / 缺 cwd 拒绝
	e := newEnv(t)
	e.config("normal")
	t.Chdir(e.root)
	e.mustRun("up")
	want, _ := filepath.EvalSymlinks(e.proj)
	eq(t, e.str("cwd"), want, "cwd 取自 anchor pane")

	e = newEnv(t)
	e.config("normal")
	t.Setenv("FAKE_ANCHOR_CWD_EMPTY", "1")
	has(t, e.mustFail("up"), "缺少 cwd", "拒绝缺 cwd")
}

func TestReplacedInstanceRejected(t *testing.T) { // D: 实例被替换
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	e.rowSet(e.str("roles", "W1", "name"), "w0:p99", "term_w0:p99", "codex", "")
	has(t, e.mustFail("init", "--team", e.teamID()), "漂移", "拒绝实例被替换")
	eq(t, e.count("agent", "prompt"), 0, "实例漂移不发 prompt（含不继续初始化后续角色）")
}

func TestCorruptStateRejected(t *testing.T) { // D
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	id := e.teamID()
	os.WriteFile(e.stateFile(), []byte("not json"), 0o600)
	has(t, e.mustFail("status", "--team", id), "校验失败", "损坏提示")
}

func TestShaAndConfigIsolation(t *testing.T) { // E
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	eq(t, e.str("roles", "O1", "instance"), "1", "owner 实例")
	eq(t, e.str("roles", "W1", "instance"), "1", "worker 实例")
	if !regexpMust(`^dm_[0-9a-f]{12}$`).MatchString(e.str("prefix")) {
		t.Fatalf("prefix 形如 dm_<12hex>：%s", e.str("prefix"))
	}
	pb, _ := os.ReadFile(filepath.Join(e.repoRoot, "prompts", "zh", "protocol.md"))
	eq(t, e.str("protocol_version"), sum(pb), "protocol sha256")
	cb, _ := os.ReadFile(e.ctx)
	eq(t, e.str("context", "sha256"), sum(cb), "context sha256")
	before, _ := os.ReadFile(e.stateFile())
	e.rawConfig(`{ "default_profile": { "owner": "cc", "worker": "cc", "advisor": "none", "pi_provider": "x" }, "projects": {} }`)
	e.run("status", "--team", e.teamID())
	after, _ := os.ReadFile(e.stateFile())
	eq(t, string(after), string(before), "运行中改 config 不改已建组")
}

func TestNaming(t *testing.T) { // F1: task_ 前缀 / 名称预校验 / 换行 prefix
	e := newEnv(t)
	e.config("noproj")
	t.Setenv("FAKE_STRICT_NAME", "1")
	e.mustRun("up")
	eq(t, e.count("pane", "split"), 2, "无项目建组")
	if !regexpMust(`^task_[0-9a-f]{12}_design$`).MatchString(e.str("roles", "O1", "name")) {
		t.Fatalf("缺省前缀应为 task_：%s", e.str("roles", "O1", "name"))
	}

	e = newEnv(t)
	e.config("badprefix")
	has(t, e.mustFail("up"), "短名", "提示项目短名非法")
	eq(t, e.count("pane", "split"), 0, "非法名零副作用（0 split）")

	e = newEnv(t)
	e.rawConfig(`{ "default_profile": { "owner": "codex/gpt-6.1-sol:high", "worker": "codex/gpt-6.1-sol:medium", "advisor": "none" }, "projects": { "demo-repo": { "prefix": "dm\nx", "context_file": "` + e.ctx + `", "trigger": ";dm.ctx" } } }`)
	e.mustFail("up")
	eq(t, e.count("pane", "split"), 0, "换行 prefix 0 split")
}

func TestFirstFreezeIdentityChecks(t *testing.T) { // F2 / F2b：首次冻结取证（后来占用者/换 terminal/换 session/缺字段/kind 不符/他名）
	cases := []struct {
		name   string
		tamper func(e *tenv, wp, wn string)
		want   string
	}{
		{"后来占用者", func(e *tenv, wp, _ string) { e.paneRowSet(wp, "term_latecomer", "pi", "") }, "指纹"},
		{"同 pane 换 terminal", func(e *tenv, wp, _ string) { e.paneRowSet(wp, "term_w0-p11-new", "pi", "") }, ""},
		{"仅 session 变化", func(e *tenv, wp, _ string) { e.paneRowSet(wp, "term_w0:p11", "pi", "sess-new") }, "指纹"},
		{"身份字段缺失", func(e *tenv, wp, _ string) { e.paneRowSet(wp, "", "pi", "") }, ""},
		{"kind 不符", func(e *tenv, wp, _ string) { e.paneRowSet(wp, "term_w0:p11", "claude", "") }, "指纹"},
		{"实例已具他名", func(e *tenv, wp, _ string) {
			e.rowDel("@" + wp)
			e.rowSet("othername", wp, "term_w0:p11", "pi", "")
		}, "已有其他名字"},
		{"自动名哈希不符", func(e *tenv, wp, _ string) {
			e.rowDel("@" + wp)
			e.rowSet("proj-0000", wp, "term_w0:p11", "pi", "")
		}, "已有其他名字"},
		{"自动名但会话漂移", func(e *tenv, wp, _ string) {
			e.rowDel("@" + wp)
			e.rowSet("proj-e2d2", wp, "term_w0:p11", "pi", "sess-new")
		}, "指纹"},
		{"自动名但无启动指纹", func(e *tenv, wp, _ string) {
			e.rowDel("@" + wp)
			e.rowSet("proj-e2d2", wp, "term_w0:p11", "pi", "")
			e.stateSet(func(m map[string]any) { asMap(asMap(m["roles"])["W1"])["launch_identity"] = nil })
		}, "缺少启动观测"},
		{"名称被他占", func(e *tenv, _ string, wn string) { e.rowSet(wn, "w0:p77", "term_w0:p77", "claude", "") }, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			e.config("normal")
			e.mustRun("up")
			wn, wp := e.str("roles", "W1", "name"), e.str("roles", "W1", "pane")
			c.tamper(e, wp, wn)
			out := e.mustFail("init", "--team", e.teamID())
			if c.want != "" {
				has(t, out, c.want, "拒绝提示")
			}
			eq(t, e.count("agent", "rename"), 0, "0 rename")
			eq(t, len(e.prompts(wn)), 0, "worker 0 prompt")
			eq(t, e.count("agent", "prompt"), 0, "0 prompt")
		})
	}
}

func TestInitAfterBridgeAutoName(t *testing.T) {
	for _, launcher := range []string{"cc/haiku", "pi/model-x:low", "ccenv/model-x"} {
		for _, timing := range []string{"before-capture", "after-capture"} {
			t.Run(launcher+"/"+timing, func(t *testing.T) {
				e := newEnv(t)
				e.config("normal")
				if launcher == "ccenv/model-x" {
					e.ccEnvPath = filepath.Join(e.root, "cc.env")
					if err := os.WriteFile(e.ccEnvPath, []byte("KEY=x\n"), 0o600); err != nil {
						t.Fatal(err)
					}
					e.config("ccenv")
				}
				if timing == "before-capture" {
					t.Setenv("NAKAMA_FAKE_MANUAL_NAME", "proj-e2d2")
				}
				e.mustRun("up", "--owner", "codex/gpt-6.1-sol:high", "--worker", launcher)
				wp := e.str("roles", "W1", "pane")
				eng, _ := parseEngine(launcher)
				// Bridge autoNameFor(".../proj", "term_w0:p11") 的独立固定样本。
				e.rowDel("@" + wp)
				e.rowSet("proj-e2d2", wp, "term_w0:p11", eng.kind(), "")
				e.mustRun("init", "--team", e.teamID())
				eq(t, e.str("phase"), "initialized", "自动命名不阻断初始化")
				eq(t, e.count("agent", "rename"), 1, "绑定角色名")
				e.mustRun("init", "--team", e.teamID())
				eq(t, e.count("agent", "prompt"), 2, "重入不重复投递")
			})
		}
	}
}

func TestRenameThenFingerprintDrift(t *testing.T) { // F12（F1 修复）：rename 后完整指纹必须仍等于启动观测
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	t.Setenv("NAKAMA_FAKE_RENAME_SESSION", "sess-after-rename")
	has(t, e.mustFail("init", "--team", e.teamID()), "指纹", "rename 后漂移提示")
	eq(t, e.count("agent", "prompt"), 0, "0 prompt")
	eq(t, e.count("agent", "rename"), 1, "rename 已发生无法撤回，接受 1 次")
	if dig(e.state(), "roles", "W1", "identity") != nil {
		t.Fatal("不得冻结新实例")
	}
}

func TestConcurrentInitSingleWinner(t *testing.T) { // F3
	e := newEnv(t)
	e.config("codex")
	e.mustRun("up")
	id := e.teamID()
	barrier := filepath.Join(e.root, "barrier")
	os.MkdirAll(barrier, 0o755)
	t.Setenv("NAKAMA_FAKE_GET_BARRIER", barrier)
	var wg sync.WaitGroup
	var ok1 bool
	wg.Add(1)
	go func() { defer wg.Done(); ok1, _ = e.run("init", "--team", id) }()
	waitFile(t, filepath.Join(barrier, "ready"))
	if ok, _ := e.run("init", "--team", id); ok {
		os.WriteFile(filepath.Join(barrier, "release"), nil, 0o644)
		wg.Wait()
		t.Fatal("并发第二个 init 应有界失败")
	}
	os.WriteFile(filepath.Join(barrier, "release"), nil, 0o644)
	wg.Wait()
	if !ok1 {
		t.Fatal("第一个 init 应成功")
	}
	eq(t, e.count("agent", "prompt"), 2, "每角色恰好一次 prompt")
}

func TestConcurrentFirstUpSingleWinner(t *testing.T) { // F13（F2 修复）：首次 up 在发布可发现状态前就持 team 锁
	e := newEnv(t)
	e.config("normal")
	barrier := filepath.Join(e.root, "barrier")
	os.MkdirAll(barrier, 0o755)
	t.Setenv("NAKAMA_FAKE_GET_BARRIER", barrier)
	var wg sync.WaitGroup
	var ok1 bool
	wg.Add(1)
	go func() { defer wg.Done(); ok1, _ = e.run("up") }()
	waitFile(t, filepath.Join(barrier, "ready"))
	if ok, _ := e.run("up"); ok {
		os.WriteFile(filepath.Join(barrier, "release"), nil, 0o644)
		wg.Wait()
		t.Fatal("并发第二个 up 应被拒绝")
	}
	os.WriteFile(filepath.Join(barrier, "release"), nil, 0o644)
	wg.Wait()
	if !ok1 {
		t.Fatal("第一个 up 应成功")
	}
	eq(t, e.count("pane", "split"), 2, "split 总数不重复")
	eq(t, e.count("agent", "start"), 1, "start 不重复")
	eq(t, e.count("pane", "run"), 1, "run 不重复")
}

func waitFile(t *testing.T, p string) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if _, err := os.Stat(p); err == nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("barrier 未就绪：%s", p)
}

func TestCcEnvQuotingAndGuards(t *testing.T) { // F4 / F11
	e := newEnv(t)
	e.ccEnvPath = filepath.Join(e.root, "cc env 'x' $(printf Y) & | dir.env")
	os.WriteFile(e.ccEnvPath, []byte("KEY=x\n"), 0o644)
	e.config("ccenv")
	e.mustRun("up")
	ownerRun := e.argvOf("pane", "run", "w0:p10")[3]
	has(t, ownerRun, "set -a; . "+posixQuote(e.ccEnvPath)+" || exit 1; set +a; cc --model", "env 路径单引号编码")
	hasNot(t, e.argvOf("pane", "run", "w0:p11")[3], e.ccEnvPath, "非 ccenv 角色不含 env 路径")

	e = newEnv(t)
	e.ccEnvPath = ""
	e.config("ccenv")
	e.mustFail("up")
	eq(t, e.count("pane", "split"), 0, "缺 env 零 split")

	e = newEnv(t)
	e.ccEnvPath = filepath.Join(e.root, "envdir")
	os.MkdirAll(e.ccEnvPath, 0o755)
	e.config("ccenv")
	has(t, e.mustFail("up"), "不是普通可读文件", "提示非普通文件")
	eq(t, e.count("pane", "split"), 0, "目录路径 0 split")
}

func TestCcEnvExecutedByTargetShell(t *testing.T) { // F11：pane 命令串经 bash 执行，source 失败不启动 cc
	runPane := func(t *testing.T, content string) (ran bool, ok bool) {
		e := newEnv(t)
		e.ccEnvPath = filepath.Join(e.root, "cc.env")
		os.WriteFile(e.ccEnvPath, []byte(content), 0o644)
		e.config("ccenv")
		e.mustRun("up")
		cmd := e.argvOf("pane", "run", "w0:p10")[3]
		bin := filepath.Join(e.root, "bin")
		os.MkdirAll(bin, 0o755)
		ccLog := filepath.Join(e.root, "cc.log")
		os.WriteFile(filepath.Join(bin, "cc"), []byte("#!/bin/sh\necho cc >> \""+ccLog+"\"\n"), 0o755)
		c := commandIn("bash", []string{"-c", cmd}, "PATH="+bin+":"+os.Getenv("PATH"))
		err := c.Run()
		_, statErr := os.Stat(ccLog)
		return statErr == nil, err == nil
	}
	if ran, ok := runPane(t, "return 1\n"); ran || ok {
		t.Fatalf("source 失败应非零且不启动 cc（ran=%v ok=%v）", ran, ok)
	}
	if ran, ok := runPane(t, "KEY=x\n"); !ran || !ok {
		t.Fatalf("正常 env 应成功并启动 cc（ran=%v ok=%v）", ran, ok)
	}
}

func TestProviderFromProfile(t *testing.T) { // F5
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	has(t, e.argvOf("pane", "run", "w0:p11")[3], "--provider 'demo-provider'", "provider 来自 profile")

	e = newEnv(t)
	e.rawConfig(`{ "default_profile": { "owner": "pi/model-x:low", "worker": "cc", "advisor": "none" }, "projects": {} }`)
	has(t, e.mustFail("up"), "pi_provider", "提示 provider")
	eq(t, e.count("pane", "split"), 0, "缺 provider 零 split")
}

func TestContextDriftRejected(t *testing.T) { // F6
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	f, _ := os.OpenFile(e.ctx, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("改动后的上下文\n")
	f.Close()
	has(t, e.mustFail("init", "--team", e.teamID()), "上下文文件已变化", "拒绝 context 漂移")
	eq(t, e.count("agent", "prompt"), 0, "漂移不 prompt")
}

func TestHalfTeamResume(t *testing.T) { // F7: 半组恢复补建缺失步骤
	e := newEnv(t)
	e.config("codex")
	e.mustRun("up")
	e.stateSet(func(m map[string]any) {
		m["phase"] = "starting"
		w := dig(m, "roles", "W1").(map[string]any)
		w["pane"], w["launched"], w["launch_identity"], w["identity"] = nil, false, nil, nil
	})
	before := e.count("pane", "split")
	out := e.mustRun("up")
	eq(t, e.count("pane", "split"), before+1, "只补建 Worker split")
	eq(t, e.str("phase"), "launched", "恢复后 phase=launched")
	hasNot(t, out, "复用", "不打印复用成功")
}

func TestHalfTeamOccupancy(t *testing.T) { // F7 余项：占用三态在 run/start 前拒绝并保留原始分类
	setup := func(t *testing.T, both bool) *tenv {
		e := newEnv(t)
		e.config("codex")
		e.mustRun("up")
		e.stateSet(func(m map[string]any) {
			m["phase"] = "starting"
			dig(m, "roles", "O1").(map[string]any)["launched"] = false
			if both {
				dig(m, "roles", "W1").(map[string]any)["launched"] = false
			}
		})
		return e
	}
	t.Run("已占用", func(t *testing.T) {
		e := setup(t, false)
		bs, br, bp := e.count("agent", "start"), e.count("pane", "run"), e.count("pane", "split")
		e.rowDel(e.str("roles", "O1", "name"))
		e.paneRowSet(e.str("roles", "O1", "pane"), "term_user", "claude", "")
		has(t, e.mustFail("up"), "occupied", "提示占用")
		eq(t, e.count("agent", "start"), bs, "0 agent start")
		eq(t, e.count("pane", "run"), br, "0 pane run")
		eq(t, e.count("pane", "split"), bp, "0 split")
	})
	t.Run("server_unavailable", func(t *testing.T) {
		e := setup(t, true)
		bs, br := e.count("agent", "start"), e.count("pane", "run")
		t.Setenv("NAKAMA_FAKE_GET_ERROR", "server_unavailable")
		out := e.mustFail("up")
		has(t, out, "unknown", "unknown 终止")
		has(t, out, "server_unavailable", "保留原始分类")
		eq(t, e.count("agent", "start"), bs, "0 start")
		eq(t, e.count("pane", "run"), br, "0 pane run")
	})
	t.Run("缺 terminal", func(t *testing.T) {
		e := setup(t, true)
		bs, br := e.count("agent", "start"), e.count("pane", "run")
		e.rowDel(e.str("roles", "O1", "name"))
		e.paneRowSet(e.str("roles", "O1", "pane"), "", "claude", "")
		out := e.mustFail("up")
		has(t, out, "unknown", "incomplete 终止")
		has(t, out, "incomplete_object", "分类 incomplete_object")
		eq(t, e.count("agent", "start"), bs, "0 start")
		eq(t, e.count("pane", "run"), br, "0 pane run")
	})
}

func TestStateValidationRejects(t *testing.T) { // F8
	e := newEnv(t)
	e.config("codex")
	e.mustRun("up")
	id := e.teamID()
	reject := func(msg string, mut func(m map[string]any), want string) {
		t.Helper()
		e.stateSet(mut)
		out := e.mustFail("status", "--team", id)
		if want != "" {
			has(t, out, want, msg)
		}
	}
	reject("schema 999", func(m map[string]any) { m["schema_version"] = 999 }, "校验失败")
	reject("pane=null", func(m map[string]any) {
		m["schema_version"] = 1
		dig(m, "roles", "O1").(map[string]any)["pane"] = nil
	}, "")
	reject("inited 无身份", func(m map[string]any) {
		o := dig(m, "roles", "O1").(map[string]any)
		o["pane"], o["inited"], o["identity"] = "w0:p10", true, nil
	}, "")
	reject("team_id 不一致", func(m map[string]any) {
		o := dig(m, "roles", "O1").(map[string]any)
		o["inited"] = false
		m["team_id"] = "mismatch"
	}, "不一致")
	reject("instance 1.5", func(m map[string]any) {
		m["team_id"] = id
		dig(m, "roles", "W1").(map[string]any)["instance"] = 1.5
	}, "校验失败")
	reject("角色名不匹配", func(m map[string]any) {
		w := dig(m, "roles", "W1").(map[string]any)
		w["instance"], w["name"] = 1, "wrong_role"
	}, "")
	e.stateSet(func(m map[string]any) { dig(m, "roles", "W1").(map[string]any)["name"] = id + "_execute" })
	b, _ := os.ReadFile(e.stateFile())
	foo := filepath.Join(filepath.Dir(e.stateFile()), "foo.json")
	os.WriteFile(foo, b, 0o600)
	has(t, e.mustFail("status", "--team", "foo"), "文件名与 team_id 不一致", "文件名拒绝")
}

func TestStateRequiresExplicitFields(t *testing.T) { // 缺必需字段/null 不得被 Go 零值当有效
	e := newEnv(t)
	e.config("codex")
	e.mustRun("up")
	id := e.teamID()
	for _, drop := range []string{"inited", "launched", "engine", "instance", "name"} {
		e.stateSet(func(m map[string]any) { delete(dig(m, "roles", "W1").(map[string]any), drop) })
		has(t, e.mustFail("status", "--team", id), "校验失败", "缺 "+drop)
		e.stateSet(func(m map[string]any) {}) // 无操作，保持文件
		e = newEnv(t)
		e.config("codex")
		e.mustRun("up")
		id = e.teamID()
	}
	e.stateSet(func(m map[string]any) { m["phase"] = nil })
	has(t, e.mustFail("status", "--team", id), "校验失败", "phase=null")
}

func TestStatusToleratesGetFailure(t *testing.T) { // F9
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	out := e.mustRun("status", "--team", e.teamID())
	has(t, out, "status=idle", "owner 状态")
	has(t, out, "status=unknown", "worker unknown")
}

func TestNeverTouchesUserPanes(t *testing.T) { // G
	e := newEnv(t)
	e.config("codex")
	os.WriteFile(e.names(), []byte("user_agent|w9:p9|term_w9:p9|codex|\n"), 0o644)
	e.mustRun("up")
	e.mustRun("init", "--team", e.teamID())
	eq(t, e.count("pane", "close"), 0, "不关闭任何 pane")
	eq(t, e.count("agent", "rename"), 0, "不改名既有 agent")
	b, _ := os.ReadFile(e.names())
	has(t, string(b), "user_agent|w9:p9|", "用户既有 agent 未被触碰")
}

func TestNativeIdentityFlow(t *testing.T) { // F2a / F2b native
	e := newEnv(t)
	e.config("codex")
	t.Setenv("NAKAMA_FAKE_GET_FAIL", "1")
	has(t, e.mustFail("up"), "无法取得完整身份", "提示取证失败")
	if dig(e.state(), "roles", "O1", "identity") != nil {
		t.Fatal("不得冻结 identity")
	}
	eq(t, e.str("pending", "delivery"), "UNKNOWN", "pending=UNKNOWN")

	e = newEnv(t)
	e.config("codex")
	t.Setenv("NAKAMA_FAKE_GET_FAIL", "")
	e.mustRun("up")
	on := e.str("roles", "O1", "name")
	e.stateSet(func(m map[string]any) { dig(m, "roles", "O1").(map[string]any)["identity"] = nil })
	has(t, e.mustFail("init", "--team", e.teamID()), "缺少冻结身份", "提示缺身份")
	eq(t, len(e.prompts(on)), 0, "Owner 不 prompt")
}

func TestAnchorChecks(t *testing.T) { // F2c
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	eq(t, e.str("anchor", "terminal_id"), "term_w0:p1", "anchor terminal 已记录")
	e.paneRowSet("w0:p1", "term_other", "", "")
	has(t, e.mustFail("init", "--team", e.teamID()), "anchor terminal_id 不一致", "terminal 不一致")
	eq(t, e.count("agent", "prompt"), 0, "不 prompt")

	e = newEnv(t)
	e.config("normal")
	t.Setenv("NAKAMA_FAKE_PANE_GET_FAIL", "1")
	has(t, e.mustFail("up"), "无法确定锚点 pane", "pane current 失败（up）")
	t.Setenv("NAKAMA_FAKE_PANE_GET_FAIL", "")
	e.mustRun("up")
	t.Setenv("NAKAMA_FAKE_PANE_GET_FAIL", "1")
	has(t, e.mustFail("init", "--team", e.teamID()), "无法读取 anchor pane", "pane get 失败")
	eq(t, e.count("agent", "prompt"), 0, "不 prompt")

	e = newEnv(t)
	e.config("normal")
	t.Setenv("NAKAMA_FAKE_PANE_GET_FAIL", "")
	os.WriteFile(e.names(), []byte("@w0:p1|w0:p1|term_w0:p1|claude|sess1\n"), 0o644)
	e.mustRun("up")
	eq(t, e.str("anchor", "identity", "session"), "sess1", "anchor identity 记录 session")
	e.mustRun("init", "--team", e.teamID())
	e.paneRowSet("w0:p1", "term_w0:p1", "claude", "sess2")
	has(t, e.mustFail("init", "--team", e.teamID()), "anchor 身份指纹不一致", "拒绝 session 变化")
}

func TestInitedDriftBecomesStale(t *testing.T) { // F2d
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	e.mustRun("init", "--team", e.teamID())
	before := e.count("agent", "prompt")
	e.rowSet(e.str("roles", "W1", "name"), e.str("roles", "W1", "pane"), "term_changed", "pi", "")
	out := e.mustFail("init", "--team", e.teamID())
	eq(t, e.count("agent", "prompt"), before, "不重发")
	hasNot(t, out, "READY", "不 READY")
	has(t, out, "stale", "标 stale")
}

func TestLockCleanupOnlyOwnPath(t *testing.T) { // F10
	e := newEnv(t)
	e.config("normal")
	victim, keep := filepath.Join(e.root, "victim"), filepath.Join(e.root, "keep")
	os.MkdirAll(filepath.Join(victim, "src"), 0o755)
	os.MkdirAll(keep, 0o755)
	os.WriteFile(filepath.Join(victim, "keep.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(keep, "keep.txt"), []byte("x"), 0o644)
	space := filepath.Join(e.root, "state victim extra")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", space)
	e.mustRun("up")
	for _, p := range []string{filepath.Join(victim, "keep.txt"), filepath.Join(keep, "keep.txt")} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("邻近目录被误删：%s", p)
		}
	}
	left, _ := filepath.Glob(filepath.Join(space, "locks", "*.lock"))
	eq(t, len(left), 0, "锁目录已清理")
}

func TestDelayedRecognitionFirstFreeze(t *testing.T) { // F2b：延迟识别
	e := newEnv(t)
	e.config("piowner")
	t.Setenv("NAKAMA_FAKE_GET_DELAY_N", "3")
	e.mustRun("up")
	e.mustRun("init", "--team", e.teamID())
	eq(t, e.str("roles", "O1", "identity", "terminal_id"), "term_w0:p10", "延迟后冻结")
	eq(t, e.str("roles", "O1", "identity", "kind"), "pi", "kind=pi")
}

func TestAgentPaneBusyBoundedRetry(t *testing.T) { // 仅启动前 agent_pane_busy 有界重试
	e := newEnv(t)
	e.config("codex")
	t.Setenv("FAKE_BUSY_ATTEMPTS", "2")
	e.mustRun("up")
	eq(t, e.count("agent", "start"), 4, "owner 重试 2 次后成功 + worker 1 次 = 4")

	e = newEnv(t)
	e.config("codex")
	t.Setenv("FAKE_BUSY_ATTEMPTS", "99")
	e.mustFail("up")
	eq(t, e.count("agent", "start"), 6, "最多 6 次")
}

func TestStatePermissions(t *testing.T) { // 目录 0700 / 文件 0600，且显式收敛既有宽松目录
	e := newEnv(t)
	e.config("codex")
	state := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	os.MkdirAll(filepath.Join(state, "teams"), 0o755)
	os.Chmod(filepath.Join(state, "teams"), 0o755)
	e.mustRun("up")
	st, _ := os.Stat(filepath.Join(state, "teams"))
	eq(t, st.Mode().Perm(), os.FileMode(0o700), "teams 目录 0700")
	f, _ := os.Stat(e.stateFile())
	eq(t, f.Mode().Perm(), os.FileMode(0o600), "状态文件 0600")
	ents, _ := os.ReadDir(filepath.Join(state, "teams"))
	for _, en := range ents {
		hasNot(t, en.Name(), ".tmp.", "无残留临时文件")
	}
}

func TestPromptAssemblyShape(t *testing.T) { // prompts 正文与组装顺序
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up", "--advisor", "cc")
	e.mustRun("init", "--team", e.teamID())
	op := e.prompts(e.str("roles", "O1", "name"))[0]
	has(t, op, "消息实例：Owner=O1；Worker=W1；Advisor=A1", "映射行")
	pi, ci, ri := strings.Index(op, "项目上下文（来源："), strings.Index(op, "协议"), strings.Index(op, "设计 Owner")
	if !(pi >= 0 && pi < ri) || ci < 0 {
		t.Fatalf("顺序应为 映射 → 上下文 → protocol → 角色模板：%d %d %d", pi, ci, ri)
	}
	has(t, op, "当前有一个或多个独立 advisor 联合设计", "advisor 规则块")
	hasNot(t, op, "当前没有独立 advisor", "无 advisor 块不出现")
	hasNot(t, op, "<!--", "标记行不输出")
	if strings.HasSuffix(op, "\n") {
		t.Fatal("尾部换行应被去掉")
	}
	// init 顺序 worker → advisor → owner：Advisor 初始化时 Owner 尚未 inited，不带 mid-task 块。
	hasNot(t, e.prompts(e.str("roles", "A1", "name"))[0], "任务已在进行中加入", "Owner 未 inited 时无 mid-task 块")
	a := &app{stdout: &syncBuf{}, stderr: &syncBuf{}, root: e.repoRoot, st: newStore()}
	tm, err := a.st.load(e.teamID())
	if err != nil {
		t.Fatal(err)
	}
	tm.owner().Inited = true
	late, err := a.assemblePrompt(tm, "A1")
	if err != nil {
		t.Fatal(err)
	}
	has(t, late, "任务已在进行中加入", "Owner 已 inited 时 Advisor 带 mid-task 块")
}

// 最后一个副作用（Owner prompt）在途时收到信号：结果照常落盘，但命令以非零退出并提示被中断。
func TestSignalAfterLastSideEffectStillNonZero(t *testing.T) {
	e := newEnv(t)
	e.config("codex")
	e.mustRun("up")
	barrier := filepath.Join(e.root, "prompt-barrier")
	os.MkdirAll(barrier, 0o755)
	t.Setenv("NAKAMA_FAKE_PROMPT_BARRIER", barrier)
	t.Setenv("NAKAMA_FAKE_PROMPT_BARRIER_NAME", e.str("roles", "O1", "name"))
	exe, _ := os.Executable()
	cmd := exec.Command(exe, "--hr-run-main", "init", "--team", e.teamID())
	cmd.Env = append(os.Environ(), "HERDR_PLUGIN_ROOT="+e.repoRoot)
	var out syncBuf
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitFile(t, filepath.Join(barrier, "ready"))
	cmd.Process.Signal(syscall.SIGTERM)
	os.WriteFile(filepath.Join(barrier, "release"), nil, 0o644)
	var ee *exec.ExitError
	if err := cmd.Wait(); !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("应以 1 退出：%v\n%s", err, out.String())
	}
	has(t, out.String(), "被信号中断：已完成的结果已落盘", "中断提示")
	eq(t, e.str("phase"), "initialized", "状态已落盘")
}

func TestSessionBackfillOnReinit(t *testing.T) { // 真实 Herdr：codex 的 session 首次 prompt 后才出现
	setup := func(t *testing.T) *tenv {
		e := newEnv(t)
		e.config("codex")
		e.mustRun("up")
		e.mustRun("init", "--team", e.teamID())
		return e
	}
	owner := func(e *tenv, term, kind, sess string) {
		e.rowSet(e.str("roles", "O1", "name"), e.str("roles", "O1", "pane"), term, kind, sess)
	}
	t.Run("空补全为非空，之后变化即漂移", func(t *testing.T) {
		e := setup(t)
		prompts := e.count("agent", "prompt")
		owner(e, e.str("roles", "O1", "identity", "terminal_id"), "codex", "sess-1")
		e.mustRun("init", "--team", e.teamID())
		eq(t, e.str("roles", "O1", "identity", "session"), "sess-1", "session 已补全")
		eq(t, e.str("roles", "O1", "stale"), "<nil>", "不标 stale")
		eq(t, e.count("agent", "prompt"), prompts, "无重发")
		owner(e, e.str("roles", "O1", "identity", "terminal_id"), "codex", "sess-2")
		has(t, e.mustFail("init", "--team", e.teamID()), "stale", "补全后变化即漂移")
	})
	// terminal 变化且 pane/kind 相同：是 rebind 候选（重启后的常见情形）。init 只提示并拒绝，不标 stale，
	// 否则 stale 永不自动清除，会永久阻断 rebind。
	t.Run("terminal 变化为 rebind 候选：拒绝、不标 stale", func(t *testing.T) {
		e := setup(t)
		owner(e, "term_changed", "codex", "sess-1")
		has(t, e.mustFail("init", "--team", e.teamID()), "rebind", "提示显式 rebind")
		eq(t, e.str("roles", "O1", "stale"), "<nil>", "不标 stale")
		eq(t, e.str("roles", "O1", "identity", "session"), "", "不写入 session")
	})
	t.Run("kind 变化仍 stale", func(t *testing.T) {
		e := setup(t)
		owner(e, e.str("roles", "O1", "identity", "terminal_id"), "claude", "sess-1")
		has(t, e.mustFail("init", "--team", e.teamID()), "stale", "拒绝补全")
		eq(t, e.str("roles", "O1", "identity", "session"), "", "不写入 session")
	})
}

func TestStaleIsNeverAutoCleared(t *testing.T) { // 已有 stale：无论补全条件成立还是身份严格匹配都保持
	for name, tamper := range map[string]func(e *tenv){
		"session 空→非空": func(e *tenv) {
			e.rowSet(e.str("roles", "O1", "name"), e.str("roles", "O1", "pane"), e.str("roles", "O1", "identity", "terminal_id"), "codex", "sess-1")
		},
		"身份严格匹配": func(e *tenv) {},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.config("codex")
			e.mustRun("up")
			e.mustRun("init", "--team", e.teamID())
			e.stateSet(func(m map[string]any) { dig(m, "roles", "O1").(map[string]any)["stale"] = true })
			tamper(e)
			prompts := e.count("agent", "prompt")
			has(t, e.mustFail("init", "--team", e.teamID()), "stale", "保持 stale 并报错")
			eq(t, e.str("roles", "O1", "stale"), "true", "stale 保持")
			eq(t, e.str("roles", "O1", "identity", "session"), "", "identity 不变")
			eq(t, e.count("agent", "prompt"), prompts, "无新 prompt")
		})
	}
}

func TestOutputNeverEchoesProvider(t *testing.T) { // pi_provider 值只进 pane 命令文本，不进任何 stdout/stderr（会被 action log 持久化）
	e := newEnv(t)
	e.config("normal")
	out := e.mustRun("up") + e.mustRun("init", "--team", e.teamID()) + e.mustRun("status")
	hasNot(t, out, "demo-provider", "输出不含 provider")
	has(t, e.argvOf("pane", "run", "w0:p11")[3], "demo-provider", "启动命令仍带 provider")
}

// 真实 plugin.action.invoke 回包里的 context（与注入 HERDR_PLUGIN_CONTEXT_JSON 同值）：
// focused_pane_id 优先于 HERDR_PANE_ID，并经 pane get 核对目标与 cwd。
func TestAnchorFromRealInvokeContext(t *testing.T) {
	e := newEnv(t)
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", string(fixture(t, "plugin_context_invoke.json")))
	t.Setenv("HERDR_PANE_ID", "w9:p9")
	a := &app{stdout: &syncBuf{}, stderr: &syncBuf{}, root: e.repoRoot, st: newStore()}
	if err := a.normalizeAnchor("", ""); err != nil {
		t.Fatal(err)
	}
	eq(t, a.anchor.PaneID, "w1P:p2", "focused_pane_id 优先")
	want, _ := filepath.EvalSymlinks(e.proj)
	eq(t, a.anchor.CWD, want, "cwd 取自 pane get")
	if e.argvOf("pane", "get", "w1P:p2") == nil || e.argvOf("pane", "get", "w9:p9") != nil {
		t.Fatalf("应只对 focused_pane_id 做 pane get：%v", e.argvs())
	}
}

func TestAuthorizationWaitAfterStartNotReady(t *testing.T) { // B1 / 裁决 1：agent_not_ready 视为已启动，blocked 期间等待；codex 真实信任提示未复现，仅假 herdr 覆盖
	e := newEnv(t)
	e.config("codex")
	t.Setenv("NAKAMA_FAKE_START_NOT_READY", "1")
	t.Setenv("NAKAMA_FAKE_STATUS_SEQ", "blocked,blocked,blocked,idle")
	has(t, e.mustRun("up"), "等待授权", "提示等待授权")
	eq(t, e.count("agent", "start"), 2, "owner/worker 各启动一次，agent_not_ready 不重试")
	eq(t, e.str("roles", "O1", "launch_identity", "kind"), "codex", "授权结束后取得启动观测")
	eq(t, e.str("roles", "O1", "identity", "kind"), "codex", "native 身份落盘")
}

func TestAuthorizationWaitBounded(t *testing.T) { // B1：授权等待有上限，超时按失败路径处理并说明是授权超时
	e := newEnv(t)
	e.config("codex")
	t.Setenv("NAKAMA_FAKE_START_NOT_READY", "1")
	t.Setenv("NAKAMA_FAKE_STATUS_SEQ", "blocked")
	t.Setenv("NAKAMA_START_WAIT_SECONDS", "1")
	has(t, e.mustFail("up"), "等待授权超时", "超时信息说明是授权等待")
}

func TestCcSessionWaitSkipsBlockedWindow(t *testing.T) { // B1 cc / 裁决 3：blocked 期间不计入 NAKAMA_START_POLL_TRIES 窗口，授权后仍能取得 session
	e := newEnv(t)
	e.rawConfig(`{ "default_profile": { "owner": "cc", "worker": "codex/gpt-6.1-luna:medium", "advisor": "none" }, "projects": {} }`)
	t.Setenv("NAKAMA_START_POLL_TRIES", "3")
	t.Setenv("NAKAMA_FAKE_STATUS_SEQ", "unknown,blocked,blocked,blocked,blocked,blocked,idle")
	t.Setenv("NAKAMA_FAKE_LATE_SESSION", "sess-late")
	e.mustRun("up")
	eq(t, e.str("roles", "O1", "launch_identity", "session"), "sess-late", "授权后取得启动观测中的 session")
}

func TestAgentBlockedPromptKeepsRetryable(t *testing.T) { // B2：agent_blocked 确证未送达，清 pending，处理对话框后可重试
	e := newEnv(t)
	e.config("codex")
	e.mustRun("up")
	id := e.teamID()
	t.Setenv("FAKE_PROMPT_ERROR", "agent_blocked")
	has(t, e.mustFail("init", "--team", id), "仍在等待授权", "提示处理后重按 init")
	eq(t, e.str("pending"), "<nil>", "pending 已清空")
	t.Setenv("FAKE_PROMPT_ERROR", "")
	e.mustRun("init", "--team", id)
	eq(t, e.str("roles", "O1", "inited"), "true", "重试后 owner 初始化成功，未被 pendingGuard 拦截")
}

func TestInitFindsTeamFromRolePane(t *testing.T) { // B3：焦点在角色 pane 时 init 找到所属编组；多个编组命中须 --team
	e := newEnv(t)
	e.config("codex")
	e.mustRun("up")
	worker := e.str("roles", "W1", "pane")
	e.mustRun("init", "--pane", worker)
	eq(t, e.str("phase"), "initialized", "角色 pane 上 init 完成")

	raw, err := os.ReadFile(e.stateFile())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(e.stateFile()), "dup-team.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	has(t, e.mustFail("init", "--pane", worker), "同时属于多个编组", "多命中拒绝并要求 --team")
}

func TestNativeDelayedBlocked(t *testing.T) { // R1：native 的首次有效观测之后才出现 blocked，也必须等授权，不能冻结
	e := newEnv(t)
	e.config("codex")
	t.Setenv("NAKAMA_FAKE_START_NOT_READY", "1")
	t.Setenv("NAKAMA_START_WAIT_SECONDS", "1")
	t.Setenv("NAKAMA_FAKE_STATUS_SEQ", "unknown,blocked")
	e.run("up")
	eq(t, e.count("agent", "start"), 1, "owner 仍 blocked 时不得完成取证并开始下一个角色")
	eq(t, e.str("roles", "O1", "launch_identity"), "<nil>", "未等到授权前不写 launch_identity")
}

func TestCcFirstObservationBlocked(t *testing.T) { // R1：手动引擎首次观测即 blocked 且 POLL_TRIES=1 时仍须等授权，不得冻结空 session
	e := newEnv(t)
	e.rawConfig(`{"default_profile":{"owner":"cc","worker":"codex/gpt-6.1-sol:high","advisor":"none"},"projects":{}}`)
	t.Setenv("NAKAMA_START_POLL_TRIES", "1")
	t.Setenv("NAKAMA_FAKE_STATUS_SEQ", "unknown,blocked,idle")
	t.Setenv("NAKAMA_FAKE_LATE_SESSION", "sess-after-authorization")
	e.mustRun("up")
	eq(t, e.str("roles", "O1", "launch_identity", "session"), "sess-after-authorization", "首次身份观测即 blocked 也必须等授权，不得冻结空 session")
}

func TestSharedAuthorizationDeadline(t *testing.T) { // R2：同一角色的授权预算只有一份；中间的 unknown 不重置预算，提示只出现一次
	e := newEnv(t)
	e.rawConfig(`{"default_profile":{"owner":"cc","worker":"codex/gpt-6.1-sol:high","advisor":"none"},"projects":{}}`)
	t.Setenv("NAKAMA_START_WAIT_SECONDS", "1")
	t.Setenv("NAKAMA_FAKE_STATUS_SEQ", "blocked,blocked,blocked,blocked,unknown,blocked,blocked,blocked,blocked,idle")
	t.Setenv("NAKAMA_FAKE_LATE_SESSION", "sess-after-authorization")
	var out syncBuf
	a := &app{stdout: &out, stderr: &out, root: e.repoRoot, st: newStore(), poll: 200 * time.Millisecond}
	defer a.unlockAll()
	start := time.Now()
	err := a.cmdUp(nil)
	elapsed := time.Since(start)
	if elapsed > 1400*time.Millisecond {
		t.Fatalf("NAKAMA_START_WAIT_SECONDS=1 却重置授权预算，耗时 %s", elapsed)
	}
	if err == nil {
		t.Fatalf("授权预算耗尽后应失败，输出：\n%s", out.String())
	}
	eq(t, strings.Count(out.String(), "请到该 pane 查看"), 1, "同一角色只提示一次等待")
}

func TestStaleCallerDoesNotInitOtherRoles(t *testing.T) { // R3：调用者角色实例已被替换时，自动定位必须在任何 prompt 前拒绝
	e := newEnv(t)
	e.config("codex")
	e.mustRun("up")
	ownerName := e.str("roles", "O1", "name")
	ownerPane := e.str("roles", "O1", "pane")
	rows := readRows(e.log + ".names")
	for i := range rows {
		if rows[i].key == ownerName {
			rows[i].sess = "replacement-agent-session"
		}
	}
	writeRows(e.log+".names", rows)
	e.mustFail("init", "--pane", ownerPane)
	eq(t, e.count("agent", "prompt"), 0, "调用者已非该编组角色，自动定位必须在任何 prompt 前拒绝")
	eq(t, e.str("roles", "W1", "inited"), "false", "其他角色未被初始化")
}

func TestCcBlockedPromptRetryNoDuplicateRename(t *testing.T) { // 控制：manual 角色已冻结后 agent_blocked 重试不得再次 rename
	e := newEnv(t)
	e.rawConfig(`{"default_profile":{"owner":"cc","worker":"cc","advisor":"none"},"projects":{}}`)
	e.mustRun("up")
	id := e.teamID()
	worker := e.str("roles", "W1", "name")
	t.Setenv("FAKE_PROMPT_ERROR", "agent_blocked")
	e.mustFail("init", "--team", id)
	eq(t, e.str("pending"), "<nil>", "确定未发送时清 pending")
	t.Setenv("FAKE_PROMPT_ERROR", "")
	e.mustRun("init", "--team", id)
	n := 0
	for _, v := range e.argvs() {
		if len(v) > 3 && v[0] == "agent" && v[1] == "rename" && v[3] == worker {
			n++
		}
	}
	eq(t, n, 1, "已冻结 identity 的 worker 重试不能再次 rename")
	eq(t, e.str("phase"), "initialized", "稳定身份下重试完成")
}

func TestNotReadyWithoutReadableIdentity(t *testing.T) { // 控制：agent_not_ready 后无法取证时 fail closed，不冻结、不发 prompt
	e := newEnv(t)
	e.config("codex")
	t.Setenv("NAKAMA_FAKE_START_NOT_READY", "1")
	t.Setenv("NAKAMA_FAKE_GET_FAIL", "1")
	e.mustFail("up")
	eq(t, e.str("pending", "delivery"), "UNKNOWN", "启动错误但无法取证时 fail closed")
	eq(t, e.str("roles", "O1", "identity"), "<nil>", "不冻结无证据身份")
	eq(t, e.count("agent", "prompt"), 0, "不误发 prompt")
}

func TestSessionWindowAfterAuthorization(t *testing.T) { // T1：session 轮询循环内的授权等待：blocked 读数不消耗窗口，迟到 session 仍被取到
	for _, middle := range []bool{false, true} {
		t.Run(fmt.Sprint(middle), func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "herdr")
			transition := ""
			if middle {
				transition = `if [ "$n" -ge 2 ] && [ "$n" -le 5 ]; then status=blocked; fi; if [ "$n" -ge 7 ]; then sess='{"value":"late"}'; fi`
			}
			script := `#!/bin/sh
n=$(cat "$0.count" 2>/dev/null || echo 0); n=$((n+1)); echo "$n" > "$0.count"
status=idle; sess=null
` + transition + `
printf '{"result":{"agent":{"name":"","pane_id":"w0:p2","terminal_id":"term_p2","agent":"claude","agent_status":"%s","agent_session":%s}}}\n' "$status" "$sess"
`
			if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HERDR_BIN_PATH", bin)
			t.Setenv("NAKAMA_START_POLL_TRIES", "3")
			var out syncBuf
			a := &app{stdout: &out, stderr: &out, st: store{dir: dir}}
			tm := &Team{TeamID: "t1", Roles: map[string]*Role{"W1": {Name: "x_execute", Engine: "cc", Pane: "w0:p2"}}}
			if !a.captureIdentity(tm, "W1") {
				t.Fatal(out.String())
			}
			wantSession, wantReads := "", "3"
			if middle {
				wantSession, wantReads = "late", "7"
			}
			eq(t, tm.role("W1").LaunchIdentity.Session, wantSession, "只计算稳定观测；blocked 读数不消耗 session 窗口")
			raw, _ := os.ReadFile(bin + ".count")
			eq(t, strings.TrimSpace(string(raw)), wantReads, "观测次数")
		})
	}
}
