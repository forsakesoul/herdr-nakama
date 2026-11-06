package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// 配置：CLI > default_profile > 报错。不内置任何个人 provider/路径/凭据；projects 只引用上下文文件路径。
// 只解析不执行。
type config struct{ root map[string]any }

func configFile() string {
	if d := os.Getenv("HERDR_PLUGIN_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "config.json")
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "nakama", "config.json")
}

// 文件不存在视为空配置；存在但不可读/非法 JSON 则报错（不静默当作缺配置）。
func loadConfig() (*config, error) {
	b, err := os.ReadFile(configFile())
	if errors.Is(err, os.ErrNotExist) {
		return &config{root: map[string]any{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf(L("无法读取配置 %s：%v"), configFile(), err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil || m == nil {
		return nil, fmt.Errorf(L("配置 %s 不是合法 JSON 对象"), configFile())
	}
	return &config{root: m}, nil
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func asStr(v any) string         { s, _ := v.(string); return s }

func (c *config) profileStr(key string) string {
	return asStr(asMap(c.root["default_profile"])[key])
}
func (c *config) piProvider() string { return c.profileStr("pi_provider") }
func (c *config) ccEnvFile() string  { return c.profileStr("cc_env_file") }

// 引擎解析：简写经 engine_map 展开（值可为字符串或按角色的对象），否则视作完整引擎串。
func (c *config) resolveEngine(input, role string) string {
	switch v := asMap(asMap(c.root["default_profile"])["engine_map"])[input].(type) {
	case string:
		return v
	case map[string]any:
		return asStr(v[role])
	}
	return input
}

// 取某角色引擎：CLI > default_profile.<role> > 报错。
func (c *config) pickEngine(role, cli string) (string, error) {
	cand := cli
	if cand == "" {
		cand = c.profileStr(role)
	}
	if cand == "" {
		return "", fmt.Errorf(L("缺少 %s 引擎配置：请在 config.json 的 default_profile.%s 填写，或用 CLI 参数指定。不内置个人 provider。"), role, role)
	}
	return c.resolveEngine(cand, role), nil
}

type project struct{ short, contextFile, trigger string }

func (c *config) project(repo string) (project, bool) {
	e := asMap(asMap(c.root["projects"])[repo])
	if e == nil {
		return project{}, false
	}
	return project{short: asStr(e["prefix"]), contextFile: asStr(e["context_file"]), trigger: asStr(e["trigger"])}, true
}
