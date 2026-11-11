// nakama：Owner/Worker/Advisor 编组的 Herdr 原生插件（up / init / status / rebind / pick / add / drop / swap / clean）。
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type app struct {
	stdout, stderr io.Writer
	root           string // 插件根：prompts/ 所在目录
	st             store
	locks          lockSet
	stop           atomic.Bool   // 收到信号后置位
	poll           time.Duration // 轮询间隔；测试置 0
	in             *bufio.Reader // pick 的交互输入
	createOnly     bool          // 内部：picker 只新建，锚点已有任何组都拒绝
	beforeUnlock   func()        // 测试钩子：pick 在释放锁前调用

	// 测试钩子（nil 时无作用）：rebind 在对应检查点调用，用于注入漂移、中断与 Bridge 竞争。
	hookBeforeRename func(role string)
	hookAfterRename  func(role string)
	hookBeforeCommit func()
	// 测试钩子：split 结果（pane ID）已落盘但尚未核验时调用；返回 true 模拟在该窗口中断。
	hookAfterSplitPersist func(pane string) bool

	// 单次命令的上下文
	anchor     Anchor
	piProvider string
	ccEnvFile  string
}

// errExit：信息已在产生处输出，调用方只需转成非零退出码。
var errExit = errors.New("exit")

func (a *app) fail(format string, v ...any) error {
	fmt.Fprintf(a.stderr, "ERROR: "+format+"\n", v...)
	return errExit
}
func (a *app) errf(format string, v ...any) { fmt.Fprintf(a.stderr, "ERROR: "+format+"\n", v...) }
func (a *app) warnf(format string, v ...any) {
	fmt.Fprintf(a.stderr, "WARN: "+format+"\n", v...)
}

// 每个副作用（split/pane run/start/rename/prompt）开始前调用。
func (a *app) interrupted() bool {
	if a.stop.Load() {
		a.errf(L("被信号中断；不再开始新的副作用。"))
		return true
	}
	return false
}
func (a *app) outf(format string, v ...any) { fmt.Fprintf(a.stdout, format, v...) }

// 插件根：二进制位于 <root>/bin/ 下；否则回落 Herdr 注入的 HERDR_PLUGIN_ROOT。
func pluginRoot() string {
	if exe, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			root := filepath.Dir(filepath.Dir(r))
			if _, err := os.Stat(filepath.Join(root, "prompts", "en", "protocol.md")); err == nil {
				return root
			}
		}
	}
	return os.Getenv("HERDR_PLUGIN_ROOT")
}

const usageText = `nakama — Herdr 原生编组插件（Owner / Worker / Advisor）

用法：
  nakama up [--owner E] [--worker E] [--workers N] [--advisor E|none] [--advisors N] [--pane P] [--cwd C]
  nakama init --team <id> [--pane P] [--cwd C]
  nakama status [--team <id>]
  nakama rebind --team <id> [--preflight | --discard-pending]
                       [--confirm-session <成员键 O1|W1|A1|anchor>=<session>]...
                       [--confirm-anchor <terminal>]   # 电脑/Herdr 重启后显式重绑，见 README
  nakama pick                   # 交互选择引擎与人数并建组；已有编组时为管理菜单（overlay 内运行，经 pick action 打开）
  nakama add --team <id> [--role worker|advisor] [--engine E]          # 中途加入 Worker 或 Advisor
  nakama drop --team <id> [--role worker|advisor] [--instance N]       # 移除 Worker 或 Advisor
  nakama swap --team <id> --role worker|advisor [--instance N] [--engine E]  # 替换角色实例（旧 pane 保留）
  nakama clean [--team <id>] [--task <task_id>]... [--all] [--yes] [--force]  # 列出 / 清理任务目录

引擎写法：<launcher>/<model>[:effort]，或 config.json 中 default_profile.engine_map 的简写。
配置：$HERDR_PLUGIN_CONFIG_DIR/config.json，或 ${XDG_CONFIG_HOME:-$HOME/.config}/nakama/config.json
`

func (a *app) dispatch(args []string) error {
	cmd := ""
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "up":
		return a.cmdUp(args)
	case "init":
		return a.cmdInit(args)
	case "status":
		return a.cmdStatus(args)
	case "rebind":
		return a.cmdRebind(args)
	case "open-picker":
		return a.cmdOpenPicker(args)
	case "pick":
		return a.cmdPick(args)
	case "add":
		return a.cmdAdd(args)
	case "drop":
		return a.cmdDrop(args)
	case "swap":
		return a.cmdSwap(args)
	case "clean":
		return a.cmdClean(args)
	case "", "-h", "--help", "help":
		io.WriteString(a.stdout, L(usageText))
		return nil
	}
	io.WriteString(a.stderr, L(usageText))
	return a.fail(L("未知子命令：%s"), cmd)
}

// 信号只置停止标记：在途 herdr 子进程照常 Wait 并把结果落盘，下一个副作用开始前才检查标记。
// 全程唯一 os.Exit 在 main()，持锁路径一律经 run() 的 defer 释放；SIGKILL 不可处理，
// 遗留锁按「不自动接管」fail-closed。
func run(args []string) int {
	a := &app{stdout: os.Stdout, stderr: os.Stderr, root: pluginRoot(), st: newStore(), poll: time.Second, in: bufio.NewReader(os.Stdin)}
	defer a.unlockAll()
	// pick 的输入阶段不持锁、无副作用，让默认信号处理直接终止；执行阶段由 pick 自己启用。
	if len(args) == 0 || args[0] != "pick" {
		defer a.watchSignals()()
	}
	if err := a.dispatch(args); err != nil {
		return 1
	}
	// 最后一个副作用恰在信号后完成：结果已落盘不回滚，但命令按被中断处理。
	if a.stop.Load() {
		a.errf(L("被信号中断：已完成的结果已落盘。"))
		return 1
	}
	return 0
}

// 信号只置停止标记；返回的函数恢复默认信号处理（幂等），并同步消费已排队的信号，
// 所以返回后读 a.stop 不会漏掉停止前到达的信号。
func (a *app) watchSignals() func() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	done, exited := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(exited)
		select {
		case <-sig:
			a.stop.Store(true)
		case <-done:
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			signal.Stop(sig)
			close(done)
			<-exited
			select {
			case <-sig:
				a.stop.Store(true)
			default:
			}
		})
	}
}

func main() { os.Exit(run(os.Args[1:])) }
