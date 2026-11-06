package main

import (
	"os"
	"strings"
)

// lang：NAKAMA_LANG > LC_ALL > LC_MESSAGES > LANG，取 zh/ja 前缀，其余一律 en。
// 每次现读环境，测试用 t.Setenv 即可切换。
func lang() string {
	for _, k := range []string{"NAKAMA_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		v := strings.ToLower(os.Getenv(k))
		if v == "" {
			continue
		}
		for _, l := range []string{"zh", "ja"} {
			if strings.HasPrefix(v, l) {
				return l
			}
		}
		return "en"
	}
	return "en"
}

// L 把中文源文案换成当前语言；目录缺失时回落源文案（TestCatalogComplete 保证不会缺）。
func L(zh string) string {
	i := map[string]int{"en": 0, "ja": 1}
	n, ok := i[lang()]
	if !ok {
		return zh
	}
	if tr, ok := catalog[zh]; ok {
		return tr[n]
	}
	return zh
}
