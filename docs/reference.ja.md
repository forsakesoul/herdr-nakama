# Nakama リファレンス

[English](reference.md) | [简体中文](reference.zh-CN.md) | 日本語

このページには、設定・コマンド・状態・復元に関する完全なルールをまとめています。問題を調査する場合や、実装上の制約を知りたい場合に参照してください。はじめての方は [README](../README.ja.md) をご覧ください。

## エンジン選択画面と `up`

対象 pane で `prefix+u` を押すとエンジン選択画面（overlay）が開きます。Owner のエンジン、Worker のエンジン、Worker 数（1〜4）の順に選び、続けて Advisor のエンジンを選びます。Advisor が `none` でない場合のみ Advisor 数（1〜4）を入力します（Enter で既定値。`engine_map` の短縮名か完全な `<launcher>/<model>[:effort]` を入力でき、Advisor は `none` も可）、概要を確認すると、キーを押した時点でフォーカスされていた pane をアンカーとしてチームを作成します。`Ctrl-C` でキャンセル（確認前は何も作成されません）。その pane があるチームのアンカーまたはロール pane である場合は、そのチームの管理メニュー（Worker / Advisor の追加・削除・交代、タスクディレクトリの削除。「途中でのロールの追加・削除・交代」参照）を表示します。在籍インスタンスのエンジンは固定のままで、モデルを変えるにはインスタンスを交代させます。新規作成のフローは新規作成専用です。確認中にその pane にチーム（作りかけを含む）が現れた場合も拒否し、復旧や引き継ぎはしません。作成中にシグナルを受けると、実行中の操作を永続化してから非ゼロで終了し、結果画面には留まりません。エージェントの起動を確認したら `prefix+i` で初期化します。結果は `herdr plugin action invoke status --plugin forsakesoul.nakama` または `herdr plugin log list --plugin forsakesoul.nakama` で確認できます。

> `up` アクションは従来の動作（`default_profile` に従ってチームを作成、または CLI で `--owner/--worker/--advisor` を指定）を維持し、作りかけのチームの再開にも使います。シェルからの `herdr plugin action invoke up/init/pick` は呼び出し元の pane を渡さず、UI で現在フォーカスされている pane をアンカーにするため、`pick` / `up` / `init` はキーバインドで（または対象 pane にフォーカスがある状態で）実行してください。

## 必要環境

- macOS
- Go ツールチェーン（バイナリのビルド用。go.mod は 1.24 を宣言）、`git`
- Ruby / YAML / jq は不要（共有プロトコルとロールプロンプトは `prompts/<lang>/*.md`）

## ディレクトリ構成

```
herdr-plugin.toml       manifest（actions: up / init / status / rebind / pick、pane: picker）
cmd/nakama/      実装（package main：config / engine / state / lock / identity / prompts / team …）
bin/nakama       ビルド成果物（コミットしない。下記参照）
prompts/{en,zh,ja}/      言語ごとの protocol.md + owner.md / worker.md / advisor.md
config.example.json     設定例（個人の provider / 認証情報は含まない）
cmd/nakama/*_test.go  go test（テストバイナリ自身が偽の herdr として argv を JSON 行で記録）
tests/fixtures/         実際の Herdr 0.9.3 の出力（匿名化済み）
```

## ビルド、インストール / リンク

manifest の `[[build]]` は `herdr plugin install` 時のみ実行されます。`herdr plugin link` は登録のみで**ビルドしません**（Herdr 0.9.3 `plugin.rs`）。ローカルでリンクする前に同じビルドコマンドを手動で実行してください：

```sh
go build -trimpath -o bin/nakama ./cmd/nakama
herdr plugin link /path/to/nakama
herdr plugin action list --plugin forsakesoul.nakama
```

`up` / `init` には pane コンテキスト（`contexts = ["pane"]`）が必要です。`status` はグローバルかつ読み取り専用です。

メンテナー向け：リリースするには PR で `herdr-plugin.toml` の `version` を変更します。PR が `main` にマージされると `.github/workflows/release.yml` が `v<version>` のリリースを作成し、パッケージを自動で添付します。手動でタグを打つ必要はありません。

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

- **言語**：CLI の出力とエージェントのプロンプトは `NAKAMA_LANG=en|zh|ja` で選び、未設定なら `LC_ALL` / `LC_MESSAGES` / `LANG`（`zh*`、`ja*`）の順に参照し、それ以外は英語です。この変数はプラグインのプロセスが読むため、Herdr を起動する環境で設定してください。`up` と `init` では同じ言語を使ってください。言語ごとにプロンプトが異なるため、途中で切り替えると protocol ハッシュのずれとして拒否されます。
- 優先順位：CLI > `default_profile` > 組み込みの中立な既定値（設定がなければエラー。個人の provider は組み込まない）。
- **`auto_init`**：`default_profile.auto_init: true` の場合、`up`（新規作成したチーム）と `add` / `swap` は、起動に成功すると自分のロックを先に解放してから続けて `init` を実行します。既定ではオフです。2 段階に分けているのは、prompt を送る前に各 pane のモデルと権限を確認できるようにするためです。`up` で再開した半端なチームは自動では初期化されません。
- エンジン記法：`<launcher>/<model>[:effort]`、または `engine_map` の短縮名。
- `cc` でモデルを省略すると `claude-opus-5-5` + `medium`（`cc --model claude-opus-5-5 --effort medium`）。モデルを指定し effort を省略した場合は `--effort` を渡さず、Claude 自身の既定値に任せます。
- **provider**：`pi` ロールの provider は `default_profile.pi_provider` から取得します（非公開、コミット禁止）。未設定なら `up` は split の前に拒否します。
- **`ccenv` ランチャー**：pane 内で env ファイルを `source`（例：`ANTHROPIC_BASE_URL` などで自前のゲートウェイを指定）してから `cc --model <model>` を起動します。`model` は必須で effort は受け付けません。`default_profile.cc_env_file` はこのマシン上の通常の読み取り可能なファイルを指す必要があり、`up` の前に検査されます（`-f` かつ `-r`。内容は読まず表示もしない）。ディレクトリは拒否します。source に失敗した場合（非ゼロの `return` を含む）は非ゼロのまま `cc` を起動しません。パスは**該当 pane のコマンド文字列**に現れます（パスは秘密情報ではありません）が、env ファイルの**内容**がコマンド / 状態 / プロンプト / ログに入ることはありません。
- `projects` テーブルはコンテキストファイルのパスを**参照**するだけで、本文はコピーしません。ファイルが存在しない / 読めない場合 `init` は停止します。
- `init` の前に `protocol.md` とコンテキストファイルの現在の sha256 が記録値と一致することを確認します。ずれていれば拒否し（新しいチームを `up` するよう案内）、ハッシュを自動更新することはありません。

### Claude の日常編組

[Claude モデル選択ガイド](https://claude.dev/blog/which-claude-model-should-you-build-with/)（2026-11-09）を参考に、Owner と Advisor は設計判断と審査を担う Opus / medium、日常の Worker は本プロジェクトの方針で Haiku / medium とします。ガイドは範囲が明確な実装に Sonnet を推奨しています。ここでは Haiku から始めても検証を省略せず、複雑な作業では編組前に Sonnet / Opus を評価します。

`config.example.json` の `engine_map.claude` を上記の Herdr プラグイン設定に追加します。対象 pane で `prefix+u` を押し、Owner / Worker / Advisor にそれぞれ `claude` を入力して、概要を確認して編組します。起動確認後に `prefix+i` で初期化します。プラグインの選択画面を使い、設定と状態のディレクトリを一致させます。

任意のプリセットであり、既存のロール既定値やプライベート設定は変更しません。`cc` は Opus 5.5 / medium のまま、Worker は `cc/haiku:medium` に展開されます。`haiku` 別名はローカルの Claude Code に従うため、起動後に実際のモデルと effort を確認します。独立審査が不要なら Advisor 欄に `none`、より強い実行モデルが必要なら Worker 欄に `cc/sonnet:medium` または `cc/opus:medium` を入力します。`ccenv` は引き続き effort を受け付けません。ゲートウェイや Pi のモデル、引数と可用性は実際の provider で確認します。

失敗時の検証、モデル昇格と調整の承認は[共有プロトコルの Claude ルール](../prompts/ja/protocol.md)に従います。在籍インスタンスのエンジンは固定したままとし、調整が必要なら安全な停止点で引き継いでそのロールのインスタンスを交代させるか（「途中でのロールの追加・削除・交代」参照）、プロトコルに従って新しい pane に編組を作成します。

共有プロトコルの内容を変えると、その言語の `protocol_version` が変わります。このバージョンは `protocol.md` を変更したため、それ以前に作成された編組は init / add / swap で拒否されます。作り直してください。旧編組の初期化時に hash が一致しなければ、新しい編組を作成し、状態を書き換えて回避しません。既存ロールのコンテキストは自動更新されません。

## 使い方

```sh
nakama up [--owner E] [--worker E] [--workers N] [--advisor E|none] [--advisors N] [--pane P] [--cwd C]
nakama init --team <id>
nakama status [--team <id>]
nakama rebind --team <id> [--preflight | --discard-pending] [--confirm-session <メンバーキー|anchor>=<session>]... [--confirm-anchor <terminal>]
nakama add --team <id> [--role worker|advisor] [--engine E]
nakama drop --team <id> [--role worker|advisor] [--instance N]
nakama swap --team <id> --role worker|advisor [--instance N] [--engine E]
nakama clean [--team <id>] [--task <task_id>]... [--all] [--yes] [--force]
nakama send --team <id> --task <task_id> --to <メンバーキー> --title <タイトル> --file <本文ファイル> [--status DONE|BLOCKED|CONTEXT_READY] [--responds-to <req>] [--reply sync|async] [--set key=value]... [--no-deliver] [--from <メンバーキー>]
nakama send --team <id> --resend <req> [--to <メンバーキー>] [--from <メンバーキー>]
nakama down --team <id> [--yes]
nakama resolve --team <id> --delivered | --not-delivered
```

- 新しいチーム名：`<プロジェクト短縮名 ≤8>_<12hex>`、一致するプロジェクトがなければ `task_`。サフィックスはナノ秒タイムスタンプと 6 バイトの暗号学的乱数を混ぜ、SHA-256 の先頭 6 バイトから生成し、外部ツールが認識できる固定形式を保ちます。ロール名は `<prefix>_design/_execute/_advisor`、全長 ≤32。既存チームは元の名前を使い続けます。
- アンカーの pane/cwd は注入されたコンテキスト（`HERDR_PLUGIN_CONTEXT_JSON` / `HERDR_PANE_ID`）から取得し、`herdr pane get/current` で読み取り専用に検証します。**プラグインルートの `$PWD` をプロジェクトディレクトリとして使うことはなく**、特定できなければ拒否します。
- すべての Herdr 呼び出しは `HERDR_BIN_PATH`（既定 `herdr`）を経由し、argv は引数ごとに渡します。eval やシェル文字列の連結はしません。
- `split` は常に `--pane/--cwd/--no-focus` を明示します。
- 起動後にロールが認可 / 信頼ダイアログで止まった場合、`up` はその pane で認可されるのを待ちます。各ロールの 1 回の取得には待機上限 `NAKAMA_START_WAIT_SECONDS`（既定 600 秒）が 1 つだけあり、待機中は入力を送りません。
- `init` のアンカーはチームの任意のロール pane で構いません。ロール pane にフォーカスがある場合は pane から所属チームを探し、呼び出し元がまだそのチームの該当ロールインスタンスであることを先に検証します（読み取り専用。prompt/rename の前に拒否）。複数のチームに一致した場合はエラーとなり `--team` の指定が必要です。`up` は引き続きアンカー pane のみで既存チームを判定します。`pick` はアンカーでもロール pane でもそのチームの管理メニューを開きます。

## 途中でのロールの追加・削除・交代

チームの初期化後に、Worker / Advisor を追加・削除・交代させられます（止まってしまったものも含みます）。Owner は交代できません。チームは Owner 1 つ、Worker 1〜4 つ、Advisor 0〜4 つで構成されます（既定は Worker 1。Advisor エンジンが `none` でなければ Advisor 1、`none` なら 0）。人数の上限は同時に在籍するメンバーに適用され、インスタンス番号には適用されません。同じロールに複数のメンバーがいる場合、`swap` と `drop` には `--instance N` が必要です。新しい pane は常にアンカーと同じ tab 内で split します。確認できない場合は操作を止め、tab やウィンドウは作りません。

- **入口**：アンカーまたは任意のロール pane で `prefix+u` を押すと、既存チームでは管理メニューが開きます（`a` Advisor を追加、`n` Worker を追加、`x` Advisor を外す、`r` Worker を外す（最後の 1 つは不可）、`v` Advisor を交代、`w` Worker を交代、`c` タスクディレクトリを削除。同じロールに複数のメンバーがいる場合はメンバーキー（例：`W2`）を入力し、CLI の `--instance` は番号（例：`2`）を使います）。エンジンを選んで確認すると実行されます。CLI では `nakama add / drop / swap --team <id>` です。
- **再バインドと解散**：同じメニューで、`b`（再バインド）はアンカーまたはいずれかのメンバーが再起動後の候補に見えるとき、あるいは未完了の再バインド計画があるときだけ表示され、`d`（解散）は常に表示されます。「コンピューターまたは Herdr の再起動後にチームを復元する」と「チームの解散」を参照してください。
- **2 段階**：`add` / `swap` は pane を作成して新しいインスタンスを起動するだけです。モデルと権限を確認したら `prefix+i` を押します。`init` が新しいロールを初期化し、ほかの在籍ロールに `[TEAM_UPDATE]`（最新のマッピングとそのロールの新しい分担規則）を 1 回ずつ配信します。`drop` は新しいインスタンスがないため、すぐに配信します。
- **アイドルのロールにだけ通知**：`[TEAM_UPDATE]` は識別の検証に通り、idle/done のロールにだけ送ります。作業中やダイアログで止まっているロールには未通知の印が残り（`status` に `team_update=pending` と表示）、アイドルになってからもう一度 `prefix+i` を押します。
- **旧インスタンス**：交代または削除されたインスタンスは状態の `retired` に記録されます。その pane は閉じず、名前も変えず、プラグインからの入力も送りません。Worker を交代させる前に旧 Worker が書き込みを止めたことを確認し、その pane はご自身で処理してください。
- **インスタンス番号は再利用しません**：新しいインスタンスは `W2`、`A2`…、名前は `<prefix>_execute2`、`<prefix>_advisor2`… です。最初のインスタンスは元の名前のままです。
- **前提条件**：チームが `initialized` で、未確認の副作用や未完了の rebind がなく、`protocol.md` とコンテキストファイルの hash が変わっていないこと。プロトコル変更前に作成したチームは変更できないため、新しいチームを作成してください。
- **中断からの復旧**：変更が途中で失敗するとチームは `phase=changing` のままになり、`init` は拒否します。確認のうえ、同じコマンドまたは `up` を再実行して完了させます。

## タスクディレクトリと削除

新しく作成したチームにはタスクディレクトリ `<プロジェクトディレクトリ>/.nakama/<チーム ID>/` があり、そのパスは状態に記録され、各ロールの初期化プロンプトに書き込まれます。`.nakama/.gitignore` の内容は `*` で、ディレクトリ全体が `git status` や diff に現れません。

- **用途**：メンバー間のメッセージは、ここに 1 件 1 ファイルで保存されます。`nakama send` は初回に `<task_id>/` サブディレクトリを作り、各メッセージを `<メンバーキー>-<連番>.md`（例：`O1-000.md`、`W1-003.md`）として書き込みます。先頭にメッセージヘッダーと関連フィールド、その後に本文全体が入ります。配信の試行はすべて `ledger.jsonl` に記録されます。メッセージファイルは書き込み後に変更されません。途中で参加または交代したロールは、ここから最新の引き継ぎを読みます。「メンバー間のメッセージ」と[共有プロトコル](../prompts/ja/protocol.md)を参照してください。
- **自動では削除しません**：タスクの完了は削除の許可ではありません。Owner は納品時にディレクトリのパスを 1 回報告し、削除してよいと明示された後にのみ、プロンプトにある削除コマンドを実行します。
- **自分で削除する**：管理メニューの `c`、または `nakama clean`。選択を指定しないとすべてのタスクディレクトリ（ファイル数、サイズ、最終更新）を一覧し、端末では番号で選んで確認できます。`--team <id> --task <task_id>` または `--all` で直接選択でき、非対話環境では `--yes` が必要です。
- **誤削除の防止**：状態に記録されたタスクディレクトリ直下のサブディレクトリだけを削除し、シンボリックリンクはたどりません。そのチームの Worker または Advisor（停止済みインスタンスを含む）が working/blocked の間、または状態を確認できないときは既定で拒否し、`--force` を付けたときだけ削除します。リクエストが決着しているかどうかは Owner がプロトコルに従って判断し、プラグインにはわかりません。
- **以前のチーム**：このバージョンより前に作成したチームにはタスクディレクトリがなく、従来どおり本文をインラインで送ります。

## メンバー間のメッセージ

各ロールは業務メッセージを手書きの `herdr agent prompt` ではなく `nakama send` で配信します。状態ディレクトリとチーム ID を埋めた完全なコマンドラインは、各ロールの初期化 prompt に含まれます。

- **1 回の送信で行うこと**：タスクディレクトリにある既存のファイルからリクエスト番号 `<task_id>/<メンバーキー>-<連番>` を割り当て、メッセージファイルを書き込み、受信者の固定済みの識別を検証してから、ヘッダー、関連フィールド、`file=<パス>` だけを含む prompt を配信します。`--reply sync` は最大 120 秒待ち、受信者の直近の出力を表示します。
- **受信者が利用できない場合**：入力を受け取るのは idle/done の受信者だけです。working の場合は最大 60 秒を 1 回だけ待ちます。それでも working、blocked、unknown のまま、または識別がずれている場合は何も送らず、`delivery=NOT_SENT` を出力して非ゼロで終了します。メッセージと番号はディスクに残り、後から `--resend <req>` で同じメッセージを配信できます。
- **配信状態**：`SENT`、`NOT_SENT`、`UNKNOWN`（prompt コマンドが失敗し、入力が届いたかどうかを証明できない状態。再送の前に `herdr agent read` で確認します）、`SYNC`（`--no-deliver`：記録するだけで逆向きには配信しない同期の回答）。prompt を送る前に `UNKNOWN` として記録するため、プロセスが強制終了されても、届いた可能性のあるメッセージが未送信として残ることはありません。
- **代理転送**：作成者以外のメンバーが `--resend <req> --to <メンバー>` を実行すると、メッセージはそのまま転送され、ヘッダーに `relayed_by` が加わります。
- **送信者**：pane が `HERDR_PANE_ID` と一致するメンバーです。`--from` はその変数が得られない場合にだけ必要で、両方ある場合は一致していなければなりません。
- **`status`**：台帳のあるタスクごとに、メッセージ数と未決着の項目を表示します。`DONE` の回答がまだないリクエスト（`WAITING`、`BLOCKED`、`PAUSED`）と、最後の配信が `NOT_SENT` または `UNKNOWN` のメッセージです。この表示は台帳から導いたもので、リクエストが決着したかどうかは引き続き各ロールがプロトコルに従って判断します。
- **ロックなし**：`send` はチーム状態を読むだけで、team ロックを取りません。リクエスト番号は排他的なハードリンクで割り当てるため、中断された `send` がチーム全体を止める残留ロックを残すことはありません。
- **タスクディレクトリがない場合**（`.nakama` のパスがシンボリックリンク）：`send` は実行を拒否し、各ロールはプロトコルの記述どおり本文のインラインに戻ります。

## チームの解散

`nakama down --team <id>`（または管理メニューの `d`）は、チームの状態ファイルを `$HERDR_PLUGIN_STATE_DIR/archive/<id>.json` に移します。以後、アンカー pane で新しいチームを作成でき、`init` / `send` / `rebind` / `status` から古いチームは見えなくなります。

- pane を閉じたり名前を変えたりせず、どのメンバーにも入力を送りません。コマンドがメンバーの pane を一覧するので、自分で処理してください。
- 空のタスクディレクトリは削除されます。メッセージが残っているものは保持され、そのパスが表示されます。
- 実行前に確認を求めます。非対話環境では `--yes` が必要です。`--team` は必須で、pane から推測しません。
- 状態ファイルが検証を通らなくなった場合にも使えます。そうでなければ、その状態がアンカーを占有し続けます。

## 結果不明の配信を決着させる

`init` や `[TEAM_UPDATE]` が結果不明で終わった場合（例：`agent_prompt_stalled`）、チームは停止し、再送は行われません。pane を確認してから、見た結果をプラグインに伝えます。`nakama resolve --team <id> --delivered` は、識別を再検証したうえでそのロールを初期化済みとして（または更新を配信済みとして）記録します。`--not-delivered` はマークを消し、次の `init` で再送されます。該当する場合は `status` がこの案内を表示します。

- 対象は `prompt`、`team_update`、`rename` のステップだけです。`split` や起動の結果不明は pane とプロセスの帰属に関わるため推測せず、`down` で解散して作り直します。
- session が空で記録されたロールが現在 session を報告している場合、`--not-delivered` は拒否されます。Codex は最初の入力で session を作るため、それが prompt の届いた証拠になります。
- 起動後の識別取得の失敗には `resolve` は不要です。取得は読み取り専用なので、次の `up` / `init` がそのまま再試行し、`up` はまだ起動していないメンバーを続けて起動します。

## 状態と安全性

- 状態：`$HERDR_PLUGIN_STATE_DIR/teams/<safe-id>.json`（既定 `${XDG_STATE_HOME:-$HOME/.local/state}/nakama`）。ディレクトリ 0700、ファイル 0600、同一ディレクトリの一時ファイル + `os.Rename` によるアトミック置換。JSON は解析するだけで実行はしません。必須フィールドの欠落 / null は常に不正とし、Go のゼロ値を有効値とみなしません。
- ロック：`mkdir` ロック（flock は使わない。プロセス終了時の自動解放は「残留ロックを自動で引き継がない」原則を壊すため）。ロック順は anchor → team に固定。新規チームは発見可能な状態を公開する前に team ロックを取得してトランザクション終了まで保持し、既存チームへのすべての書き込みは同じ team ロックを使います。**残留ロックは自動で引き継がず**、エラーで手動確認を促します。ロック内で状態と pending を読み直してから判断し、このプロセスが取得した完全なパスのみを解放します。
- インスタンス識別：各ロールの起動 / 検証が成功すると `terminal|pane|kind|session` を永続化します。`init` / 再入 / `rename` / `prompt` の前に `agent get` で再検証し、pane / 識別フィールドの欠落やずれがあれば拒否します。識別の比較は単一の実装で terminal/pane/kind/session を完全に比較します（session の空 ↔ 非空の変化もずれとみなす）。手動ランチャー（`cc`/`ccenv`/`pi`）は `rename` の前に pane の現在のインスタンスが起動時の観測と一致することを確認し、後から入った別のインスタンスを `rename` しません。`rename` 後は同じ基準（起動時の観測または固定済み identity）で完全なフィンガープリントを再確認し、通過して初めて固定して prompt を許可します（`rename` 自体は取り消せないため、ずれた場合も実行済みの 1 回の `rename` は残ります）。
- 副作用の順序：各 `split`/`pane run`/`rename`/`prompt` の前に `pending` を書き込み、戻った後に更新します。`split` が成功したら即座に pane を永続化します。再入時はまず検証し、`UNKNOWN` なら再送せず停止します。「起動前の `agent_pane_busy`」のみ有限回リトライします。
- `timeout`/`agent_prompt_stalled` となった初期化は「送信済み・未確認」として記録され、`status` が手動での `get/read` を案内します。
- 起動時の取得では、各観測（初回を含む）が `blocked`（認可 / 信頼ダイアログ）または `unknown` の場合、同じ待機予算内で再観測し、その状態を抜けてから固定します。`start` が `agent_not_ready`（名前は確保済み）を返した場合も同じ待機に入ります。予算は 1 回の取得あたり `NAKAMA_START_WAIT_SECONDS`（既定 600 秒）で、途中で `unknown`/`blocked` が切り替わってもリセットされません。タイムアウトは起動失敗として扱い、認可待ちのタイムアウトであることを示します。待機中は観測のみで入力は送らず、各ロールにつき 1 回だけ通知します。cc/ccenv/pi の待機時間は `NAKAMA_START_POLL_TRIES` の session ポーリング枠に含めません。
- `init` の `prompt` が `agent_blocked` を返した場合、入力は送信されていません。pending をクリアして非ゼロで終了するので、ダイアログを処理してから `prefix+i`（または `init`）を再実行してください。これは UNKNOWN ではありません。
- 手動ランチャー（cc/ccenv/pi）の session は、プロセスが認識された後に SessionStart フックによって報告されます。起動時の観測は同じ有限ポーリング（`NAKAMA_START_POLL_TRIES`）内で session が現れるのを待ってから永続化し、初回の固定で空 → 非空をずれと判定しないようにします。最後まで報告されなければ空として記録します。
- 固定時に session が空だったロール（codex は最初の prompt 後に session を報告）：`init` の再入時の再検証で、観測された非空の session を identity に一方向で補完できます（terminal/pane/kind は完全一致が必要）。補完後の変化はずれとみなします。この例外は inited 済みで stale でない再入ロールにのみ適用されます。stale になったロールは自動でクリアされず、手動対応（新しいチームを up）が必要です。既知の限界：補完前に同じターミナル・同じ kind の新しいインスタンスに置き換えられた場合は区別できません。
- SIGHUP/SIGINT/SIGTERM を受けると、ディスパッチのチェックポイントを通過済みの Herdr 操作 1 件は完了して永続化され、その後は新しい副作用を開始しません。コマンドは非ゼロで終了し、ロックは通常の終了経路で解放されます。SIGKILL は処理できず、残留ロックは「自動で引き継がない」原則に従いフェイルクローズするため、手動確認の上で削除が必要です。
- `up` の再入は `starting`（新たな書き込みが発生する）経路でのみ protocol/context のハッシュを確認します。launched/initialized 経路は Herdr への書き込みを追加せず、ハッシュは `init` が確認します。
- ユーザーの既存 pane を閉じたり改名したりせず、実行中の古いチームを引き継ぐこともありません。

## コンピューターまたは Herdr の再起動後にチームを復元する

**何が起きるか。** Herdr（またはコンピューター）の再起動後、各ロール pane の `pane_id` と kind は通常維持されますが、`terminal_id` はすべて変わります（アンカーも同様）。
タスク名はクリアされるか、外部ツール（Bridge）によって `<プロジェクト slug>-<terminal ダイジェスト>` に自動改名されることがあります。claude の会話は通常同じものが続き、codex の session は最初の prompt 後にのみ現れます。
この時 `status` ではロールが見えますが、`init` / `prompt` の terminal と名前のチェックが拒否するため、チームはランタイムから使えなくなります。

**手順。**
1. まず読み取り専用のプリフライト（書き込みゼロ）を実行：`nakama rebind --team <id> --preflight`。ロールとアンカーごとに `terminal 旧 → 新`、session とタスク名の差分を出力します。
2. 出力に従って確認引数を加えて実行：`nakama rebind --team <id> --confirm-anchor <新しいアンカーの terminal> [--confirm-session <メンバーキー|anchor>=<session>]...`。メンバーキーは O1、W1、W2、A1… の形です（例：`--confirm-session W2=<session>` は 2 番目の Worker の session を確認します）。
   - `--confirm-session` は記録された session が空のロール（例：最初の prompt 前に固定された codex）専用で、値は今回観測された非空の session と一致する必要があります。
   - `--confirm-anchor` は identity を持たないシェルアンカー専用で、値は今回観測された新しい terminal と一致する必要があります。
   - 記録された session が非空のロールは確認による上書きを受け付けず、観測値が記録と一致する必要があります。
3. 再バインド不要（識別と名前がすでに一致）の場合は `再バインド不要` を出力し、状態を書き込みません。繰り返し実行しても完全な no-op で、`team_rev` も監査記録も増えません。

**管理メニューから。** チームが再起動後の候補に見えるとき、メニューに `b` が表示されます。同じプリフライトを出力し、明示的な確認が必要な値を一覧し、確認後はまさにその値で `rebind` を実行します。`rebind` は改めて観測し、値が一致しなくなっていれば拒否します。

**拒否条件（いずれも rename ゼロ、状態書き込みゼロ）。** チームが `initialized` でない、pending がある、いずれかのロールが stale または未初期化。pane が読めない。
pane の workspace/tab/cwd が記録と異なる。kind が変わった。観測された session が空、または記録と異なる。タスク名が空でも Bridge の正確な自動名でもない（他者が使用中の可能性）。
対象のタスク名が他の pane に使われている。確認値が観測と異なる、または確認不要なフィールドを指している。拒否時は候補の差分を出力するだけで、新しい identity として書き込むことはありません。

**意味は「受け入れ」であり「証明」ではない。** 同じ pane + 同じ kind + 非空の session は、ユーザーが「同じ会話が新しい terminal で再開した」ことを明示的に受け入れたことを示すだけで、同一プロセスや同一の認証情報を証明するものではありません。
CLI では Herdr サーバーの名前空間を証明できません。アンカーの根拠は `pane get` の workspace/tab/cwd の連続性と pane id です。

**実行と中断。** プリフライト通過後、まず完全な計画（旧 / 新の識別、改名ステップ）を `rebind_pending` として永続化し、ロールごとに：改名前に再読込 → `agent rename` → pane とタスク名の両方向で再検証、
各ステップの結果を永続化します。すべての再検証が通過して初めて、現在の identity とアンカーの terminal（および `pane_terminal`）を一括でコミットし、`team_rev` を 1 増やし、pending をクリアして監査記録を追加します。監査（`rebinds`）には操作 id、時刻、結果（`committed` または `discarded`）、確認方法、各ステップの旧 / 新の完全な identity（session の確認前後の値を含む）、アンカーの旧 / 新の terminal を記録し、アンカーが identity を持つ場合はその旧 / 新の完全な identity も記録します。出力の抜粋は記録しません。
`launch_identity` は起動履歴として残し、現在の検証には使いません。途中で失敗・シグナル・不明な結果が発生した場合は pending と完了済みステップを残して非ゼロで終了し、**盲目的なロールバックは行いません**。
実行前とコミット前にはチーム全体を再検証します：安定したチームの条件、計画された各ロールの pane コンテキスト（workspace/tab/cwd）と完全な識別、アンカーのコンテキストと識別（アンカーが変化していない場合も同様）。完了済みのステップも再検証し、改名を再送しないだけです。実行または再開を始める前に、現在の観測が計画上の新しい識別と一致している必要があり、まだ旧識別のままなら停止し（再プリフライトまたは待機）、新たな rename はゼロで、部分的に改名された結果を成功とはみなしません。ずれを検出したら新たな rename の前に停止し、計画と現在の identity を保持します。永続化された計画がチームの記録とフィールド間で矛盾する場合（ステップの欠落や重複、存在しないロール、旧識別の相違、未確認の session 変化など）は破損とみなし、実行もコミットも拒否し、既定値で補完しません。

**部分的な失敗後の手動確認。**
1. `herdr agent get <pane>` と `herdr agent get <name>` で各ロールの pane、terminal、kind、session、タスク名を確認します。
2. 照合を継続できる場合は、元の確認引数を付けて `rebind --team <id>` を再実行します（計画で確認済みの値のみ指定でき、異なれば拒否）。
3. 識別がもはや計画と一致しない場合（例：再度再起動した）は、`rebind --team <id> --discard-pending` で計画を破棄します。識別の記録は変わらず、完了済みの改名は Herdr に残ります。その後もう一度 `--preflight` してから実行してください。
   計画はコミット前に現在の identity を一切変更しないため、破棄は安全です。次のプリフライトは現在の観測に基づいて改めて判断します。

**Bridge との競合。** Herdr の rename には compare-and-swap の意味論がないため、読み取りと改名の間に Bridge が自動命名する可能性を数学的に排除できません。
ずれを検出したら停止して計画を保持します。プラグインは Bridge を追いかけたり変更したりしません。成功しても名前が永久に変わらないとは限らず、その後の `init` / `prompt` は引き続き厳格なルールで検査します。

**状態の互換性。** このバージョンは `schema_version: 4` を書き込みます：ロールはメンバーキー（`O1`、`W1`、`W2`、`A1`…）で管理されます。schema 1〜3 の旧状態はメモリ上で変換され、初回保存時にのみ schema 4 として書き戻されます。schema 2 で `rebind_pending` と `rebinds`（監査）、schema 3 で `task_dir`、`retired`、ロールごとの `team_update_pending` を追加しました。旧バージョンはより高い schema を読むと拒否するため、
新しいフィールドを失った状態で上書きされることはありません。旧バージョンに戻す場合は、未完了の計画がないことを確認し、状態ファイルを自分で処理してください。schema 1〜3 は引き続き読み込め、初回の実際の書き込み時に 4 へ更新されます。

**自動では行われないこと。** `init` と `up` は再バインドを行いません。`init` は「terminal または名前だけが変わった候補」を検出すると `明示的な rebind --team <id> が必要な可能性` を案内するだけで、
stale にせず状態も書き込みません。`status` は読み取り専用で、記録された pane に基づいて観測し、`binding=bound|candidate|unverified` を出力して、ライフサイクルの `status` とバインドの有効性を区別します。`bound` は現在のタスク名が記録されたロール名と一致することも要求し、他者の名前や空の場合は最大でも `unverified` です。`candidate` は候補（同じ pane と kind で terminal または名前が変化）にすぎず、`rebind` がプリフライトを通過する保証はないため、まず `--preflight` で確認してください。

## プロトコルの出典

`prompts/<lang>/{protocol,owner,worker,advisor}.md` は共有プロトコルとロール本文の唯一の正本で、実行時に直接読み込まれます。`protocol_version` は選択した言語の `protocol.md` の sha256 です。中国語が原文で、英語と日本語は翻訳です。同じ `<!-- … -->` マーカーを保つ必要があります（テストで検査）。下流でこれらのテンプレートを再利用する場合は、このディレクトリから同期し、出典バージョンを各自で検証してください。手動 Skill の許可はプロジェクトのコンテキストファイルまたは依頼内容に記述し、プロトコル自体は Skill の許可リストを持ちません。

`protocol.md` では、`<!-- role:owner,worker -->` と `<!-- /role -->` の間の行は列挙されたロールにだけ送られ、それ以外はすべてのロールに送られます。`protocol_version` はこれまでどおりファイル全体の sha256 です。

アーキテクチャタスクでは、`architecture` Skill（ユーザー定義、プラグインには含まれません）がインストールされていればロールごとに読み込み、なければ元の引き継ぎ契約で続行してギャップを明記します。プラグインは Skill のインストール、ロール分担の変更、チームの派生を行いません。テンプレートの変更は既存ロールのコンテキストを更新しません。初期化が完了していない古いチームは元のプロトコルハッシュのずれ検査で保護されたままで、状態をクリアして回避することはできません。

ユーザーがいずれかのロールにコンテキスト圧縮を求めた場合は、インストール済みの [`long-task`](https://github.com/forsakesoul/awesome-skills/tree/main/long-task) Skill（ユーザー定義の圧縮 Skill、プラグインには同梱されません）の自動圧縮分岐を使います。本プラグインは既存の責務、一時停止 / 再開の許可、リクエスト状態との接続ルールのみを保持します。バックグラウンド制御は呼び出し元自身のセッションのみを対象とし、未インストールまたは制御プレーンが未対応の場合は復旧情報を保持してギャップを報告します。

## テスト

```sh
gofmt -l .
go vet ./...
go test ./...
go build -trimpath -o bin/nakama ./cmd/nakama
```

フィクスチャ `tests/fixtures/agent_get.json` / `pane_get_shell.json` は実際の Herdr 0.9.3 の出力（匿名化済み）です。通常のシェル pane にも `terminal_id` があり、`agent get` の `agent` フィールドが kind、`agent_session.value` が session です。

`plugin_context_invoke.json` は実際の `plugin.action.invoke` 応答の `context` です（注入される `HERDR_PLUGIN_CONTEXT_JSON` と同値であることをソースで確認済み。サンプルは invoke 応答由来で、子プロセスの環境ではありません）。その `tab_label` / `focused_pane_agent` は UI のアクティブタブによるフィールド単位の上書きで、明示的に渡した tab/pane とは一致しないため、プログラムは `focused_pane_id` のみを使います。`agent_start.json` は実際の `up` のアクションログにある native `agent start` の stdout（匿名化済み）で、サンプルとして残しているだけで解析はしません。

## 既知の制限

- Herdr サーバーの名前空間は CLI から直接証明できません。アンカーの根拠は `pane get` の `terminal_id` の連続性（init 時 / 既存チームで比較）で代替しており、本ドキュメントはその境界を記載しています。
- `rebind` の改名と Bridge の自動命名の間には解消できない競合ウィンドウがあります（「コンピューターまたは Herdr の再起動後にチームを復元する」を参照）。ずれを検出したら停止して計画を保持します。
- `rebind` の Herdr 呼び出しは偽 herdr ハーネスでのみ検証しています。実際の Herdr 0.9.3 での `agent rename` の戻り値と `plugin action invoke` の引数渡しは、このバージョンではサンプリングしていません。
- `send` と `down` は、Claude Code、Codex、Pi、Qoder のメンバーを使って実際の Herdr 0.9.5 上でエンドツーエンドに実行しました。どのエンジンも引き継ぎを受け取り、作業を行い、自分で `send` を実行して報告しました。ほかに、working の受信者に対する有限の待機とその後の `NOT_SENT`、`--resend`、`--no-deliver` で回答する同期リクエスト、30 件の並行送信も確認しています。実際の Herdr で採取していないもの：メニューからの再バインドと、起動直後の `auto_init` のタイミング。
- プラグインがメンバーの状態について知っていることは、すべて Herdr の状態検出に基づきます。Qoder CLI 1.1.89 は Herdr の検出マニフェストのどのルールにも一致せず、常に `idle` と報告されます。そのため作業中の Qoder にも prompt が届き、`--reply sync` や `init` は prompt が届いていても `agent_prompt_stalled` で終わることがあります（`resolve` で処理します）。Codex はターンの途中で一時的に idle と報告されたことがあるため、`send` は idle を 2 回続けて観測してから配信します。これは窓を狭めるだけで、なくすことはできません。
- Herdr が idle と報告する起動時のダイアログは入力を飲み込みます。pane の画面に `enter … · esc …` 形式のダイアログのフッターがあると `init` は拒否します（Codex でフォルダー信頼、フック確認など 3 種類を確認）。これは画面の文字との照合なので、ほかのダイアログは検出できません。Qoder のフォルダー信頼ダイアログには区別できるフッターがありません。また、起動途中の Codex が初期化 prompt を受け取ったのに返信しなかった例もあります。
- `send` は受信者の状態を確認してから配信します。2 つの送信者が同じ受信者を同時に idle と観測すると両方が配信し、2 つ目の prompt は受信者がすでに working のときに届きます。Herdr には「比較してから送信」の仕組みがないため、受信側は引き続き「同じ req は一度だけ進める」というプロトコルの規則に依存します。
- 手動ランチャー（pi/cc/ccenv）の初回固定には「split 時に記録した terminal_id == 最初の agent get の terminal_id」+「kind が期待する種類と一致」+「インスタンスが他者に命名されていない」が必要で、いずれかを満たさなければ UNKNOWN で停止します。唯一の名前互換は外部 Bridge の正確な自動名（プロジェクトディレクトリの slug + terminal_id の SHA-256 先頭 4 桁）で、完全な起動フィンガープリントが一致している間はロール名に置き換えられます。それ以外の名前、起動フィンガープリントの欠落、フィンガープリントのずれは引き続き拒否します。タイムスタンプは作成バッチの区別に使い、この互換は自動命名が init を妨げないためのものです。
- 作りかけのチームの補完は、pane の占有状態が `idle`（`agent_not_found` を確認済み）の場合にのみ起動します。`occupied` と `unknown`（get 失敗 / オブジェクト不完全）はいずれも起動前に非ゼロで終了します。
- `agent start` の応答構造はサンプリングしておらず、取得の根拠にしません。native の識別には常に有限回の `agent get` を使います。

## 未対応

popup / TUI、`startup` / `events`、Owner の交代、`sync`、古いチームの引き継ぎ、CI。
