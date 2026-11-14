# Nakama

[English](README.md) | 简体中文 | [日本語](README.ja.md)

在 [Herdr](https://herdr.dev) 里把多个编码 agent 组成一支分工明确的小队：1 个 Owner 负责设计与验收，1–4 个 Worker 负责实现，0–4 个可选 Advisor 负责独立审查。Nakama 是 Herdr 原生插件，负责建组、初始化、中途增减成员，并在电脑或 Herdr 重启后把同一支队伍找回来。

https://github.com/user-attachments/assets/f2b21f2f-da61-4832-9113-158efeb8f4ec

▶ 109 秒使用演示 · [English version](https://github.com/user-attachments/assets/861b71da-e0f5-4123-bc7f-5a1b3bc27bec) · [项目主页](https://forsakesoul.github.io/herdr-nakama/)

## 一张图看懂 Nakama

![herdr-nakama 概览：1 个 Owner、1–4 个 Worker 与 0–4 个可选 Advisor 按角色协作；Nakama 负责选人数建组、初始化、查看状态、重启后重新绑定，以及中途添加、移除或替换成员。](docs/media/nakama-overview-zh.png)

每个成员在同一个 Herdr 标签页的独立 pane 中运行；角色分工由提示词和协作协议约定，多个 Worker 各自只修改互不重叠的范围。具体操作与恢复条件见下文。

## 为什么叫 Nakama

*Nakama*（仲間）是《海贼王》里路飞对船员的称呼：不是雇员，也不是工具，而是各自负责一段航程、又把后背交给彼此的伙伴。这正是本插件要组建的团队：

- **Owner：船长。** 定航向：调研、冻结交接包、验收结果，自己不改项目代码。
- **Worker：动手的船员。** 只有他们修改代码；完成、受阻或上下文快用完时主动回报。
- **Advisor：航海士。** 可选；独立核验方案、审查 diff。

每位伙伴在自己的 pane 里、用自己的引擎航行，共享协议就是他们的沟通方式：冻结交接包、发 NOTICE、谁也不替别人作答。当一场风暴（电脑或 Herdr 重启）把船员吹散时，`nakama rebind` 会像“生命卡”指引方向一样把每个人找回来：还是原来那支船团，而不是重新拉一支。

这个名字是致敬，不代表任何关联：本项目与《海贼王》、尾田荣一郎、集英社及东映动画均无关系。

## 安装

需要 macOS 和 Herdr 0.9.3 及以上。下面的安装命令会在本机构建插件，需要 Go 1.24 及以上；没有 Go 就用后面的发布包。

```sh
# 1. 安装（会执行 manifest 中的构建）；本地开发改用：go build … && herdr plugin link "$PWD" --enabled
herdr plugin install forsakesoul/herdr-nakama
# 2. 私有配置：仅在不存在时以 config.example.json 为模板生成（0600），再按需填写 pi_provider / cc_env_file（私有，勿入库）
cfg="$(herdr plugin config-dir forsakesoul.nakama)/config.json"
[ -e "$cfg" ] || (umask 077 && mkdir -p "${cfg%/*}" && curl -fsSL https://raw.githubusercontent.com/forsakesoul/herdr-nakama/main/config.example.json -o "$cfg.tmp" && mv "$cfg.tmp" "$cfg")
# 3. 绑定快捷键（追加到 ~/.config/herdr/config.toml）后重载配置
herdr server reload-config
```

### 不装 Go：用发布包

发布包内已含编译好的二进制，无需安装 Go，也无需 clone：

```sh
arch="$(uname -m | sed 's/x86_64/amd64/')"
dir="$HOME/.local/share/herdr-nakama"
mkdir -p "$dir" && curl -fsSL "https://github.com/forsakesoul/herdr-nakama/releases/latest/download/nakama-darwin-$arch.tar.gz" | tar -xz -C "$dir" --strip-components=1
herdr plugin link "$dir" --enabled
```

固定版本时把 `latest/download` 换成 `download/v0.0.3`；升级时重新执行同一段。从 v0.0.3 起提供。

快捷键示例（`config.toml`）：

```toml
[[keys.command]]
key = "prefix+u"
type = "plugin_action"
command = "forsakesoul.nakama.pick"
description = "nakama: 选择引擎并以当前 pane 为锚点建组"

[[keys.command]]
key = "prefix+i"
type = "plugin_action"
command = "forsakesoul.nakama.init"
description = "nakama: init（初始化当前 anchor 的编组）"
```

## 快速上手

1. **建组**：在项目目录的一个 pane 里按 `prefix+u`，依次选择 Owner、Worker、Advisor 的引擎，以及 Worker / Advisor 的数量（回车取默认，Advisor 可填 `none`）。确认后，插件在当前标签页里为每个成员分出一个 pane 并启动 agent。
2. **初始化**：等各 pane 里的 agent 启动好（有授权对话框就先处理），按 `prefix+i`。每个成员会收到自己的角色说明和协作规则。
3. **开始工作**：在 Owner 的 pane 里说出需求。Owner 拆分任务并交给 Worker，Worker 完成后回报，Advisor 按需审查。

查看编组状态：`herdr plugin action invoke status --plugin forsakesoul.nakama`。

## 中途增减成员

在编组的任意一个 pane 里再按 `prefix+u`，打开的是该编组的管理菜单：

| 按键 | 作用 |
|---|---|
| `n` / `a` | 添加 Worker / Advisor |
| `r` / `x` | 移除 Worker / Advisor |
| `w` / `v` | 替换 Worker / Advisor（换引擎，或换掉没有进展的成员） |
| `c` | 清理任务记录 |
| `b` | 重启后重绑编组（仅在需要时显示） |
| `d` | 解散编组：状态归档，pane 保留 |

- 添加或替换后，等新成员启动好再按一次 `prefix+i`：新成员完成初始化，其他成员收到最新的成员名单。
- 新成员只在当前标签页内打开。Worker 至少保留 1 个，Worker 和 Advisor 各最多 4 个，Owner 不可替换。
- 被移除或替换的成员，其 pane 会保留，由你自行关闭。替换 Worker 前请先确认旧 Worker 已停止写入，避免新旧两个 Worker 同时改同一范围。
- 同类成员有多个时，移除或替换会先让你输入成员键（如 `W2`）。

## 重启后恢复

电脑或 Herdr 重启后，各 pane 的终端标识会变化，编组需要重新绑定才能继续使用：

```sh
nakama rebind --team <id> --preflight      # 只读预检，列出新旧差异
nakama rebind --team <id> --confirm-anchor <新锚点 terminal>
```

按预检输出的提示补充确认参数。恢复的是原来那支队伍，各成员保留原有对话。拒绝条件和部分失败后的处理见[参考文档](docs/reference.zh-CN.md)。

也可以在编组任一 pane 按 `prefix+u` 选 `b`：它显示同样的预检结果，确认后按观测到的值重绑。

## 任务记录

每个编组在项目目录下有一个 `.nakama/<编组ID>/` 目录，保存交接包、回报和审查结论，默认不进入 Git。任务完成后不会自动删除：你说可以删，Owner 才清理；也可以用管理菜单的 `c` 或 `nakama clean` 自己清理。 成员之间通过 `nakama send` 互发消息：它为每条消息编号、把正文存到这里，并在台账里记录是否送达；`nakama status` 会列出还没等到答复的请求和没送达的消息。

## 配置

配置目录：`$HERDR_PLUGIN_CONFIG_DIR`（Herdr 注入），否则 `${XDG_CONFIG_HOME:-$HOME/.config}/nakama`。
文件：`config.json`。**不要把真实 provider / 路径 / 凭据提交进仓库**。

```json
{
  "default_profile": {
    "owner": "sol",
    "worker": "ds",
    "advisor": "none",
    "engine_map": { "sol": "codex/<model>:high", "ds": { "owner": "pi/<model>:low", "worker": "pi/<model>:low", "advisor": "pi/<model>:high" } },
    "cc_env_file": ""
  },
  "projects": {
    "<repo-name>": { "prefix": "<short>", "context_file": "/abs/project-context.md", "trigger": ";xx.ctx" }
  }
}
```

- 引擎写法是 `<launcher>/<model>[:effort]`，也可以在 `engine_map` 里定义简写。
- `projects` 为指定仓库设置编组名前缀和项目背景文件，初始化时一并带给各成员。
- `default_profile.auto_init: true` 会在建组、加人或换人后接着初始化。默认关闭，方便你先核对各 pane 的模型和权限。
- 界面和提示词语言由 `NAKAMA_LANG=en|zh|ja` 决定，未设置时跟随系统语言；需设置在启动 Herdr 的环境里。
- 各启动器的细节（`pi` 的 provider、`ccenv`、Claude 模型预设）见[参考文档](docs/reference.zh-CN.md)。

## 命令

```sh
nakama up [--owner E] [--worker E] [--workers N] [--advisor E|none] [--advisors N] [--pane P] [--cwd C]
nakama init --team <id>
nakama status [--team <id>]
nakama rebind --team <id> [--preflight | --discard-pending] [--confirm-session <成员键|anchor>=<session>]... [--confirm-anchor <terminal>]
nakama add --team <id> [--role worker|advisor] [--engine E]
nakama drop --team <id> [--role worker|advisor] [--instance N]
nakama swap --team <id> --role worker|advisor [--instance N] [--engine E]
nakama clean [--team <id>] [--task <task_id>]... [--all] [--yes] [--force]
nakama send --team <id> --task <task_id> --to <成员键> --title <标题> --file <正文文件> [...]
nakama down --team <id> [--yes]
nakama resolve --team <id> --delivered | --not-delivered
```

## 升级

升级插件后，之前创建的编组可以继续对话。如果新版本修改了共享协议，对旧编组执行初始化、添加或替换成员会被拒绝，需要重新建组。

## 更多

- [参考文档](docs/reference.zh-CN.md)：完整配置、命令细则、状态与安全、重启恢复的全部规则、协议来源、测试与已知限制。
- [共享协议与角色提示词](prompts/zh/)：各角色实际收到的内容。
- [项目主页](https://forsakesoul.github.io/herdr-nakama/)

## 致谢

- [Herdr](https://herdr.dev)（[herdrdev/herdr](https://github.com/herdrdev/herdr)，Apache-2.0）：Nakama 所运行的编码 agent 终端复用器，所有 pane、agent 与改名操作都经由它的 CLI 完成。
- [motion-use](https://github.com/leeguooooo/motion-use)（MIT）：用于编排和渲染使用演示视频，包括字幕、画面文字和旁白的检查。
- [Azure AI 语音](https://azure.microsoft.com/products/ai-services/ai-speech)：视频旁白的神经网络语音合成。

## 许可证

[MIT](LICENSE)
