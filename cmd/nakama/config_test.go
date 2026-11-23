package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExampleClaudePreset 直接读取发布示例，避免角色预设与实际启动命令漂移。
func TestExampleClaudePreset(t *testing.T) {
	b, err := os.ReadFile("../../config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	c := &config{}
	if err := json.Unmarshal(b, &c.root); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ role, input, command string }{
		{"owner", "claude", "cc --model 'claude-opus-5-5' --effort 'medium'"},
		{"worker", "claude", "cc --model 'haiku' --effort 'medium'"},
		{"advisor", "claude", "cc --model 'claude-opus-5-5' --effort 'medium'"},
		{"worker", "cc/sonnet:medium", "cc --model 'sonnet' --effort 'medium'"},
	} {
		s, err := c.pickEngine(tc.role, tc.input)
		if err != nil {
			t.Fatal(err)
		}
		g, ok := parseEngine(s)
		if !ok {
			t.Fatalf("invalid engine for %s: %q", tc.role, s)
		}
		eq(t, g.paneCommand("", ""), tc.command, tc.role+"/"+tc.input)
	}
}

// 配置与状态目录：Herdr 注入的目录优先，其次 XDG，最后 HOME 下的默认位置；改名后目录名固定为 nakama。
func TestConfigAndStatePaths(t *testing.T) {
	home := t.TempDir()
	for _, k := range []string{"HERDR_PLUGIN_CONFIG_DIR", "XDG_CONFIG_HOME", "HERDR_PLUGIN_STATE_DIR", "XDG_STATE_HOME"} {
		t.Setenv(k, "")
	}
	t.Setenv("HOME", home)
	eq(t, configFile(), filepath.Join(home, ".config", "nakama", "config.local.json"), "缺省配置路径")
	eq(t, newStore().dir, filepath.Join(home, ".local", "state", "nakama"), "缺省状态目录")

	t.Setenv("XDG_CONFIG_HOME", "/xdg/config")
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	eq(t, configFile(), "/xdg/config/nakama/config.local.json", "XDG_CONFIG_HOME")
	eq(t, newStore().dir, "/xdg/state/nakama", "XDG_STATE_HOME")

	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "/herdr/config")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "/herdr/state")
	eq(t, configFile(), "/herdr/config/config.local.json", "Herdr 注入的配置目录优先于 XDG")
	eq(t, newStore().dir, "/herdr/state", "Herdr 注入的状态目录优先于 XDG")
}

// 缺文件视为空配置；文件存在但不可读或不是 JSON 对象时必须报错，不能静默当作缺配置。
func TestLoadConfigErrors(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", dir)
	file := filepath.Join(dir, "config.local.json")

	c, err := loadConfig()
	if err != nil || len(c.root) != 0 {
		t.Fatalf("缺文件应为空配置：%v %v", c, err)
	}
	legacyFile := filepath.Join(dir, "config.json")
	if err := os.WriteFile(legacyFile, []byte(`{"default_profile":{"owner":"codex/old:high"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err = loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	eq(t, c.profileStr("owner"), "codex/old:high", "本地文件缺失时读取旧配置")

	for name, body := range map[string]string{"非法 JSON": "{", "数组": "[]", "null": "null", "字符串": `"x"`} {
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), file) {
			t.Errorf("%s 应报告本地配置错误：%v", name, err)
		}
	}
	if err := os.WriteFile(file, []byte(`{"default_profile":{"owner":"codex/new:high"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err = loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	eq(t, c.profileStr("owner"), "codex/new:high", "本地配置优先")

	os.Remove(file)
	os.Mkdir(file, 0o700) // 目录：存在但读不了
	if _, err := loadConfig(); err == nil {
		t.Error("不可读的配置应报错")
	}
}

// 引擎取值：CLI > default_profile.<role>；简写按 engine_map 展开，值为对象时按角色取。
func TestPickEngine(t *testing.T) {
	c := &config{root: map[string]any{"default_profile": map[string]any{
		"owner":  "sol",
		"worker": "ds",
		"engine_map": map[string]any{
			"sol": "codex/gpt-6.2-sol:high",
			"ds":  map[string]any{"owner": "pi/m:low", "worker": "pi/m:high"},
		},
	}}}
	for _, tc := range []struct{ role, cli, want string }{
		{"owner", "", "codex/gpt-6.2-sol:high"},
		{"worker", "", "pi/m:high"},
		{"owner", "ds", "pi/m:low"},
		{"worker", "cc/haiku", "cc/haiku"},
		{"advisor", "ds", ""}, // 对象里没有该角色：展开为空串，由 parseEngine 拒绝
	} {
		got, err := c.pickEngine(tc.role, tc.cli)
		if err != nil {
			t.Fatalf("%s/%q: %v", tc.role, tc.cli, err)
		}
		eq(t, got, tc.want, tc.role+"/"+tc.cli)
	}
	if _, err := c.pickEngine("advisor", ""); err == nil {
		t.Error("CLI 与 profile 都缺失时应报错，不内置默认引擎")
	}
}
