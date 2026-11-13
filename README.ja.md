# Nakama

[English](README.md) | [简体中文](README.zh-CN.md) | 日本語

[Herdr](https://herdr.dev) 上の複数のコーディングエージェントを、役割のはっきりしたチームにまとめます。Owner 1 名が設計と受け入れを担い、Worker 1〜4 名が実装し、任意の Advisor 0〜4 名が独立してレビューします。Nakama は Herdr ネイティブプラグインで、チームの作成、初期化、タスク途中のメンバー追加・削除を行い、コンピューターや Herdr の再起動後には同じチームを復元します。

https://github.com/user-attachments/assets/861b71da-e0f5-4123-bc7f-5a1b3bc27bec

▶ 109 秒の使い方紹介（英語音声）· [中文版](https://github.com/user-attachments/assets/f2b21f2f-da61-4832-9113-158efeb8f4ec) · [プロジェクトページ](https://forsakesoul.github.io/herdr-nakama/)

## 図でわかる Nakama（英語版）

![herdr-nakama の概要（英語）：Owner 1 名、Worker 1〜4 名、任意の Advisor 0〜4 名が役割に沿って協働します。Nakama は人数を選んでのチーム作成、初期化、状態確認、再起動後の再バインド、タスク途中のメンバー追加・削除・交代を担います。](docs/media/nakama-overview-en.png)

各メンバーは同じ Herdr タブ内の独立した pane で動作し、役割分担はプロンプトと共有の協働プロトコルで定められます。複数の Worker はそれぞれ重ならない範囲だけを変更します。具体的な操作と復元条件は以下を参照してください。

## なぜ「Nakama」なのか

*Nakama*（仲間）は『ONE PIECE』でルフィが船員を呼ぶ言葉です。部下でも道具でもなく、それぞれが航海の一部を担い、互いに背中を預け合う仲間。それがこのプラグインの作るチームの形です：

- **Owner：船長。** 航路を決めます。調査し、引き継ぎパッケージを確定し、結果を受け入れます。プロジェクトのコードは書きません。
- **Worker：手を動かす船員。** コードを変更するのは Worker だけで、完了・ブロック・コンテキスト不足のときに自分から報告します。
- **Advisor：航海士。** 任意。方針を検証し、diff を独立にレビューします。

仲間はそれぞれ自分の pane と自分のエンジンで航海し、共有プロトコルがその会話の仕方です。確定した引き継ぎ、NOTICE、誰も他人の代わりに答えないこと。嵐（コンピューターや Herdr の再起動）で船員が散り散りになっても、`nakama rebind` がビブルカードのように一人ひとりを見つけ出します。新しいチームではなく、元の一味のままで。

この名前はオマージュであり、提携を示すものではありません。本プロジェクトは『ONE PIECE』、尾田栄一郎氏、集英社、東映アニメーションとは一切関係がありません。

## インストール

macOS と Herdr 0.9.3 以降が必要です。下のインストールコマンドは手元でプラグインをビルドするため、Go 1.24 以降が必要です。Go がない場合は、その後のリリースパッケージを使ってください。

```sh
# 1. インストール（manifest のビルドが実行されます）。ローカル開発時は：go build … && herdr plugin link "$PWD" --enabled
herdr plugin install forsakesoul/herdr-nakama
# 2. 非公開設定：存在しない場合のみ config.example.json から生成（0600）し、必要に応じて pi_provider / cc_env_file を記入（非公開、コミット禁止）
cfg="$(herdr plugin config-dir forsakesoul.nakama)/config.json"
[ -e "$cfg" ] || (umask 077 && mkdir -p "${cfg%/*}" && curl -fsSL https://raw.githubusercontent.com/forsakesoul/herdr-nakama/main/config.example.json -o "$cfg.tmp" && mv "$cfg.tmp" "$cfg")
# 3. キーバインドを ~/.config/herdr/config.toml に追記して設定を再読み込み
herdr server reload-config
```

### Go なしで：リリースパッケージを使う

リリースパッケージにはビルド済みのバイナリが含まれるため、Go も clone も不要です：

```sh
arch="$(uname -m | sed 's/x86_64/amd64/')"
dir="$HOME/.local/share/herdr-nakama"
mkdir -p "$dir" && curl -fsSL "https://github.com/forsakesoul/herdr-nakama/releases/latest/download/nakama-darwin-$arch.tar.gz" | tar -xz -C "$dir" --strip-components=1
herdr plugin link "$dir" --enabled
```

バージョンを固定するには `latest/download` を `download/v0.0.3` に置き換えます。アップグレードは同じブロックを再実行します。v0.0.3 から提供されます。

キーバインド例（`config.toml`）：

```toml
[[keys.command]]
key = "prefix+u"
type = "plugin_action"
command = "forsakesoul.nakama.pick"
description = "nakama: エンジンを選んで現在の pane をアンカーにチームを作成"

[[keys.command]]
key = "prefix+i"
type = "plugin_action"
command = "forsakesoul.nakama.init"
description = "nakama: init（現在のアンカーのチームを初期化）"
```

## クイックスタート

1. **チームを作る**：プロジェクトディレクトリの pane で `prefix+u` を押し、Owner・Worker・Advisor のエンジンと、Worker / Advisor の人数を順に選びます（Enter で既定値、Advisor は `none` も可）。確認すると、プラグインが現在のタブ内にメンバーごとの pane を分割してエージェントを起動します。
2. **初期化する**：各 pane のエージェントが起動したら（承認ダイアログがあれば先に処理して）、`prefix+i` を押します。各メンバーに役割の説明と協働ルールが届きます。
3. **作業を始める**：Owner の pane で要件を伝えます。Owner がタスクを分けて Worker に渡し、Worker は完了したら報告し、Advisor は依頼に応じてレビューします。

チームの状態を確認：`herdr plugin action invoke status --plugin forsakesoul.nakama`。

## タスク途中でのメンバー変更

チームのいずれかの pane でもう一度 `prefix+u` を押すと、そのチームの管理メニューが開きます：

| キー | 動作 |
|---|---|
| `n` / `a` | Worker / Advisor を追加 |
| `r` / `x` | Worker / Advisor を削除 |
| `w` / `v` | Worker / Advisor を交代（エンジンの変更や、進まないメンバーの入れ替え） |
| `c` | タスク記録を削除 |

- 追加または交代のあとは、新しいメンバーの起動を待ってからもう一度 `prefix+i` を押します。新しいメンバーが初期化され、ほかのメンバーには最新のメンバー一覧が届きます。
- 新しいメンバーは現在のタブ内にのみ開きます。Worker は最低 1 名を残し、Worker と Advisor はそれぞれ最大 4 名、Owner は交代できません。
- 削除または交代されたメンバーの pane は残るので、自分で閉じてください。Worker を交代させる前に、旧 Worker が書き込みを止めたことを確認してください。新旧の Worker が同じ範囲を同時に編集するのを防ぐためです。
- 同じロールのメンバーが複数いる場合、削除や交代の前にメンバーキー（例：`W2`）の入力を求められます。

## 再起動後の復元

コンピューターまたは Herdr を再起動すると各 pane の terminal ID が変わるため、チームを使い続けるには再バインドが必要です：

```sh
nakama rebind --team <id> --preflight      # 読み取り専用：変化した点を表示
nakama rebind --team <id> --confirm-anchor <新しいアンカーの terminal>
```

プリフライトの出力に従って確認引数を追加します。元のチームがそのまま戻り、各メンバーの会話も保たれます。拒否条件と部分的に失敗した場合の対処は[リファレンス](docs/reference.ja.md)を参照してください。

## タスク記録

各チームはプロジェクト内に `.nakama/<チームID>/` ディレクトリを持ち、引き継ぎ、報告、レビュー結論を保存します。既定で Git の対象外です。タスクが終わっても自動では削除されません。あなたが許可したときだけ Owner が削除し、管理メニューの `c` や `nakama clean` で自分で削除することもできます。

## 設定

設定ディレクトリ：`$HERDR_PLUGIN_CONFIG_DIR`（Herdr が注入）、なければ `${XDG_CONFIG_HOME:-$HOME/.config}/nakama`。
ファイル：`config.json`。**実際の provider / パス / 認証情報をリポジトリにコミットしないでください。**

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

- エンジンは `<launcher>/<model>[:effort]` の形式か、`engine_map` に定義した短縮名で指定します。
- `projects` では、リポジトリごとにチーム名の接頭辞と、初期化時に全メンバーへ渡すプロジェクト背景ファイルを設定します。
- 画面とプロンプトの言語は `NAKAMA_LANG=en|zh|ja` で決まり、未設定の場合はシステムのロケールに従います。Herdr を起動する環境で設定してください。
- ランチャーごとの詳細（`pi` の provider、`ccenv`、Claude モデルのプリセット）は[リファレンス](docs/reference.ja.md)にあります。

## コマンド

```sh
nakama up [--owner E] [--worker E] [--workers N] [--advisor E|none] [--advisors N] [--pane P] [--cwd C]
nakama init --team <id>
nakama status [--team <id>]
nakama rebind --team <id> [--preflight | --discard-pending] [--confirm-session <メンバーキー|anchor>=<session>]... [--confirm-anchor <terminal>]
nakama add --team <id> [--role worker|advisor] [--engine E]
nakama drop --team <id> [--role worker|advisor] [--instance N]
nakama swap --team <id> --role worker|advisor [--instance N] [--engine E]
nakama clean [--team <id>] [--task <task_id>]... [--all] [--yes] [--force]
```

## アップグレード

アップグレード前に作成したチームは、会話としてはそのまま使えます。新しいバージョンで共有プロトコルが変わった場合、それらのチームの初期化やメンバーの追加・交代は拒否されるので、チームを作り直してください。

## その他

- [リファレンス](docs/reference.ja.md)：設定の全項目、コマンドの詳細、状態と安全性、再起動後の復元に関する全ルール、プロトコルの出典、テスト、既知の制限。
- [共有プロトコルとロール用プロンプト](prompts/ja/)：各ロールが実際に受け取る内容。
- [プロジェクトページ](https://forsakesoul.github.io/herdr-nakama/)

## 謝辞

- [Herdr](https://herdr.dev)（[herdrdev/herdr](https://github.com/herdrdev/herdr)、Apache-2.0）：Nakama が動作するコーディングエージェント向けターミナルマルチプレクサ。pane、エージェント、改名の操作はすべてその CLI を経由します。
- [motion-use](https://github.com/leeguooooo/motion-use)（MIT）：使い方動画の構成とレンダリングに使用。字幕・画面テキスト・ナレーションの検査も含みます。
- [Azure AI Speech](https://azure.microsoft.com/products/ai-services/ai-speech)：動画ナレーションのニューラル音声合成。

## ライセンス

[MIT](LICENSE)
