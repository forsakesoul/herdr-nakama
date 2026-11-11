package main

import "testing"

// h3：中途加入的 Worker 带 mid-task 块；初始 up 的 Worker 不带。
func TestMidTaskBlockOnlyForLateJoiningWorker(t *testing.T) {
	e := newEnv(t)
	e.config("normal")
	e.mustRun("up")
	id := e.teamID()
	e.mustRun("init", "--team", id)
	hasNot(t, e.prompts(id + "_execute")[0], "任务已在进行中加入", "初始 Worker 不带 mid-task 块")
	e.mustRun("add", "--team", id, "--role", "worker")
	e.mustRun("init", "--team", id)
	has(t, e.lastPrompt(id+"_execute2"), "任务已在进行中加入", "中途加入的 Worker 带 mid-task 块")
}
