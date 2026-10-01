![skillctrl](wordmark.svg)

# skillctrl

[![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](../LICENSE)

[English](../README.md) · [移植・公開の要件](requirements.md) · [CI での使い方](ci.md) · [対応表](parity.md)

agent skill を Git リポジトリで管理しながら、ローカルで加えた適応の「意図」を
失わないようにするための CLI です。

skill はどこか別の場所から導入したあと、その環境に合わせて手で編集します。
次の更新がその編集を上書きしても、何のために編集したのかの記録は残りません。
`skillctrl` はその理由を skill の隣に置きます。上流のリリースで編集済みの skill
が変わっても、原本で置き換えるのではなく自分の版に再適用し、実際に受け入れた
内容の hash を Git に記録します。

## インストール

```sh
# Go
go install github.com/wwwyo/skillctrl@v0.1.0

# mise (GitHub release backend)
mise use -g github:wwwyo/skillctrl@v0.1.0

# Homebrew
brew tap wwwyo/tap
brew install wwwyo/tap/skillctrl
```

3 つの経路は同じコマンドと 提供する版を入れます（`go install` ビルドと
release archive はバイナリ列バイトが一致するとは限りません）。
`skillctrl --version` が版を表示します。

## リポジトリ側の準備

`skillctrl` は Git リポジトリを前提に動作します。次の 2 つを用意します。

```
.agents/skills/<name>/                  導入・適応する skill
.agents/skillctrl/intents/<name>.md     維持したいカスタマイズの意図
```

lock ファイルはツールが作成します。上流の記録は
`.agents/.skill-lock.json`、受け入れ済みの hash は
`.agents/skillctrl/intents/lock.json` に保存されます。

## 手順

```sh
# 公開 index を検索する（導入はしない）
skillctrl find browser --owner vercel-labs

# 導入する
skillctrl add vercel-labs/agent-browser --skill agent-browser

# 導入した skill の意図を書く
$EDITOR .agents/skillctrl/intents/agent-browser.md

# 更新する。意図に適合させて skill 本体を書き直す
skillctrl update agent-browser

# 差分を確認する
skillctrl status

# 意図的な手動編集を確定する
skillctrl record agent-browser

# upstream 登録を削除する（意図ファイルは残る）
skillctrl remove agent-browser
```

標準出力は JSON、ログとエラーは stderr に出ます。exit 2 は再適応の一部が
未確定、exit 1 は失敗です。作業内容は作業ディレクトリに残るため、commit・push・
PR 作成は skillctrl 側では行いません。

## 安全の根拠

意図の再適応は、外部からの入力に対してモデルがコードを書き換える処理です。
どこまでを信頼するか、何を拒否するかを明記します。

- **原本は Git から直接取得します。** shallow clone の追跡済みファイルだけを
  読み、外部の installer・script・hook は一切実行しません。symlink、submodule、
  `.gitignore` / `.gitattributes`、配置先で除外されるファイルを含む skill は
  拒否し、実行ビットとバイナリ内容を保持します。名前が一意に決まらない選択は
  推測しません。
- **更新で変わっていない原本は取り込みません。** これが手動適応を毎回失わずに保つ仕組みです。
- **2 つの lock。** `.agents/.skill-lock.json`（version 3）は取得元、
  `.agents/skillctrl/intents/lock.json`（version 2）は受け入れ済みの実体の
  Git tree hash です。skill ディレクトリ全体（本文・reference・実行属性）が
  hash に含まれ、意図ファイルは含まれません。意図の変更・削除だけを理由に
  再適応が起動することはありません。判断がつかない skill はそのまま残し、古い
  hash を保持して未確定として報告します。削除された skill と意図の組み合わせは
  自動復旧せず、報告します。
- **作業ディレクトリ。** `add` / `update` / `remove` は dirty な checkout を
  拒否します。既に linked worktree ならその checkout をそのまま作業場所として
  使います。main checkout を指した場合は一時ディレクトリに detached worktree を
  新設し、元の checkout は書き換えません（`--worktree-provider orca` で Orca
  連携も選べます）。
- **再適応の隔離。** agent は no session / no context files / no skills / no
  extensions / no prompt templates / no auto-approve で起動します。これらは
  agent の探索と自動承認を止めるもので、作業対象である checkout 内のファイル
  自体を隠すものではありません。ローカル実行の `skillctrl update` は作業
  ディレクトリの作業ツリーを直接編集し、CI の `skillctrl ci apply` は検証済み
  patch を index にだけ適用します。
- **toolchain の解決先。** agent の toolchain は実行のために用意した設定
  ディレクトリで解決します。呼び出し元のディレクトリでも、レビュー対象の
  repository でもありません。解決値は継承値より優先され、agent の実行ファイル
  も解決済みの PATH から探します。
- **成果物は無信頼です。** agent の出力は、認証情報を持たない別の経路が
  再計算した plan と照合します。対象 skill 以外の変更、accepted / unresolved
  の不完全な分割、未確定 skill の変更、機密情報の混入（report・result・patch・
  変更 blob のすべて）を検査してからだけ hash を記録します。

詳細な設計は [README.md](../README.md)、CI での使い方と trust boundary は
[docs/ci.md](ci.md)、確認済みのテストとの対応表は [docs/parity.md](parity.md)
を参照してください。

[MIT ライセンス](../LICENSE)で公開しています。
