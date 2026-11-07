#!/usr/bin/env python3
"""Build docs/index.html (GitHub Pages) for Nakama.

Edit the copy in T or the styles below, then rerun this script and commit docs/index.html.
`--check` exits non-zero when the committed page is out of date instead of writing it.
Media URLs carry ?v=<first 8 hex of sha256> so a replaced file under the same name is not served from cache.
"""
import hashlib
import html
import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "docs/index.html"
REPO = "https://github.com/forsakesoul/herdr-nakama"
INSTALL = """herdr plugin install forsakesoul/herdr-nakama
cfg="$(herdr plugin config-dir forsakesoul.nakama)/config.json"
[ -e "$cfg" ] || (umask 077 && mkdir -p "${cfg%/*}" && \\
  curl -fsSL https://raw.githubusercontent.com/forsakesoul/herdr-nakama/main/config.example.json -o "$cfg.tmp" && mv "$cfg.tmp" "$cfg")"""
KEYS = """[[keys.command]]
key = "prefix+u"
type = "plugin_action"
command = "forsakesoul.nakama.pick"

[[keys.command]]
key = "prefix+i"
type = "plugin_action"
command = "forsakesoul.nakama.init\""""
REBIND = """nakama rebind --team <id> --preflight
nakama rebind --team <id> --confirm-anchor <terminal>"""

T = {
"en": dict(
  docs="Docs", readme=f"{REPO}#readme", crumb="Herdr plugin · Owner / Worker / Advisor",
  h1a="One pane in Herdr,", h1b="a whole crew", h1c="of coding agents.",
  lead="Nakama builds an Owner / Worker / Advisor team from the pane you're in, initializes every role with the same protocol, and brings the same team back after a restart.",
  cta_install="Install", cta_github="GitHub",
  video_note="85-second walkthrough · illustration, not a screen recording",
  steps_h="Three steps to your first crew",
  s1="Install the plugin and create your private config (never committed).",
  s2="Bind two keys in <code>~/.config/herdr/config.toml</code>, then <code>herdr server reload-config</code>.",
  s3="In a project pane press <kbd>prefix</kbd>+<kbd>u</kbd>, pick the engines and confirm. When the agents are up, press <kbd>prefix</kbd>+<kbd>i</kbd>.",
  crew_h="The crew",
  owner_t="Owner · captain", owner_d="Investigates, freezes the handoff and accepts the result. Never writes project code.",
  worker_t="Worker · crew at work", worker_d="The only one who changes code. Reports back when done, blocked or out of context.",
  advisor_t="Advisor · navigator", advisor_d="Optional. Checks the plan and reviews the diff independently.", optional="optional",
  restart_h="After a restart, the same crew",
  restart_p="Herdr or the computer restarted and every terminal ID changed? Preflight first — read-only, zero writes — then confirm to bind the team again. Nothing is renamed or resent behind your back.",
  why_h="Why “Nakama”",
  why_p="<em>Nakama</em> (仲間) is what Luffy calls his crew in <em>One Piece</em>: companions who each own part of the voyage. And like a Vivre Card pointing home, <code>nakama rebind</code> finds every member again after a storm.",
  lang_h="Your language", lang_p="CLI output and agent prompts in English, 中文 and 日本語 — set <code>NAKAMA_LANG</code>.",
  thanks="Built for <a href=\"https://herdr.dev\">Herdr</a> · video made with <a href=\"https://github.com/leeguooooo/motion-use\">motion-use</a> and Azure AI Speech · MIT License · not affiliated with <em>One Piece</em>, Eiichiro Oda, Shueisha or Toei Animation",
  copy="Copy", copied="Copied", video="nakama-usage-en"),
"zh": dict(
  docs="文档", readme=f"{REPO}/blob/main/README.zh-CN.md", crumb="Herdr 插件 · Owner / Worker / Advisor",
  h1a="在 Herdr 里，一个 pane", h1b="拉起一整船", h1c="编码 agent 伙伴。",
  lead="Nakama 以你所在的 pane 为锚点，组建 Owner / Worker / Advisor 编组，用同一套协议初始化每个角色；电脑或 Herdr 重启后，还能把原来那支船团接回来。",
  cta_install="安装", cta_github="GitHub",
  video_note="85 秒使用演示 · 画面为示意动画，不是真实录屏",
  steps_h="三步组建你的第一支船团",
  s1="安装插件，生成私有配置（不入库）。",
  s2="在 <code>~/.config/herdr/config.toml</code> 绑定两个快捷键，然后 <code>herdr server reload-config</code>。",
  s3="在项目 pane 按 <kbd>prefix</kbd>+<kbd>u</kbd>，选好引擎并确认；agent 起好后按 <kbd>prefix</kbd>+<kbd>i</kbd>。",
  crew_h="船员分工",
  owner_t="Owner · 船长", owner_d="调研、冻结交接包、验收结果，自己不改项目代码。",
  worker_t="Worker · 动手的船员", worker_d="唯一修改代码的人；完成、受阻或上下文快用完时主动回报。",
  advisor_t="Advisor · 航海士", advisor_d="可选。独立核验方案、审查 diff。", optional="可选",
  restart_h="重启之后，还是那支船团",
  restart_p="电脑或 Herdr 重启、终端编号全变了？先预检（只读、零写入），确认后重新绑定。不会背着你改名或重发任何消息。",
  why_h="为什么叫 Nakama",
  why_p="<em>Nakama</em>（仲間）是《海贼王》里路飞对船员的称呼：各自负责一段航程的伙伴。就像指向主人的生命卡，<code>nakama rebind</code> 会在风暴过后把每位伙伴找回来。",
  lang_h="你的语言", lang_p="CLI 输出与 agent 提示词支持 English、中文、日本語，用 <code>NAKAMA_LANG</code> 选择。",
  thanks="为 <a href=\"https://herdr.dev\">Herdr</a> 打造 · 视频由 <a href=\"https://github.com/leeguooooo/motion-use\">motion-use</a> 与 Azure AI 语音制作 · MIT 许可证 · 与《海贼王》、尾田荣一郎、集英社及东映动画无关",
  copy="复制", copied="已复制", video="nakama-usage-zh"),
"ja": dict(
  docs="ドキュメント", readme=f"{REPO}/blob/main/README.ja.md", crumb="Herdr プラグイン · Owner / Worker / Advisor",
  h1a="Herdr の 1 つの pane から、", h1b="エージェントの一味", h1c="を。",
  lead="Nakama はいまの pane を起点に Owner / Worker / Advisor のチームを組み、同じプロトコルで各ロールを初期化します。再起動後も、元の一味をそのまま呼び戻せます。",
  cta_install="インストール", cta_github="GitHub",
  video_note="85 秒の使い方紹介（英語音声）· 画面はイメージ図で、実際の録画ではありません",
  steps_h="3 ステップで最初の一味を",
  s1="プラグインをインストールし、非公開の設定を作成します（コミットしません）。",
  s2="<code>~/.config/herdr/config.toml</code> にキーを 2 つ割り当て、<code>herdr server reload-config</code> を実行します。",
  s3="プロジェクトの pane で <kbd>prefix</kbd>+<kbd>u</kbd> を押してエンジンを選び確定。エージェントが起動したら <kbd>prefix</kbd>+<kbd>i</kbd>。",
  crew_h="一味の役割",
  owner_t="Owner · 船長", owner_d="調査し、引き継ぎを確定し、結果を受け入れます。プロジェクトのコードは書きません。",
  worker_t="Worker · 手を動かす船員", worker_d="コードを変更する唯一の存在。完了・ブロック・コンテキスト不足のときに報告します。",
  advisor_t="Advisor · 航海士", advisor_d="任意。方針を検証し、diff を独立にレビューします。", optional="任意",
  restart_h="再起動しても、同じ一味",
  restart_p="Herdr やコンピューターが再起動して terminal ID がすべて変わっても大丈夫。まずプリフライト（読み取り専用・書き込みゼロ）、確認してから再バインドします。勝手に改名や再送はしません。",
  why_h="なぜ「Nakama」なのか",
  why_p="<em>Nakama</em>（仲間）は『ONE PIECE』でルフィが船員を呼ぶ言葉です。ビブルカードのように、<code>nakama rebind</code> は嵐のあとで仲間を一人ひとり見つけ出します。",
  lang_h="あなたの言語で", lang_p="CLI 出力とエージェントのプロンプトは English・中文・日本語に対応。<code>NAKAMA_LANG</code> で選べます。",
  thanks="<a href=\"https://herdr.dev\">Herdr</a> のためのプラグイン · 動画は <a href=\"https://github.com/leeguooooo/motion-use\">motion-use</a> と Azure AI Speech で制作 · MIT ライセンス · 『ONE PIECE』、尾田栄一郎氏、集英社、東映アニメーションとは無関係です",
  copy="コピー", copied="コピー済み", video="nakama-usage-en"),
}

FONTS = "family=ZCOOL+KuaiLe&family=Permanent+Marker&family=Noto+Sans+SC:wght@400;700&family=JetBrains+Mono:wght@400;700"

THEME_CSS = """
:root{--bg:#f6eedb;--fg:#1d2a44;--sub:#5a5f6b;--line:#dccfae;--accent:#c8372d;--hi:#f2c230;--card:#fbf6e8;--link:#1f5ea8;--code:#16223b;--codefg:#ebe4d1;
--display:"ZCOOL KuaiLe","Chalkboard SE",cursive;--logo:"Permanent Marker",cursive;--body:"Noto Sans SC",-apple-system,"PingFang SC",sans-serif;--mono:"JetBrains Mono",ui-monospace,Menlo,monospace;--radius:14px}
body{background-image:radial-gradient(circle at 15% 8%,#fbf5e6 0,transparent 42%),radial-gradient(circle at 92% 75%,#eedfbd 0,transparent 46%)}
.brand{font-family:var(--logo);letter-spacing:.5px}.brand b{color:var(--accent);font-weight:400}
h1,h2{font-family:var(--display);font-weight:400}
h2::before{content:"";display:inline-block;width:20px;height:20px;margin-right:12px;border:3px solid var(--accent);border-radius:50%;vertical-align:-1px;box-shadow:inset 0 0 0 3px var(--bg),inset 0 0 0 6px var(--accent)}
.hi{background:linear-gradient(transparent 55%,var(--hi) 55%);padding:0 .1em}
.crumb{color:var(--accent)}
.btn.primary{background:var(--hi);border:2px solid var(--fg);color:var(--fg);box-shadow:3px 3px 0 var(--fg)}
.btn{border:2px solid var(--fg)}
.num{background:var(--accent);color:#fff8e7;border:3px double #fff8e7;box-shadow:0 0 0 2px var(--accent)}
.card{border:2px dashed #cbb98d;background:var(--card)}
.card.why{background:#fffaf0;border:2px solid var(--fg);position:relative;box-shadow:5px 5px 0 var(--hi);transform:rotate(-.6deg)}
.card.why::after{content:"";position:absolute;right:20px;top:-11px;width:72px;height:22px;background:#c8372d55;transform:rotate(4deg)}
.video{border:2px solid var(--fg);box-shadow:6px 6px 0 var(--hi)}
.langs button[aria-pressed=true]{background:var(--fg);color:var(--bg)}
"""

BASE_CSS = """
*{box-sizing:border-box}html{scroll-behavior:smooth}
body{margin:0;background:var(--bg);color:var(--fg);font:17px/1.75 var(--body)}
a{color:var(--link)}
.top{position:sticky;top:0;z-index:5;background:color-mix(in srgb,var(--bg) 88%,transparent);backdrop-filter:blur(8px);border-bottom:1px solid var(--line)}
.top .in{max-width:1080px;margin:0 auto;padding:12px 24px;display:flex;align-items:center;gap:18px}
.brand{font-size:24px;color:var(--fg);text-decoration:none}
.top nav{margin-left:auto;display:flex;align-items:center;gap:10px}
.top nav>a{color:var(--fg);text-decoration:none;font-size:15px;padding:6px 10px}
.langs{display:inline-flex;border:1px solid var(--line);border-radius:999px;overflow:hidden}
.langs button{font:600 13px var(--body);border:0;background:transparent;color:var(--sub);padding:6px 12px;cursor:pointer}
.langs button[aria-pressed=true]{background:var(--fg);color:var(--bg)}
main{max-width:1080px;margin:0 auto;padding:40px 24px 80px}
.hero{display:grid;grid-template-columns:1fr;gap:28px;padding:24px 0 8px}
.crumb{font:600 13px var(--mono);text-transform:uppercase;letter-spacing:2px;margin:0}
h1{font-size:clamp(36px,6vw,64px);line-height:1.18;margin:6px 0 0}
.lead{font-size:19px;color:var(--sub);max-width:760px;margin:0}
.ctas{display:flex;gap:14px;flex-wrap:wrap}
.btn{display:inline-flex;align-items:center;gap:8px;padding:10px 22px;border-radius:999px;font-weight:700;text-decoration:none;color:var(--fg)}
.video{display:block;width:100%;border-radius:var(--radius);background:#000;margin-top:8px}
.note{color:var(--sub);font-size:14px;margin:10px 0 0}
section{margin-top:72px}
h2{font-size:clamp(26px,3.6vw,36px);margin:0 0 22px}
.steps{list-style:none;padding:0;margin:0;display:grid;gap:26px}
.steps li{display:grid;grid-template-columns:44px 1fr;gap:16px;align-items:start}
.num{width:36px;height:36px;border-radius:50%;display:grid;place-items:center;font:800 17px var(--mono);margin-top:2px}
.steps p{margin:4px 0 12px}
.term{border-radius:12px;overflow:hidden;background:var(--code);color:var(--codefg)}
.term .bar{display:flex;align-items:center;gap:7px;padding:9px 12px;background:#ffffff0d}
.term .bar i{width:11px;height:11px;border-radius:50%;display:inline-block}
.term .bar i:nth-child(1){background:#ff5f57}.term .bar i:nth-child(2){background:#febc2e}.term .bar i:nth-child(3){background:#28c840}
.term .bar span{font:12px var(--mono);opacity:.6;margin-left:6px}
.term .bar button{margin-left:auto;font:12px var(--body);color:inherit;background:#ffffff14;border:1px solid #ffffff26;border-radius:6px;padding:2px 10px;cursor:pointer}
.term pre{margin:0;padding:14px 18px 16px;overflow-x:auto;font:14px/1.65 var(--mono)}
code{font-family:var(--mono);font-size:.9em}
kbd{font:600 .85em var(--mono);border:1px solid #bdb6a3;border-bottom-width:3px;border-radius:6px;padding:1px 7px;background:#fff}
.crew{display:grid;grid-template-columns:repeat(3,1fr);gap:18px}
.card{border-radius:var(--radius);padding:22px}
.card h3{margin:0 0 6px;font-size:19px}
.card p{margin:0;color:var(--sub)}
.tag{display:inline-block;font:600 12px var(--mono);border:1px solid currentColor;border-radius:999px;padding:0 8px;margin-left:6px;color:var(--accent);vertical-align:2px}
.two{display:grid;grid-template-columns:1.1fr 1fr;gap:24px;align-items:start}
.two p{margin:0 0 14px}
footer{margin-top:80px;padding-top:22px;border-top:1px solid var(--line);color:var(--sub);font-size:14px}
[data-l]{display:none}[data-l].on{display:revert}
@media (max-width:820px){.crew,.two{grid-template-columns:1fr}.top nav>a.hide-s{display:none}}
"""

def term(label, body, t):
    return (f'<div class="term"><div class="bar"><i></i><i></i><i></i><span>{label}</span>'
            f'<button type="button" class="cp">{t["copy"]}</button></div><pre>{html.escape(body)}</pre></div>')

# Content-hash query strings: Pages caches media for 10 min and browsers longer, so a replaced
# file under the same name would otherwise keep showing the old version.
VERSIONS = {}

def asset(media, name):
    v = VERSIONS.get(name)
    return f"{media}/{name}?v={v}" if v else f"{media}/{name}"

def section_for(lang, t, media):
    return f"""
<div data-l="{lang}">
  <div class="hero">
    <p class="crumb">{t['crumb']}</p>
    <h1>{t['h1a']}<br><span class="hi">{t['h1b']}</span>{t['h1c']}</h1>
    <p class="lead">{t['lead']}</p>
    <div class="ctas"><a class="btn primary" href="#install-{lang}">{t['cta_install']}</a><a class="btn" href="{REPO}">{t['cta_github']} ↗</a></div>
    <video class="video" controls preload="metadata" playsinline poster="{asset(media, t['video'] + '.png')}" src="{asset(media, t['video'] + '.mp4')}"></video>
    <p class="note">{t['video_note']}</p>
  </div>
  <section id="install-{lang}">
    <h2>{t['steps_h']}</h2>
    <ol class="steps">
      <li><span class="num">1</span><div><p>{t['s1']}</p>{term('bash', INSTALL, t)}</div></li>
      <li><span class="num">2</span><div><p>{t['s2']}</p>{term('config.toml', KEYS, t)}</div></li>
      <li><span class="num">3</span><div><p>{t['s3']}</p></div></li>
    </ol>
  </section>
  <section>
    <h2>{t['crew_h']}</h2>
    <div class="crew">
      <div class="card"><h3>{t['owner_t']}</h3><p>{t['owner_d']}</p></div>
      <div class="card"><h3>{t['worker_t']}</h3><p>{t['worker_d']}</p></div>
      <div class="card"><h3>{t['advisor_t']}<span class="tag">{t['optional']}</span></h3><p>{t['advisor_d']}</p></div>
    </div>
  </section>
  <section class="two">
    <div><h2>{t['restart_h']}</h2><p>{t['restart_p']}</p></div>
    <div>{term('bash', REBIND, t)}</div>
  </section>
  <section class="two">
    <div class="card why"><h2>{t['why_h']}</h2><p>{t['why_p']}</p></div>
    <div><h2>{t['lang_h']}</h2><p>{t['lang_p']}</p></div>
  </section>
  <footer>{t['thanks']}</footer>
</div>"""

def page(media):
    secs = "".join(section_for(l, T[l], media) for l in ("en", "zh", "ja"))
    docs_links = "".join(f'<a class="hide-s" data-l="{l}" href="{T[l]["readme"]}">{T[l]["docs"]}</a>' for l in T)
    copy_txt = json.dumps({l: [T[l]["copy"], T[l]["copied"]] for l in T}, ensure_ascii=False)
    return f"""<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Nakama — a crew of coding agents for Herdr</title>
<meta name="description" content="A native Herdr plugin that builds, initializes and restores Owner / Worker / Advisor coding-agent teams.">
<link rel="preconnect" href="https://fonts.googleapis.com"><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?{FONTS}&display=swap">
<style>{BASE_CSS}{THEME_CSS}</style>
<noscript><style>[data-l="en"]{{display:revert}}</style></noscript>
</head>
<body>
<div class="top"><div class="in">
  <a class="brand" href="#">Naka<b>ma</b></a>
  <nav>{docs_links}<a href="{REPO}">GitHub</a>
    <span class="langs" role="group" aria-label="Language"><button type="button" data-set="en">EN</button><button type="button" data-set="zh">中</button><button type="button" data-set="ja">日</button></span>
  </nav>
</div></div>
<main>{secs}</main>
<script>
const COPY = {copy_txt}, langs = ["en", "zh", "ja"];
let cur = "en";
function pick(l, keep) {{
  if (!langs.includes(l)) l = "en";
  cur = l;
  document.documentElement.lang = l === "zh" ? "zh-CN" : l;
  document.querySelectorAll("[data-l]").forEach((e) => {{
    const on = e.dataset.l === l;
    e.classList.toggle("on", on);
    if (!on) e.querySelectorAll("video").forEach((v) => v.pause());
  }});
  document.querySelectorAll(".langs button").forEach((b) => b.setAttribute("aria-pressed", String(b.dataset.set === l)));
  if (!keep) history.replaceState(null, "", "#" + l);
}}
document.querySelectorAll(".langs button").forEach((b) => b.addEventListener("click", () => pick(b.dataset.set)));
document.querySelectorAll(".cp").forEach((b) => b.addEventListener("click", async () => {{
  const pre = b.closest(".term").querySelector("pre");
  try {{ await navigator.clipboard.writeText(pre.textContent); }}
  catch {{ getSelection().selectAllChildren(pre); return; }}  // no clipboard access: leave the command selected for manual copy
  b.textContent = COPY[cur][1];
  setTimeout(() => (b.textContent = COPY[cur][0]), 1400);
}}));
// A fragment is a language code (#zh) or ends with one (#install-zh): show that language and keep the fragment.
function langOf(h) {{ const m = h.match(/(?:^|-)([a-z]{{2}})$/); return m && langs.includes(m[1]) ? m[1] : null; }}
addEventListener("hashchange", () => {{ const h = location.hash.slice(1), l = langOf(h); if (l) pick(l, h !== l); }});
const h = location.hash.slice(1), hl = langOf(h);
pick(hl || (navigator.language || "en").slice(0, 2).toLowerCase(), !!hl && h !== hl);
</script>
</body>
</html>
"""

if __name__ == "__main__":
    for f in sorted((OUT.parent / "media").iterdir()):
        VERSIONS[f.name] = hashlib.sha256(f.read_bytes()).hexdigest()[:8]
    html_text = page("media")
    if "--check" in sys.argv:
        if not OUT.exists() or OUT.read_text() != html_text:
            sys.exit("docs/index.html is out of date; run python3 tools/build_pages.py")
        print("docs/index.html is up to date")
    else:
        OUT.write_text(html_text)
        print(f"wrote {OUT.relative_to(ROOT)}")
