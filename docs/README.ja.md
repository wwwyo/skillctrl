![skillctrl](wordmark.svg)

# skillctrl

[![Go 1.27](https://img.shields.io/badge/go-1.27-blue.svg)](../go.mod) [![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](../LICENSE)

[English](../README.md) · [移植・公開の要件](requirements.md) · [CI での使い方](ci.md) · [対応表](parity.md)

agent skill を Git リポジトリで管理しながら、ローカルで加えた適応の「意図」を
失わないようにするための CLI です。

skill はどこか別の場所から導入したあと、その環境に合わせて手で編集します。
次の更新がその編集を上書きしても、何のために編集したのかの記録は残りません。
`skillctrl` はその理由を skill の隣に置きます。上流のリリースで編集済みの skill
が変わっても、原本で置き換えるのではなく自分の版に再適用し、実際に受け入れた
内容の hash を Git に記録します。

出力・コードコメント・公開文書・レビューは英語を標準とし、この README は
日本語で概要を案内します。

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

3 つの経路で同じバージョンのコマンドをインストールできます。
`go install` でビルドしたバイナリと release archive のバイナリは、同一の
バイト列になるとは限りません。バージョンは `skillctrl --version` で確認できます。

## リポジトリ側の準備

`skillctrl` は Git リポジトリを前提に動作します。次の 2 つを用意します。

```
.agents/skills/<name>/                  imported and adapted skills
.agents/skillctrl/intents/<name>.md     what your customization must keep doing
```

lock ファイルはツールが作成します。上流の記録は
`.agents/.skill-lock.json`、受け入れ済みの hash は
`.agents/skillctrl/intents/lock.json` に保存されます。

## 手順

```sh
# Search the public index without installing anything
skillctrl find browser --owner vercel-labs

# Import a skill
skillctrl add vercel-labs/agent-browser --skill agent-browser

# Write the intent for the imported skill
$EDITOR .agents/skillctrl/intents/agent-browser.md

# Update the skill body to satisfy the saved intent
skillctrl update agent-browser

# Check differences from the accepted hashes
skillctrl status

# Accept a deliberate manual edit
skillctrl record agent-browser

# Remove the upstream registration while keeping the intent file
skillctrl remove agent-browser
```

標準出力は JSON、ログとエラーは stderr に出ます。exit 2 は再適応の一部が
未確定、exit 1 は失敗です。上記のローカルコマンドは作業内容を作業ディレクトリに
残し、commit・push・PR 作成は行いません。`ci` / `schedule` の公開コマンドは
検証した変更を commit・push するためのもので、[CI の文書](ci.md)に手順を記載しています。

## agent 向け skill

配布用の [skillctrl skill](../skills/skillctrl/SKILL.md) に、検索・導入・調整・
更新・削除の手順をまとめています。新規作成・改善のガイドは必要なときに読む
別ファイルに置き、`find-skills` や `skill-creator` の別途導入は不要にしています。

このパッケージがリポジトリで公開されたら、対象を指定して導入できます。

```sh
skillctrl --repo /absolute/path/to/project add wwwyo/skillctrl --skill skillctrl
```

CLI の事前導入と、対象リポジトリの `.agents/skills/` が必要です。結果の JSON に
含まれる作業先を確認してから変更を統合します。配布元は `skills/`、導入先は
対象リポジトリの `.agents/skills/` です。skill と CLI は別の配布物です。

## 複数の原本を統合する

1つの skill の更新元を `sources` 配列で管理できます。まず
`.agents/skillctrl/intents/combined.md` に統合方針を保存し、リポジトリの
通常の手順で clean な checkout を用意します。

```sh
skillctrl merge combined \
  --from owner/discovery:find-skills \
  --from owner/authoring:skill-creator

skillctrl update combined
```

上記の取得元は説明用の仮名です。`merge` がリリースに含まれるまでは、
このソースからビルドした CLI を使います。`--from` の一覧は既存の更新元を
置き換えます。各原本の取得元・skill 名・commit・配置先・tree hash を個別に記録し、
既存の単一 source の記録は従来の形式で保持します。

原本は統合先 skill 内の `.skillctrl-sources/<index>/` に分けて保存します。
agent は原本を変更せず、保存した意図に従って統合後の本文を更新します。
原本が変わらなければ再統合しません。意図だけを変えたときは、本文を手で修正・
検証して受理します。統合が未解決なら受理済み hash は保持します。

結果の `repo` が実際の作業先です。統合結果は1つのローカル skill で、
そのディレクトリを配布できます。原本の manifest は skillctrl の検索対象から
除外します。旧 CLI は複数 source の記録に対応しません。統合には
[reviewer の設定](ci.md)が必要です。

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
