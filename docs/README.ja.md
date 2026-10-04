<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../assets/wordmark-dark.svg">
  <img src="../assets/wordmark.svg" alt="skillctrl" width="312" height="88">
</picture>

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
go install github.com/wwwyo/skillctrl@latest

# mise (GitHub release backend)
mise use -g github:wwwyo/skillctrl@latest

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
`skills-lock.json`、受け入れ済みの hash は
`.agents/skillctrl/intents/lock.json` に保存されます。
受け入れ済み hash・`status`・自動の意図チェックは、上流の lock に登録された
skill だけが対象です。自作 skill は、意図ファイルがあっても対象にしません。
意図的な編集を別途受け入れる二重管理を避けるためです。既存の自作 skill の
hash は、導入・更新・削除、`record`、CI の次の lock 書き込み時に取り除きます。
`status` は書き換えず、削除待ちの記録を `lock_changed` で報告します。

`npx skills` のプロジェクト用 lock（version 1）は root に置いたまま読み、既存の
version・他の provider の登録・未知のフィールドを保持します。skillctrl は Git の
取得情報を追加します。旧 `.agents/.skill-lock.json` だけがある場合は、取り込みの
成功後に root へ移行します。`status` や dry run では移動しません。root に既存の
lock があればそちらを優先します。統合済みの登録は skillctrl 独自の `sources`
を使うため、skillctrl で管理します。

## 手順

```sh
# Search the public index without installing anything
skillctrl find review --owner owner

# Import a skill
skillctrl add owner/repo --skill chosen-skill

# Write the intent for the imported skill
$EDITOR .agents/skillctrl/intents/chosen-skill.md

# Update the skill body to satisfy the saved intent
skillctrl update chosen-skill

# Check differences from the accepted hashes
skillctrl status

# Accept a deliberate manual edit
skillctrl record chosen-skill

# Remove the upstream registration while keeping the intent file
skillctrl remove chosen-skill
```

`owner/repo` と `chosen-skill` は説明用の仮名です。確認した取得元と skill 名に
置き換えてください。意図には、例えば「指示を簡潔にし、コード変更の完了前に
テストを要求する」といった振る舞いを記載します。

取得処理は adapter に委ねます。既定の `skills` は `npx skills` と同じ
パッケージの `skills` CLI を呼び、`--adapter gh` は `gh skill install` を
呼びます。`--adapter git` で従来の直接 Git 取得も選べます。
`SKILLCTRL_ADAPTER` で既定値を設定でき、明示した flag が優先されます。
依存コマンドがなければエラーにし、別の adapter へ勝手に切り替えません。
ツールは mise で管理し、repo では `skills` 1.7.0 と `gh` 2.101.0 を固定しています。

```sh
skillctrl --adapter skills add owner/repo --skill chosen-skill
skillctrl --adapter gh add owner/repo --skill chosen-skill
skillctrl list
skillctrl check chosen-skill
```

| command | 役割 |
| --- | --- |
| `find` / `search` | `skills` / `git` は skills.sh API、`gh` は `gh skill search` で検索。skills 1.7.0 の find には JSON 出力がないため API を使います。 |
| `add` / `install`, `update` | adapter で原本を取得し、skillctrl が検証・取り込み・意図の再適用を担当。 |
| `list` / `ls` | 手書きを含む project の skill 一覧。 |
| `check` | 原本の更新確認。取り込みと reviewer 実行はしません。 |
| `remove` / `rm` | ローカルの実体と登録を削除。ネットワークや adapter コマンドは不要。 |
| `status`, `record`, `merge` | skillctrl 独自の受理 hash と保存した意図の管理。status は手元の受理状態、check は更新元の変更を確認します。 |
| `plan`, `prompt`, `schema`, `ci`, `schedule` | 検査と任意の自動化。 |

adapter は一時ディレクトリと一時 home に取得し、project の root にある
`skills-lock.json` の管理場所は変更しません。GitHub CLI が埋め込む追跡 metadata は
SKILL.md に残します。非公開の取得元にも接続できるよう、GitHub の認証は
adapter に渡し、モデル用の credential は除去します。取得ツール自体は信頼する
実行コマンドとして扱います。探索方法・release/ref の選択・取得ファイルの実行属性は
backend の仕様に従います。切替で原本が変われば再適応します。command adapter が
取得 commit を提供しない場合、存在しない commit を記録しません。
CI は任意です。意図の再適用には CI とは別に reviewer とモデルの設定が必要です。

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
`.agents/skillctrl/intents/combined.md` に統合方針を保存します。

```sh
skillctrl merge combined \
  --from owner/discovery:find-skills \
  --from owner/authoring:skill-creator

skillctrl update combined
```

上記の取得元は説明用の仮名です。
`--from` の一覧は既存の更新元を
置き換えます。各原本の取得元・skill 名・配置先・tree hash（取得できる場合は commit も） を個別に記録し、
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

- **原本は隔離して準備します。** adapter は作業 repo の外で取得し、取り込み前に
  symlink・Git 制御ファイル・配置先の除外規則を検証します。`git` adapter は
  追跡済み blob と実行属性を保持します。command adapter は各ツール本来の
  探索・導入仕様に従い、skillctrl は取得した skill の script を実行しません。
- **更新で変わっていない原本は取り込みません。** これが手動適応を毎回失わずに保つ仕組みです。
- **2 つの lock。** `skills-lock.json`（新規作成は version 1、旧 version 3 も読み取り可能）は取得元、
  `.agents/skillctrl/intents/lock.json`（version 2）は受け入れ済みの実体の
  Git tree hash です。skill ディレクトリ全体（本文・reference・実行属性）が
  hash に含まれ、意図ファイルは含まれません。意図の変更・削除だけを理由に
  再適応が起動することはありません。判断がつかない skill はそのまま残し、古い
  hash を保持して未確定として報告します。削除された skill と意図の組み合わせは
  自動復旧せず、報告します。
- **未コミットの編集があっても使えます。** `add` / `merge` / `update` / `remove`
  は、無関係な作業内容と呼び出し元の staging を保持します。未コミットの編集を
  含む skill ディレクトリを置き換える場合は、取り込み前に拒否します。原本が
  変わっていなければ、その編集を上書きしません。linked worktree ではその場で
  作業します。main checkout では現在の commit から別の worktree を作り、追跡済み
  ファイルの差分と Git の除外対象でない未追跡ファイルを引き継ぎます。元の
  checkout は変更しません。結果の `repo` が実際の作業先です。
  `--worktree-provider orca` で Orca 連携も選べます。
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
