package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// version / update：版本取自插件根的 herdr-plugin.toml（与 Herdr 看到的一致，不另埋进二进制）。
// update 按安装方式分三路：Herdr 从 GitHub 安装的交给 herdr plugin update；源码检出（有 .git）不代为改动；
// 发布包（link 的解压目录）才由本命令下载、校验并替换。

const releaseRepo = "https://github.com/forsakesoul/herdr-nakama"

var (
	versionLineRe = regexp.MustCompile(`(?m)^version = "([^"]+)"$`)
	releaseTagRe  = regexp.MustCompile(`^v[0-9]+(\.[0-9]+){2}$`)
	httpClient    = &http.Client{Timeout: 60 * time.Second}
)

func releaseBase() string {
	if b := os.Getenv("NAKAMA_RELEASE_BASE_URL"); b != "" { // 测试用
		return b
	}
	return releaseRepo
}

func (a *app) localVersion() string {
	b, _ := os.ReadFile(filepath.Join(a.root, "herdr-plugin.toml"))
	if m := versionLineRe.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return ""
}

// 最新版本：releases/latest 会跳转到 releases/tag/<tag>，只读跳转目标，不走有频率限制的 API。
func latestTag() (string, error) {
	c := *httpClient
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Get(releaseBase() + "/releases/latest")
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	tag := path.Base(resp.Header.Get("Location"))
	if !releaseTagRe.MatchString(tag) {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return tag, nil
}

// 按数字逐段比较 x.y.z；a 新于 b 返回 true。
func newer(a, b string) bool {
	as, bs := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, _ := strconv.Atoi(as[i])
		y, _ := strconv.Atoi(bs[i])
		if x != y {
			return x > y
		}
	}
	return len(as) > len(bs)
}

func (a *app) cmdVersion(args []string) error {
	v, help, err := a.parseArgs(args, L("用法：nakama version [--check]\n"), nil, []string{"--check"})
	if help || err != nil {
		return err
	}
	cur := a.localVersion()
	if cur == "" {
		return a.fail(L("无法从 %s 读取版本号。"), filepath.Join(a.root, "herdr-plugin.toml"))
	}
	a.outf("nakama %s\n", cur)
	if _, check := v["--check"]; !check {
		return nil
	}
	tag, err := latestTag()
	if err != nil {
		return a.fail(L("无法查询最新版本：%v"), err)
	}
	if newer(tag, cur) {
		a.outf(L("有新版本 %s，运行 nakama update 升级。\n"), tag)
	} else {
		a.outf(L("已是最新版本（最新发布 %s）。\n"), tag)
	}
	return nil
}

func fetch(url string, limit int64) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = fmt.Errorf("%s: too large", url)
	}
	return b, err
}

// 解包到 dir：只接受 nakama/ 下的普通文件和目录，拒绝链接、绝对路径与 ..。
func untar(pkg []byte, dir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(pkg))
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Clean(h.Name)
		if name == "nakama" {
			continue
		}
		rel := strings.TrimPrefix(name, "nakama/")
		if rel == name || !filepath.IsLocal(rel) {
			return fmt.Errorf("unexpected entry %q", h.Name)
		}
		dest := filepath.Join(dir, rel)
		switch h.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(dest, 0o755)
		case tar.TypeReg:
			if err = os.MkdirAll(filepath.Dir(dest), 0o755); err == nil {
				var f *os.File
				if f, err = os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, h.FileInfo().Mode().Perm()); err == nil {
					_, err = io.Copy(f, io.LimitReader(tr, 64<<20))
					if cerr := f.Close(); err == nil {
						err = cerr
					}
				}
			}
		default:
			err = fmt.Errorf("unexpected entry type for %q", h.Name)
		}
		if err != nil {
			return err
		}
	}
}

func (a *app) cmdUpdate(args []string) error {
	v, help, err := a.parseArgs(args, L("用法：nakama update [--check] [--yes]\n"), nil, []string{"--check", "--yes"})
	if help || err != nil {
		return err
	}
	_, check := v["--check"]
	_, yes := v["--yes"]
	cur := a.localVersion()
	if cur == "" {
		return a.fail(L("无法从 %s 读取版本号。"), filepath.Join(a.root, "herdr-plugin.toml"))
	}
	tag, err := latestTag()
	if err != nil {
		return a.fail(L("无法查询最新版本：%v"), err)
	}
	if !newer(tag, cur) {
		a.outf("nakama %s\n", cur)
		a.outf(L("已是最新版本（最新发布 %s）。\n"), tag)
		return nil
	}
	a.outf(L("有新版本 %s（当前 %s）。\n"), tag, cur)
	if check {
		return nil
	}

	// 安装方式以 Herdr 的记录为准；查不到（未注册）时按本地目录处理。
	var list struct {
		Result struct {
			Plugins []struct {
				Source struct {
					Kind string `json:"kind"`
				} `json:"source"`
			} `json:"plugins"`
		} `json:"result"`
	}
	if r := a.herdr("plugin", "list", "--json", "--plugin", pluginID); r.ok() {
		_ = json.Unmarshal(r.stdout, &list)
	}
	if ps := list.Result.Plugins; len(ps) == 1 && ps[0].Source.Kind != "" && ps[0].Source.Kind != "local" {
		a.outf("%s", L("本插件由 Herdr 从 GitHub 安装，交给 herdr plugin update 升级。\n"))
		if r := a.herdrPass("plugin", "update", pluginID, "--yes"); !r.ok() {
			return a.fail(L("herdr plugin update 失败。"))
		}
		return nil
	}
	if _, err := os.Lstat(filepath.Join(a.root, ".git")); err == nil {
		return a.fail(L("插件目录 %s 是源码检出，update 不改动它：请在该目录 git pull 后重新构建（go build -trimpath -o bin/nakama ./cmd/nakama）。"), a.root)
	}

	if !yes {
		if !stdinIsTTY() {
			return a.fail(L("非交互环境需要 --yes 确认；未升级。"))
		}
		a.outf(L("将下载 %s 并替换 %s 中的插件文件，确认？[y/N] "), tag, a.root)
		ans, err := a.readLine()
		if l := strings.ToLower(ans); err != nil || (l != "y" && l != "yes") {
			a.outf("%s", L("已取消。\n"))
			return nil
		}
	}
	name := "nakama-" + runtime.GOOS + "-" + runtime.GOARCH + ".tar.gz"
	dl := releaseBase() + "/releases/download/" + tag + "/"
	sums, err := fetch(dl+"SHA256SUMS", 1<<20)
	if err != nil {
		return a.fail(L("下载失败：%v"), err)
	}
	want := ""
	for _, l := range strings.Split(string(sums), "\n") {
		if f := strings.Fields(l); len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			want = f[0]
		}
	}
	if want == "" {
		return a.fail(L("发布 %s 没有适用于本机的安装包 %s。"), tag, name)
	}
	pkg, err := fetch(dl+name, 256<<20)
	if err != nil {
		return a.fail(L("下载失败：%v"), err)
	}
	if got := sha256.Sum256(pkg); hex.EncodeToString(got[:]) != want {
		return a.fail(L("安装包 %s 的 SHA-256 与 SHA256SUMS 不一致；未升级。"), name)
	}
	// 暂存目录与插件根同级，保证之后的 rename 不跨文件系统。
	stage, err := os.MkdirTemp(filepath.Dir(a.root), ".nakama-update-*")
	if err != nil {
		return a.fail(L("升级失败：%v"), err)
	}
	defer os.RemoveAll(stage)
	if err := untar(pkg, stage); err != nil {
		return a.fail(L("升级失败：%v"), err)
	}
	if !readableFile(filepath.Join(stage, "bin", "nakama")) || !readableFile(filepath.Join(stage, "herdr-plugin.toml")) {
		return a.fail(L("升级失败：%v"), "package is incomplete")
	}
	oldProto, _ := a.protocolSha()
	// ponytail: 逐项替换，不是整体原子；中途失败会留下新旧混合的目录，重跑 update 即可补齐。
	ents, _ := os.ReadDir(stage)
	for _, e := range ents {
		dest := filepath.Join(a.root, e.Name())
		if err = os.RemoveAll(dest); err == nil {
			err = os.Rename(filepath.Join(stage, e.Name()), dest)
		}
		if err != nil {
			return a.fail(L("升级失败：%v"), err)
		}
	}
	a.outf(L("已升级到 %s。运行 herdr server reload-config 让 Herdr 读取新的插件清单。\n"), tag)
	if newProto, _ := a.protocolSha(); newProto != oldProto {
		a.outf("%s", L("共享协议有变化：升级前创建的编组在 init / add / swap 时会被拒绝，需要重建。\n"))
	}
	return nil
}
