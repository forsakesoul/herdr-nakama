package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type tarEntry struct {
	name, body string
	link       bool
}

func makePkg(t *testing.T, ents []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range ents {
		h := &tar.Header{Name: e.name, Mode: 0o755, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if e.link {
			h.Typeflag, h.Linkname, h.Size = tar.TypeSymlink, "/etc/passwd", 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if !e.link {
			tw.Write([]byte(e.body))
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// 假发布站：latest 跳转到 tag，download 提供安装包与校验和。sums 为空时按包内容计算。
func releaseServer(t *testing.T, tag string, pkg []byte, sums string) {
	t.Helper()
	name := "nakama-" + runtime.GOOS + "-" + runtime.GOARCH + ".tar.gz"
	if sums == "" {
		sums = sum(pkg) + "  " + name + "\n"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/"+tag, http.StatusFound)
	})
	mux.HandleFunc("/releases/download/"+tag+"/SHA256SUMS", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(sums)) })
	mux.HandleFunc("/releases/download/"+tag+"/"+name, func(w http.ResponseWriter, _ *http.Request) { w.Write(pkg) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("NAKAMA_RELEASE_BASE_URL", srv.URL)
}

// 发布包形态的插件根：没有 .git。
func pkgRoot(t *testing.T, e *tenv, version string) string {
	t.Helper()
	root := filepath.Join(e.root, "plugin")
	os.MkdirAll(filepath.Join(root, "bin"), 0o755)
	os.MkdirAll(filepath.Join(root, "prompts", "zh"), 0o755)
	os.WriteFile(filepath.Join(root, "herdr-plugin.toml"), []byte("id = \"x\"\nversion = \""+version+"\"\n"), 0o644)
	os.WriteFile(filepath.Join(root, "bin", "nakama"), []byte("old"), 0o755)
	os.WriteFile(filepath.Join(root, "prompts", "zh", "protocol.md"), []byte("old protocol\n"), 0o644)
	os.WriteFile(filepath.Join(root, "keep.txt"), []byte("mine"), 0o644)
	os.WriteFile(filepath.Join(root, "bin", "helper.sh"), []byte("mine"), 0o755)
	os.WriteFile(filepath.Join(root, "prompts", "zh", "custom.md"), []byte("mine"), 0o644)
	return root
}

func runAt(root, in string, args ...string) (bool, string) {
	var buf syncBuf
	a := &app{stdout: &buf, stderr: &buf, root: root, st: newStore(), in: bufio.NewReader(strings.NewReader(in))}
	defer a.unlockAll()
	return a.dispatch(args) == nil, buf.String()
}

func goodPkg(t *testing.T) []byte {
	return makePkg(t, []tarEntry{
		{name: "nakama/herdr-plugin.toml", body: "id = \"x\"\nversion = \"0.0.9\"\n"},
		{name: "nakama/bin/nakama", body: "new"},
		{name: "nakama/prompts/zh/protocol.md", body: "new protocol\n"},
	})
}

func TestVersionAndCheck(t *testing.T) {
	e := newEnv(t)
	root := pkgRoot(t, e, "0.0.4")
	releaseServer(t, "v0.0.9", nil, "x")
	ok, out := runAt(root, "", "version")
	eq(t, ok, true, "version")
	eq(t, out, "nakama 0.0.4\n", "只输出本地版本，不联网")
	_, out = runAt(root, "", "version", "--check")
	has(t, out, "有新版本 v0.0.9", "提示新版本")
	releaseServer(t, "v0.0.4", nil, "x")
	_, out = runAt(root, "", "version", "--check")
	has(t, out, "已是最新版本", "相同版本")
	eq(t, newer("v0.0.10", "0.0.9"), true, "按数字比较而非字典序")
	eq(t, newer("v0.0.4", "0.1.0"), false, "本地更新时不提示")
	t.Setenv("NAKAMA_RELEASE_BASE_URL", "http://127.0.0.1:1")
	ok, out = runAt(root, "", "version", "--check")
	eq(t, ok, false, "查询失败非零")
	has(t, out, "nakama 0.0.4", "仍输出本地版本")
}

func TestUpdateReplacesReleasePackage(t *testing.T) {
	e := newEnv(t)
	root := pkgRoot(t, e, "0.0.4")
	releaseServer(t, "v0.0.9", goodPkg(t), "")
	read := func(p string) string { b, _ := os.ReadFile(filepath.Join(root, p)); return string(b) }

	_, out := runAt(root, "", "update", "--check")
	has(t, out, "有新版本 v0.0.9（当前 0.0.4）", "--check 只报告")
	eq(t, read("bin/nakama"), "old", "--check 不改动")
	ok, out := runAt(root, "", "update")
	eq(t, ok, false, "非交互须 --yes")
	eq(t, read("bin/nakama"), "old", "未确认不改动")

	ok, out = runAt(root, "", "update", "--yes")
	if !ok {
		t.Fatalf("update 应成功：\n%s", out)
	}
	has(t, out, "已升级到 v0.0.9", "成功")
	has(t, out, "共享协议有变化", "协议变化时提醒重建编组")
	eq(t, read("bin/nakama"), "new", "二进制已替换")
	eq(t, read("prompts/zh/protocol.md"), "new protocol\n", "提示词已替换")
	eq(t, read("keep.txt"), "mine", "包外文件保留")
	eq(t, read("bin/helper.sh")+read("prompts/zh/custom.md"), "minemine", "包内目录下的包外文件也保留")
	ents, _ := os.ReadDir(e.root)
	for _, x := range ents {
		if strings.HasPrefix(x.Name(), ".nakama-update-") {
			t.Fatalf("暂存目录未清理：%s", x.Name())
		}
	}
	_, out = runAt(root, "", "update", "--yes")
	has(t, out, "已是最新版本", "再次运行为空操作")
}

func TestUpdateRefusals(t *testing.T) {
	e := newEnv(t)
	root := pkgRoot(t, e, "0.0.4")
	read := func() string { b, _ := os.ReadFile(filepath.Join(root, "bin", "nakama")); return string(b) }

	releaseServer(t, "v0.0.9", goodPkg(t), strings.Repeat("0", 64)+"  nakama-"+runtime.GOOS+"-"+runtime.GOARCH+".tar.gz\n")
	_, out := runAt(root, "", "update", "--yes")
	has(t, out, "SHA-256", "校验和不符")

	for name, ents := range map[string][]tarEntry{
		"越出包目录": {{name: "nakama/../evil", body: "x"}},
		"包目录之外": {{name: "other/bin/nakama", body: "x"}},
		"符号链接":  {{name: "nakama/bin/nakama", link: true}},
		"不完整":   {{name: "nakama/bin/nakama", body: "x"}},
	} {
		releaseServer(t, "v0.0.9", makePkg(t, ents), "")
		ok, out := runAt(root, "", "update", "--yes")
		if ok {
			t.Fatalf("%s 应失败：\n%s", name, out)
		}
		has(t, out, "升级失败", name)
	}
	eq(t, read(), "old", "以上失败均未改动插件")
	if _, err := os.Stat(filepath.Join(e.root, "evil")); err == nil {
		t.Fatal("路径穿越写出了文件")
	}

	// 源码检出不代为改动。
	releaseServer(t, "v0.0.9", goodPkg(t), "")
	os.Mkdir(filepath.Join(root, ".git"), 0o755)
	_, out = runAt(root, "", "update", "--yes")
	has(t, out, "源码检出", "拒绝并给出做法")
	eq(t, read(), "old", "未改动")
}

func TestUpdateHandsGitHubInstallToHerdr(t *testing.T) {
	e := newEnv(t)
	root := pkgRoot(t, e, "0.0.4")
	releaseServer(t, "v0.0.9", goodPkg(t), "")
	t.Setenv("FAKE_PLUGIN_SOURCE", "github")
	ok, out := runAt(root, "", "update")
	eq(t, ok, false, "未确认不委托")
	has(t, out, "需要 --yes", "同一条确认规则")
	eq(t, e.count("plugin", "update"), 0, "未调用 herdr plugin update")
	t.Setenv("FAKE_PLUGIN_UPDATE_FAIL", "1")
	_, out = runAt(root, "", "update", "--yes")
	has(t, out, "升级 Herdr", "旧版 Herdr 不支持该子命令时给出出路")
	t.Setenv("FAKE_PLUGIN_UPDATE_FAIL", "")
	if ok, out = runAt(root, "", "update", "--yes"); !ok {
		t.Fatalf("应成功：\n%s", out)
	}
	eq(t, strings.Join(e.argvOf("plugin", "update", pluginID), " "), "plugin update "+pluginID+" --yes", "交给 herdr plugin update")
	b, _ := os.ReadFile(filepath.Join(root, "bin", "nakama"))
	eq(t, string(b), "old", "自己不替换文件")
}

func TestUpdateRerunCompletesPartialFailure(t *testing.T) {
	e := newEnv(t)
	root := pkgRoot(t, e, "0.0.4")
	releaseServer(t, "v0.0.9", goodPkg(t), "")
	// 提示词位置被目录占住：换入失败。
	proto := filepath.Join(root, "prompts", "zh", "protocol.md")
	os.Remove(proto)
	os.Mkdir(proto, 0o755)
	os.WriteFile(filepath.Join(proto, "x"), nil, 0o644)
	ok, out := runAt(root, "", "update", "--yes")
	eq(t, ok, false, "中途失败")
	has(t, out, "升级失败", "报告失败")
	eq(t, (&app{root: root}).localVersion(), "0.0.4", "清单未提前换成新版本")
	os.RemoveAll(proto)
	if ok, out = runAt(root, "", "update", "--yes"); !ok {
		t.Fatalf("修复后重跑应补齐：\n%s", out)
	}
	b, _ := os.ReadFile(proto)
	eq(t, string(b), "new protocol\n", "提示词已补齐")
	eq(t, (&app{root: root}).localVersion(), "0.0.9", "最后才换清单")
}

func TestUntarRejectsOversizedEntry(t *testing.T) {
	old := maxEntry
	maxEntry = 4
	t.Cleanup(func() { maxEntry = old })
	dir := t.TempDir()
	if err := untar(makePkg(t, []tarEntry{{name: "nakama/a", body: "1234"}}), dir); err != nil {
		t.Fatalf("恰好上限应通过：%v", err)
	}
	if err := untar(makePkg(t, []tarEntry{{name: "nakama/b", body: "12345"}}), dir); err == nil {
		t.Fatal("超过上限应报错，不能截断")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "b")); len(b) != 0 {
		t.Fatalf("不应留下截断文件：%q", b)
	}
}
