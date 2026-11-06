package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// mkdir 锁：不用 flock，因为进程退出自动释放会破坏「遗留锁不自动接管」。
// 锁序固定 anchor → team。只释放本进程实际创建的完整路径。
type lockSet struct{ dirs []string }

func (a *app) lock(key, op string) error {
	if err := mkdirSecure(a.st.locksDir()); err != nil {
		return a.fail(L("无法创建锁目录 %s：%v"), a.st.locksDir(), err)
	}
	d := filepath.Join(a.st.locksDir(), safeName(key)+".lock")
	if err := os.Mkdir(d, 0o700); err != nil {
		return a.fail(L("无法获取锁 %s：另一进程可能正在操作，或存在遗留锁（不自动接管）。请人工核对后删除该目录。"), d)
	}
	_ = os.WriteFile(filepath.Join(d, "owner"), []byte(fmt.Sprintf("%d %s\n", os.Getpid(), op)), 0o600)
	a.locks.dirs = append(a.locks.dirs, d)
	return nil
}

func (a *app) unlockAll() {
	for _, d := range a.locks.dirs {
		os.RemoveAll(d)
	}
	a.locks.dirs = nil
}
