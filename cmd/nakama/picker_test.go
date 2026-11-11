package main

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func (e *tenv) anchorEnv(t *testing.T) {
	t.Setenv("NAKAMA_ANCHOR_PANE", "w0:p1")
	t.Setenv("NAKAMA_ANCHOR_CWD", e.proj)
	t.Setenv("HERDR_PANE_ID", "w9:p9") // overlay 自身；绝不能被当作锚点
}

func withTTY(t *testing.T, tty bool) {
	old := stdinIsTTY
	stdinIsTTY = func() bool { return tty }
	t.Cleanup(func() { stdinIsTTY = old })
}

// 以给定输入在进程内运行 pick（假 herdr 仍是子进程）。
func (e *tenv) pick(in string) (bool, string) {
	var buf syncBuf
	a := &app{stdout: &buf, stderr: &buf, root: e.repoRoot, st: newStore(), in: bufio.NewReader(strings.NewReader(in))}
	defer a.unlockAll()
	return a.cmdPick(nil) == nil, buf.String()
}

func (e *tenv) noSideEffects(t *testing.T) {
	t.Helper()
	eq(t, e.count("pane", "split"), 0, "0 split")
	eq(t, e.count("agent", "start"), 0, "0 start")
	eq(t, e.count("pane", "run"), 0, "0 run")
	ents, _ := os.ReadDir(filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "teams"))
	eq(t, len(ents), 0, "无 Team 文件")
}

func TestOpenPicker(t *testing.T) { // a
	e := newEnv(t)
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", string(fixture(t, "plugin_context_invoke.json")))
	a := &app{stdout: &syncBuf{}, stderr: &syncBuf{}, root: e.repoRoot, st: newStore()}
	if err := a.cmdOpenPicker(nil); err != nil {
		t.Fatal(err)
	}
	var open []string
	for _, v := range e.argvs() {
		if len(v) > 2 && v[0] == "plugin" && v[1] == "pane" && v[2] == "open" {
			open = v
		}
	}
	if open == nil {
		t.Fatal("应调用 plugin pane open")
	}
	line := strings.Join(open, " ")
	for _, bad := range []string{"--target-pane", "--workspace", "--direction"} {
		hasNot(t, line, bad, "overlay 不得带")
	}
	cwd, _ := filepath.EvalSymlinks(e.proj)
	has(t, line, "--placement overlay", "placement")
	has(t, line, "--entrypoint picker", "entrypoint")
	has(t, line, "--env NAKAMA_ANCHOR_PANE=w1P:p2", "锚点 pane 来自 context")
	has(t, line, "--env NAKAMA_ANCHOR_CWD="+cwd, "锚点 cwd")

	// 锚点核验失败 → 不 open
	e2 := newEnv(t)
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", string(fixture(t, "plugin_context_invoke.json")))
	t.Setenv("NAKAMA_FAKE_PANE_GET_FAIL", "1")
	b := &app{stdout: &syncBuf{}, stderr: &syncBuf{}, root: e2.repoRoot, st: newStore()}
	if b.cmdOpenPicker(nil) == nil {
		t.Fatal("核验失败应拒绝")
	}
	eq(t, e2.count("plugin", "pane"), 0, "不调用 open")

	// open 失败 → 非零，不重试
	t.Setenv("NAKAMA_FAKE_PANE_GET_FAIL", "")
	t.Setenv("NAKAMA_FAKE_PLUGIN_OPEN_FAIL", "1")
	c := &app{stdout: &syncBuf{}, stderr: &syncBuf{}, root: e2.repoRoot, st: newStore()}
	if c.cmdOpenPicker(nil) == nil {
		t.Fatal("open 失败应非零")
	}
	eq(t, e2.count("plugin", "pane"), 1, "不重试")
}

func TestPickCreatesTeamFromPickedEngines(t *testing.T) { // b
	e := newEnv(t)
	e.config("normal")
	e.anchorEnv(t)
	withTTY(t, true)
	ok, out := e.pick("\n\n\n\ny\n") // 三次默认：sol / ds(按角色展开) / none
	if !ok {
		t.Fatalf("应成功：\n%s", out)
	}
	has(t, out, "Owner   codex/gpt-6.1-sol:high", "摘要")
	has(t, out, "Worker  pi/model-x:low", "摘要按角色展开对象别名")
	eq(t, e.str("roles", "O1", "engine"), "codex/gpt-6.1-sol:high", "Owner 引擎=摘要")
	eq(t, e.str("roles", "W1", "engine"), "pi/model-x:low", "Worker 引擎=摘要")
	eq(t, e.str("anchor", "pane_id"), "w0:p1", "锚点来自 env 而非 HERDR_PANE_ID")
	eq(t, e.argvOf("pane", "split", "--pane")[3], "w0:p1", "split 到锚点")
	for _, v := range e.argvs() {
		for _, x := range v {
			if x == "w9:p9" {
				t.Fatalf("不得触碰 overlay 自身 pane：%v", v)
			}
		}
	}
	hasNot(t, out, "demo-provider", "不显示 provider")

	e = newEnv(t)
	e.config("normal")
	e.anchorEnv(t)
	ok, out = e.pick("codex/gpt-6.1-luna:medium\ncodex/gpt-6.1-sol\n\nnone\nyes\n")
	if !ok {
		t.Fatalf("完整写法应成功：\n%s", out)
	}
	eq(t, e.str("roles", "O1", "engine"), "codex/gpt-6.1-luna:medium", "完整写法 Owner")
	eq(t, e.str("roles", "W1", "engine"), "codex/gpt-6.1-sol", "完整写法 Worker")
	eq(t, e.str("roles", "A1"), "<nil>", "Advisor none")
}

func TestPickRepromptsOnInvalid(t *testing.T) { // c
	e := newEnv(t)
	e.config("normal")
	e.anchorEnv(t)
	withTTY(t, true)
	ok, out := e.pick("bogus\npi/model with space\nnone\n\n\n\n\ny\n")
	if !ok {
		t.Fatalf("重问后应成功：\n%s", out)
	}
	has(t, out, "无法识别的引擎：bogus", "未知简写")
	has(t, out, "无法识别的引擎：pi/model with space", "坏格式")
	has(t, out, "Owner 不能是 none", "Owner 拒绝 none")
	eq(t, e.count("pane", "split"), 2, "最终只建一次组")

	e = newEnv(t)
	e.config("normal")
	e.anchorEnv(t)
	ok, _ = e.pick("bogus\n") // 确认前输入结束
	eq(t, ok, false, "EOF 应非零")
	e.noSideEffects(t)
}

func TestPickCancelAndRejections(t *testing.T) { // d
	cases := map[string]struct {
		in  string
		tty bool
		pre func(e *tenv, t *testing.T)
	}{
		"确认答 n":       {"\n\n\n\nn\n", true, nil},
		"确认为空(默认 N)":  {"\n\n\n\n\n", true, nil},
		"确认前 EOF":     {"\n\n\n\n", true, nil},
		"非 TTY":       {"\n\n\n\ny\n", false, nil},
		"锚点 env 缺失":   {"\n\n\n\ny\n", true, func(e *tenv, t *testing.T) { t.Setenv("NAKAMA_ANCHOR_PANE", "") }},
		"pane get 失败": {"\n\n\n\ny\n", true, func(e *tenv, t *testing.T) { t.Setenv("NAKAMA_FAKE_PANE_GET_FAIL", "1") }},
		"cwd 漂移":      {"\n\n\n\ny\n", true, func(e *tenv, t *testing.T) { t.Setenv("NAKAMA_ANCHOR_CWD", filepath.Join(e.root, "elsewhere")) }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.config("normal")
			e.anchorEnv(t)
			withTTY(t, c.tty)
			if c.pre != nil {
				c.pre(e, t)
			}
			e.pick(c.in)
			e.noSideEffects(t)
		})
	}
}

func TestPickExistingTeamShowsFrozenEngines(t *testing.T) { // e
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	splits := e.count("pane", "split")
	e.anchorEnv(t)
	withTTY(t, true)
	ok, out := e.pick("\n")
	if !ok {
		t.Fatalf("应正常退出：\n%s", out)
	}
	has(t, out, e.teamID(), "显示现有组")
	has(t, out, "codex/gpt-6.1-sol:high", "显示冻结引擎")
	has(t, out, "选择 [回车关闭]", "进入管理菜单")
	hasNot(t, out, "引擎 [默认", "不再询问")
	eq(t, e.count("pane", "split"), splits, "零新增副作用")
}

// f：结果页等回车时锁已释放、状态已落盘（成功与失败各一）。
func TestPickReleasesLocksBeforeResultPage(t *testing.T) {
	for name, failSplit := range map[string]bool{"成功": false, "失败": true} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.config("normal")
			e.anchorEnv(t)
			withTTY(t, true)
			if failSplit {
				t.Setenv("NAKAMA_FAKE_SPLIT_FAIL_AT", "1")
			}
			pr, pw := io.Pipe()
			var buf syncBuf
			a := &app{stdout: &buf, stderr: &buf, root: e.repoRoot, st: newStore(), in: bufio.NewReader(pr)}
			done := make(chan error, 1)
			go func() { done <- a.cmdPick(nil) }()
			io.WriteString(pw, "\n\n\n\ny\n")
			deadline := time.Now().Add(20 * time.Second)
			for !strings.Contains(buf.String(), "按回车关闭") {
				if time.Now().After(deadline) {
					t.Fatalf("未到结果页：\n%s", buf.String())
				}
				time.Sleep(20 * time.Millisecond)
			}
			locks, _ := filepath.Glob(filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "locks", "*.lock"))
			eq(t, len(locks), 0, "等待回车时锁已释放")
			if _, err := os.Stat(e.stateFile()); err != nil {
				t.Fatal("状态应已落盘")
			}
			io.WriteString(pw, "\n")
			err := <-done
			eq(t, err != nil, failSplit, "返回值反映 cmdUp 结果")
		})
	}
}

// g：输入阶段对真实入口发信号 → 及时退出、零副作用。
func TestPickSignalDuringInput(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			e := newEnv(t)
			e.config("normal")
			e.anchorEnv(t)
			exe, _ := os.Executable()
			cmd := exec.Command(exe, "--hr-run-main", "pick")
			cmd.Env = append(os.Environ(), "HERDR_PLUGIN_ROOT="+e.repoRoot, "NAKAMA_TEST_TTY=1")
			in, _ := cmd.StdinPipe()
			defer in.Close()
			out, _ := cmd.StdoutPipe()
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			rd := bufio.NewReader(out)
			for { // 等到首个提示出现，确保已阻塞在读入上
				line, err := rd.ReadString(':')
				if err != nil {
					t.Fatalf("未见提示：%v", err)
				}
				if strings.Contains(line, "Owner 引擎") {
					break
				}
			}
			cmd.Process.Signal(sig)
			waited := make(chan error, 1)
			go func() { waited <- cmd.Wait() }()
			select {
			case err := <-waited:
				if err == nil {
					t.Fatal("应非零退出")
				}
			case <-time.After(5 * time.Second):
				cmd.Process.Kill()
				t.Fatal("输入阶段收到信号应立即退出")
			}
			e.noSideEffects(t)
		})
	}
}

// F1①：执行期间收到信号 → 在途 split 落盘后非零退出，不停在结果页等输入，锁已释放。
func TestPickSignalDuringExecutionExitsWithoutResultPage(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	e.anchorEnv(t)
	barrier := filepath.Join(e.root, "split-barrier")
	os.MkdirAll(barrier, 0o755)
	t.Setenv("NAKAMA_FAKE_SPLIT_BARRIER", barrier)
	exe, _ := os.Executable()
	cmd := exec.Command(exe, "--hr-run-main", "pick")
	cmd.Env = append(os.Environ(), "HERDR_PLUGIN_ROOT="+e.repoRoot, "NAKAMA_TEST_TTY=1")
	in, _ := cmd.StdinPipe()
	defer in.Close() // 保持打开：若进程错误地停在结果页，会卡住而不是读到 EOF
	var out syncBuf
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	io.WriteString(in, "\n\n\n\ny\n")
	waitFile(t, filepath.Join(barrier, "ready"))
	cmd.Process.Signal(syscall.SIGTERM)
	os.WriteFile(filepath.Join(barrier, "release"), nil, 0o644)
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case err := <-waited:
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 1 {
			t.Fatalf("应以 1 退出：%v\n%s", err, out.String())
		}
	case <-time.After(15 * time.Second):
		cmd.Process.Kill()
		t.Fatalf("不应停在结果页等待输入：\n%s", out.String())
	}
	has(t, out.String(), "被信号中断：已完成的结果已落盘", "中断提示")
	hasNot(t, out.String(), "按回车关闭", "不进入结果页")
	locks, _ := filepath.Glob(filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "locks", "*.lock"))
	eq(t, len(locks), 0, "锁已释放")
	eq(t, e.str("roles", "O1", "pane"), "w0:p10", "在途 split 已落盘")
	eq(t, e.count("agent", "start"), 0, "之后无 start")
}

// F1②：信号恰落在 cmdUp 返回与锁释放之间 → 锁仍被释放，不遗留。
func TestPickSignalInLockReleaseWindow(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	e.anchorEnv(t)
	withTTY(t, true)
	var buf syncBuf
	a := &app{stdout: &buf, stderr: &buf, root: e.repoRoot, st: newStore(), in: bufio.NewReader(strings.NewReader("\n\n\n\ny\n"))}
	a.beforeUnlock = func() {
		syscall.Kill(os.Getpid(), syscall.SIGTERM)
		for i := 0; i < 200 && !a.stop.Load(); i++ {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if a.cmdPick(nil) == nil {
		t.Fatal("被信号中断应非零")
	}
	locks, _ := filepath.Glob(filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "locks", "*.lock"))
	eq(t, len(locks), 0, "锁已释放")
	has(t, buf.String(), "被信号中断", "提示")
	hasNot(t, buf.String(), "按回车关闭", "不进入结果页")
}

// F2：确认期间同 anchor 出现 starting 半组 → picker 不恢复、不接管。
func TestPickRefusesTeamCreatedDuringConfirm(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	e.anchorEnv(t)
	withTTY(t, true)
	pr, pw := io.Pipe()
	var buf syncBuf
	a := &app{stdout: &buf, stderr: &buf, root: e.repoRoot, st: newStore(), in: bufio.NewReader(pr)}
	done := make(chan error, 1)
	go func() { done <- a.cmdPick(nil) }()
	io.WriteString(pw, "\n\n\n\n")
	for i := 0; i < 500 && !strings.Contains(buf.String(), "确认建组？"); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	e.mustRun("up", "--pane", "w0:p1")
	e.stateSet(func(m map[string]any) {
		m["phase"] = "starting"
		w := dig(m, "roles", "W1").(map[string]any)
		w["pane"], w["launched"], w["launch_identity"], w["identity"] = nil, false, nil, nil
	})
	before, _ := os.ReadFile(e.stateFile())
	splits, starts := e.count("pane", "split"), e.count("agent", "start")
	io.WriteString(pw, "y\n")
	io.WriteString(pw, "\n") // 拒绝后同样停在结果页等回车
	if err := <-done; err == nil {
		t.Fatal("应拒绝")
	}
	after, _ := os.ReadFile(e.stateFile())
	eq(t, string(after), string(before), "状态未变")
	eq(t, e.count("pane", "split"), splits, "零新 split")
	eq(t, e.count("agent", "start"), starts, "零新 start")
	has(t, buf.String(), "不恢复/不换模型", "拒绝提示")
	hasNot(t, buf.String(), "建组完成", "不得报成功")
}

// F3：EOF 前的半截输入不算确认。
func TestPickPartialLineIsCancel(t *testing.T) {
	for _, in := range []string{"\n\n\n\ny", "\n\n\n\nyes"} {
		e := newEnv(t)
		e.config("normal")
		e.anchorEnv(t)
		withTTY(t, true)
		ok, _ := e.pick(in)
		eq(t, ok, false, "半截确认应取消："+in)
		e.noSideEffects(t)
	}
}

// F4：/dev/zero 是字符设备但不是终端。
func TestRealStdinIsTTYRejectsCharDevice(t *testing.T) {
	f, err := os.Open("/dev/zero")
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	old := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = old }()
	eq(t, realStdinIsTTY(), false, "/dev/zero 不是 TTY")
}

// F3：确认答 n 必须到达确认摘要并正常取消（返回 nil、输出“已取消”），零副作用。
func TestPickDeclineReachesConfirmAndCancels(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	e.anchorEnv(t)
	withTTY(t, true)
	ok, out := e.pick("\n\n\n\nn\n")
	if !ok {
		t.Fatalf("确认答 n 应正常取消（返回 nil）：\n%s", out)
	}
	has(t, out, "确认建组？", "到达确认摘要")
	has(t, out, "已取消", "输出取消")
	e.noSideEffects(t)
}
