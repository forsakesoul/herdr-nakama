//go:build !darwin

package main

// 插件清单只声明 macOS；其他平台不提供交互选择。
func realStdinIsTTY() bool { return false }
