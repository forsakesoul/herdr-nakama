package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// 面向用户的中文字面量必须经 L()（或是 const 文案）且在 catalog 中有 en/ja，格式化动词逐个一致。
func TestCatalogComplete(t *testing.T) {
	verbs := regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z%]`)
	used := map[string]bool{}
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "i18n_catalog.go" {
			continue
		}
		fs := token.NewFileSet()
		af, err := parser.ParseFile(fs, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		var stack []ast.Node
		ast.Inspect(af, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			bl, ok := n.(*ast.BasicLit)
			if !ok || bl.Kind != token.STRING {
				return true
			}
			s, _ := strconv.Unquote(bl.Value)
			if !hasHan(s) {
				return true
			}
			for _, p := range stack {
				if g, isGen := p.(*ast.GenDecl); isGen && g.Tok == token.CONST {
					return true // const 文案按整体值登记（见下方 usageText/rebindUsage）
				}
			}
			ok = false
			if c, isCall := stack[len(stack)-2].(*ast.CallExpr); isCall {
				if id, isID := c.Fun.(*ast.Ident); isID && id.Name == "L" {
					ok = true
				}
			}
			if !ok {
				t.Errorf("%s: 中文字面量未经 L()：%s", fs.Position(bl.Pos()), bl.Value)
			}
			used[s] = true
			return true
		})
	}
	used[usageText], used[rebindUsage] = true, true
	for k := range used {
		if _, ok := catalog[k]; !ok {
			t.Errorf("catalog 缺少：%q", k)
		}
	}
	for k, tr := range catalog {
		if !used[k] {
			t.Errorf("catalog 多余条目：%q", k)
		}
		for _, v := range tr {
			if v == "" || strings.Join(verbs.FindAllString(v, -1), " ") != strings.Join(verbs.FindAllString(k, -1), " ") {
				t.Errorf("译文缺失或格式化动词不一致：%q → %q", k, v)
			}
		}
	}
}

func TestLangSelection(t *testing.T) {
	for _, k := range []string{"NAKAMA_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Setenv(k, "")
	}
	eq(t, lang(), "en", "无设置默认 en")
	t.Setenv("LANG", "ja_JP.UTF-8")
	eq(t, lang(), "ja", "LANG=ja")
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	eq(t, lang(), "zh", "LC_ALL 优先于 LANG")
	t.Setenv("NAKAMA_LANG", "fr")
	eq(t, lang(), "en", "未支持语言回落 en")
	eq(t, L("无法生成 run id。"), "cannot generate run id.", "en 译文")
	t.Setenv("NAKAMA_LANG", "ja")
	eq(t, L("无法生成 run id。"), "run id を生成できません。", "ja 译文")
	eq(t, L("不在目录里"), "不在目录里", "缺失回落源文案")
}

// 每种语言都有四份模板，且 team-rule / mid-task 标记逐行一致（renderRole 依赖它们）。
func TestPromptsPerLanguage(t *testing.T) {
	marker := regexp.MustCompile(`(?m)^<!-- .* -->$`)
	for _, name := range []string{"protocol", "owner", "worker", "advisor"} {
		var want string
		for i, l := range []string{"zh", "en", "ja"} {
			b, err := os.ReadFile(filepath.Join("..", "..", "prompts", l, name+".md"))
			if err != nil {
				t.Fatal(err)
			}
			got := strings.Join(marker.FindAllString(string(b), -1), "\n")
			if i == 0 {
				want = got
			}
			eq(t, got, want, l+"/"+name+".md 标记")
		}
	}
}
