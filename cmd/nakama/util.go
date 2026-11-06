package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// 文件名安全化（team id / 锁 key）：逐字节替换，与旧 `tr -c 'A-Za-z0-9._-' '_'` 一致。
func safeName(s string) string {
	b := []byte(s)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			b[i] = '_'
		}
	}
	return string(b)
}

// POSIX 单引号编码：结果可安全作为单个 shell 词（pane 命令串会进入目标 shell）。
func posixQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Herdr agent 名称规则：小写字母开头，仅小写字母/数字/-/_，1-32 字符。整串校验（正则锚定整串，换行不会被绕过）。
var nameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

func nameValid(s string) bool { return nameRe.MatchString(s) }

// 项目短名边界：^[a-z][a-z0-9]{0,7}$。
var prefixRe = regexp.MustCompile(`^[a-z][a-z0-9]{0,7}$`)

func prefixValid(s string) bool { return prefixRe.MatchString(s) }

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// 物理路径；不存在或不是目录时回落原串（与旧 `cd -- p && pwd -P || p` 一致）。
func realPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	r, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return p
	}
	if st, err := os.Stat(r); err != nil || !st.IsDir() {
		return p
	}
	return r
}

// 可读普通文件（不读内容）。
func readableFile(p string) bool {
	st, err := os.Stat(p)
	if err != nil || !st.Mode().IsRegular() {
		return false
	}
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

func sleepFor(d time.Duration) {
	if d > 0 {
		time.Sleep(d)
	}
}
