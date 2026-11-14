package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strconv"
)

// 所有 Herdr 调用走 HERDR_BIN_PATH（缺省 herdr），argv 逐参数直传，不经 shell。
type result struct {
	stdout, stderr []byte
	code           int // 0=成功；-1=无法执行
}

func (r result) ok() bool { return r.code == 0 }

func herdrBin() string {
	if b := os.Getenv("HERDR_BIN_PATH"); b != "" {
		return b
	}
	return "herdr"
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func (a *app) herdr(args ...string) result {
	var so, se bytes.Buffer
	c := exec.Command(herdrBin(), args...)
	c.Stdout, c.Stderr = &so, &se
	code := exitCode(c.Run()) // 先执行再取缓冲区
	return result{so.Bytes(), se.Bytes(), code}
}

// herdrCombined 合并 stdout/stderr（占用判定需要原始错误分类，无论输出落在哪个流）。
func (a *app) herdrCombined(args ...string) result {
	var b bytes.Buffer
	c := exec.Command(herdrBin(), args...)
	c.Stdout, c.Stderr = &b, &b
	code := exitCode(c.Run())
	return result{b.Bytes(), nil, code}
}

// herdrPass 的 stdout 直通用户终端，stderr 捕获后回显（调用方还要读错误码）。
func (a *app) herdrPass(args ...string) result {
	var se bytes.Buffer
	c := exec.Command(herdrBin(), args...)
	c.Stdout, c.Stderr = a.stdout, &se
	code := exitCode(c.Run())
	r := result{nil, se.Bytes(), code}
	a.stderr.Write(r.stderr)
	return r
}

func errorCode(b []byte) string {
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &e) != nil {
		return ""
	}
	return e.Error.Code
}

type agentObj struct {
	Name         string `json:"name"`
	PaneID       string `json:"pane_id"`
	TerminalID   string `json:"terminal_id"`
	Agent        string `json:"agent"`
	AgentSession *struct {
		Value string `json:"value"`
	} `json:"agent_session"`
	AgentStatus string `json:"agent_status"`
}

func (o agentObj) ident() Ident {
	i := Ident{TerminalID: o.TerminalID, PaneID: o.PaneID, Kind: o.Agent}
	if o.AgentSession != nil {
		i.Session = o.AgentSession.Value
	}
	return i
}

func parseAgent(b []byte) (agentObj, bool) {
	var r struct {
		Result struct {
			Agent *agentObj `json:"agent"`
		} `json:"result"`
	}
	if json.Unmarshal(b, &r) != nil || r.Result.Agent == nil {
		return agentObj{}, false
	}
	return *r.Result.Agent, true
}

type paneObj struct {
	PaneID      string `json:"pane_id"`
	CWD         string `json:"cwd"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	TerminalID  string `json:"terminal_id"`
}

func parsePane(b []byte) paneObj {
	var r struct {
		Result struct {
			Pane paneObj `json:"pane"`
		} `json:"result"`
	}
	_ = json.Unmarshal(b, &r)
	return r.Result.Pane
}

// 单次 agent get；失败（非零/无 agent 对象）返回 false。
func (a *app) agentGet(target string) (agentObj, bool) {
	r := a.herdr("agent", "get", target)
	if !r.ok() {
		return agentObj{}, false
	}
	return parseAgent(r.stdout)
}

// 有界轮询：Herdr 识别手动启动的 agent 有延迟，次数由 NAKAMA_START_POLL_TRIES 控制（缺省 6）。
func (a *app) observeAgent(target string) (agentObj, bool) {
	tries := pollTries()
	for i := 0; i < tries; i++ {
		if o, ok := a.agentGet(target); ok {
			return o, true
		}
		if i < tries-1 {
			sleepFor(a.poll)
		}
	}
	return agentObj{}, false
}

// 手动启动器刚 pane run 时，Herdr 要等 CLI 起来才识别得到（Claude Code 冷启动可超过 6 秒）：给 5 倍窗口。
func (a *app) observeLaunch(target string) (agentObj, bool) {
	for i := 0; i < pollTries()*5 && !a.stop.Load(); i++ {
		if o, ok := a.agentGet(target); ok {
			return o, true
		}
		sleepFor(a.poll)
	}
	return agentObj{}, false
}

// ponytail: 按屏幕文字认 Codex 的启动对话框（Herdr 把它们报成 idle）；文案随 Codex 版本变，Herdr 能报 blocked 后删掉。
// 真机见过的页脚：enter continue · esc quit / enter confirm · esc skip / enter review · esc close。
var dialogFooter = regexp.MustCompile(`enter [a-z]+ · esc [a-z]+`)

func (a *app) startupDialog(pane string) bool {
	return dialogFooter.Match(a.herdr("pane", "read", pane, "--source", "visible").stdout)
}

func pollTries() int {
	if n, err := strconv.Atoi(os.Getenv("NAKAMA_START_POLL_TRIES")); err == nil && n > 0 {
		return n
	}
	return 6
}

// pane 占用三态：idle（确证 agent_not_found）/ occupied / unknown（get 失败、对象缺失或不完整）。
// code 保留原始分类（agent_not_found、server_unavailable、invalid_response、incomplete_object…）。
func (a *app) paneOccupancy(pane string) (state, code string) {
	r := a.herdrCombined("agent", "get", pane)
	if !r.ok() {
		code = errorCode(r.stdout)
		if code == "" {
			code = "invalid_response"
		}
		if code == "agent_not_found" {
			return "idle", code
		}
		return "unknown", code
	}
	o, ok := parseAgent(r.stdout)
	if !ok {
		return "unknown", "invalid_response"
	}
	if !o.ident().complete() {
		return "unknown", "incomplete_object"
	}
	return "occupied", ""
}
