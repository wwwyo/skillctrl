<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../../assets/wordmark-dark.svg">
  <img src="../../assets/wordmark.svg" alt="skillctrl" width="312" height="88">
</picture>

# skillctrl

[![Go 1.27](https://img.shields.io/badge/go-1.27-blue.svg)](../../go.mod) [![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](../../LICENSE)

[English](../../README.md) · [skill の日本語訳](skills/skillctrl.md) · [移植・公開の要件](../requirements.md) · [CI での使い方](../ci.md) · [対応表](../parity.md)

agent skill を Git リポジトリで管理しながら、ローカルで加えた適応の「意図」を
失わないようにするための CLI です。

`add`・`merge`・`update` は原本の取得と登録だけを行います。調整の要件は
`.agents/skillctrl/intents/<name>.md` と skill を直接編集し、確認済みの
内容を `record` で受け入れます。通常の CLI は指定した repo をその場で変更し、
AI の起動や worktree の作成を行いません。取得時には受理 hash も更新しません。

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
受け入れ済み hash・`check`・意図のレビューは、upstream に登録され、かつ intent が
ある skill だけが対象です。intent のない導入済み skill と自作 skill は対象外です。
対象外の既存 hash は次の受理 lock 書き込み時に取り除きます。取得コマンドは受理 lock を
書きません。`check` は削除待ちの記録を `lock_changed` で報告します。

`npx skills` のプロジェクト用 lock（version 1）は root に置いたまま読み、既存の
version・他の provider の登録・未知のフィールドを保持します。skillctrl は Git の
取得情報を追加します。旧 `.agents/.skill-lock.json` だけがある場合は、取り込みの
成功後に root へ移行します。`check` や dry run では移動しません。root に既存の
lock があればそちらを優先します。統合済みの登録は skillctrl 独自の `sources`
を使うため、skillctrl で管理します。

## 手順

```sh
# Search the public index without installing anything
skillctrl find review --owner owner

# Import a skill
skillctrl add owner/repo:chosen-skill

# Refresh the original without AI or acceptance
skillctrl update chosen-skill

# Save requirements, then edit and verify the skill directly
mkdir -p .agents/skillctrl/intents
cp requirements.md .agents/skillctrl/intents/chosen-skill.md

# Check differences from the accepted hashes
skillctrl check

# Compute and save the hash after verifying a deliberate edit
skillctrl record chosen-skill

# Remove the skill, its saved intent, and registrations
skillctrl remove chosen-skill
```

`owner/repo` と `chosen-skill` は説明用の仮名です。確認した取得元と skill 名に
置き換えてください。意図には、例えば「指示を簡潔にし、コード変更の完了前に
テストを要求する」といった振る舞いを記載します。

取得処理は adapter に委ねます。既定の `skills` は `npx skills` と同じ
パッケージの `skills` CLI を呼び、`--adapter gh` は `gh skill install` を
呼びます。`--adapter git` で従来の直接 Git 取得も選べます。
`SKILLCTRL_ADAPTER` で既定値を設定でき、明示した flag が優先されます。
`--adapter` の値は `skills`・`gh`・`git` に限定します。無効な flag 値や、実際に
使われる `SKILLCTRL_ADAPTER` の値は、ローカル操作や dry run でも実行前に拒否します。
依存コマンドがなければエラーにし、別の adapter へ勝手に切り替えません。
ツールは mise で管理し、repo では `skills` 1.7.0 と `gh` 2.101.0 を固定しています。

```sh
skillctrl --adapter skills add owner/repo:chosen-skill
skillctrl --adapter gh add owner/repo:chosen-skill
skillctrl list
skillctrl check chosen-skill
```

| command | 役割 |
| --- | --- |
| `find` / `search` | `skills` / `git` は skills.sh API、`gh` は `gh skill search` で検索。skills 1.7.0 の find には JSON 出力がないため API を使います。 |
| `add` / `install`, `update` | adapter で原本を取得し、skillctrl が検証・取り込み・登録を担当。AI と受理 hash 更新は行いません。 |
| `list` / `ls` | 手書きを含む project の skill 一覧。 |
| `check` | 手元の受け入れ済み hash の差分だけをオフラインで確認。名前で対象を限定できます。upstream 参照・取り込み・reviewer 実行はしません。 |
| `remove` / `rm` | ローカルの実体・対応する意図ファイル・登録を削除。ネットワークや adapter コマンドは不要。 |
| `merge` | skillctrl 独自の routing skill 作成。intent や AI は不要です。 |
| `record` | 現在の skill ディレクトリから hash を計算し、指定 skill の lock の値を作成・置換します。内容は実行前に確認します。 |
| `ci`, `schedule` | 任意の自動化。`ci plan` は固定 commit の対象選択、`ci prompt` は reviewer 用の指示を担当します。 |

adapter は一時ディレクトリと一時 home に取得し、project の root にある
`skills-lock.json` の管理場所は変更しません。GitHub CLI が埋め込む追跡 metadata は
SKILL.md に残します。GitHub CLI は一時 home に切り替える前に既存の
認証を解決し、取得プロセスだけに渡します。モデル用の credential は除去します。取得ツール自体は信頼する
実行コマンドとして扱います。探索方法・release/ref の選択・取得ファイルの実行属性は
backend の仕様に従います。切替で原本が変わった場合の調整は直接編集して行います。command adapter が
取得 commit を提供しない場合、存在しない commit を記録しません。
CI は任意です。通常の CLI には reviewer やモデルの設定は不要です。
エディタや今使っている agent で直接編集し、確認後に record します。

`record NAME` は現在の skill ディレクトリ全体から hash を計算し、
`.agents/skillctrl/intents/lock.json` の NAME の値を作成・置換します。
NAME は対象 skill 名で、hash を渡す引数ではありません。同じ内容なら同じ hash になります。
root の `skills-lock.json` にある原本の hash は更新しません。
本文・upstream 登録・Git の staging は変更せず、reviewer も実行しません。
意図を満たすかの検証は実行前に自分で行います。
`record --dry-run` は操作の概要だけを表示し、hash の計算・比較はしません。差分の確認には `check [NAME...]` を使います。
upstream 登録と intent のある指定 skill の hash だけを更新し、他の対象の hash は保持します。対象外の記録は取り除きます。
引数なしの `record` は対象外の記録の整理だけを行い、内容を受理しません。intent を全部削除した後にも使えます。

`ci plan` は固定 commit から CI のレビュー対象を選び、`ci prompt` は準備後に外部の
agent へ渡すレビュー指示を表示します。ローカルの編集や `record` には不要です。
`check` は upstream を参照せず、手元の差分だけを `local` に報告します。
skill ディレクトリ全体と受け入れ済み hash の比較であり、意図を満たすかの AI 検証ではありません。
差分があるだけでは異常終了せず、JSON で報告します。名前を省略すると upstream 登録と
intent の両方がある全 skill を比較し、名前を指定するとその範囲に絞ります。
upstream の取得は明示的な `update` または `schedule prepare` で行います。upstream の更新だけでは手元の受理状態や CI の対象は変わりません。
`ci` は PR の検証・修復・公開、`schedule` は upstream 更新の準備と検証後の
更新 PR 作成を担います。workflow の導入やタイマーの起動はせず、外部の CI や scheduler
から各段階を呼び出す必要があります。

原本を更新するときは、現在のローカル調整を確認してから次を実行します。

```sh
skillctrl update chosen-skill
git diff -- .agents/skills/chosen-skill skills-lock.json
# Edit and verify the skill against its saved intent
skillctrl record chosen-skill
git diff -- .agents/skills/chosen-skill .agents/skillctrl/intents/lock.json
```

単体 skill は原本が変わると本文を置き換え、merged skill は登録した references を
更新してルート本文を保持します。Git diff は取り込み前後の差分であり、旧原本と新原本の
比較を別途自動生成するものではありません。保存した intent に沿って skill を直接編集し、
確認後に `record chosen-skill` で明示的に受理します。intent のない skill は受理 lock の対象外です。
commit 前に最終的な変更を確認します。

標準出力は JSON、ログとエラーは stderr に出ます。exit 0 は成功、exit 1 は失敗です。上記のローカルコマンドは作業内容を作業ディレクトリに
残し、commit・push・PR 作成は行いません。`ci` / `schedule` の公開コマンドは
検証した変更を commit・push するためのもので、[CI の文書](../ci.md)に手順を記載しています。

## agent 向け skill

配布用の [skillctrl skill](../../skills/skillctrl/SKILL.md) に、検索・導入・調整・
更新・削除の手順をまとめています。新規作成・改善のガイドは必要なときに読む
別ファイルに置き、`find-skills` や `skill-creator` の別途導入は不要にしています。

このパッケージがリポジトリで公開されたら、対象を指定して導入できます。

```sh
cd /absolute/path/to/project
skillctrl add wwwyo/skillctrl:skillctrl
```

CLI の事前導入と、対象リポジトリの `.agents/skills/` が必要です。結果の JSON に
含まれる作業先を確認してから変更を統合します。配布元は `skills/`、導入先は
対象リポジトリの `.agents/skills/` です。skill と CLI は別の配布物です。

## 複数の原本を統合する

`add` と `merge` は同じ `owner/repo:skill` を位置引数に並べて取得元を指定します。
`add` はそれぞれ別の skill を導入します。
`merge` は原本を1つの skill 配下に保存し、root の `SKILL.md` を routing として作ります。
intent や reviewer は不要です。

```sh
# Import separate skills
skillctrl add owner/first:first-skill owner/second:second-skill

# Combine the same inputs under one routing skill
skillctrl merge --name combined \
  owner/first:first-skill \
  owner/second:second-skill
skillctrl update combined
```

取得元は説明用の仮名です。原本は `references/first-skill/`、`references/second-skill/`
に保存し、root の `SKILL.md` から参照します。reference・script と相対参照も保持します。
原本の manifest は独立した skill として検索しません。異なる取得元でも元の skill 名が
同じ場合は取得前にエラーにします（大文字小文字も区別しません）。手書きの reference は
保持します。旧形式は update 時に配置を保持し、merge の再実行で新しい配置に切り替えます。`merge` を再実行すると更新元の
一覧を置き換えて routing を再生成します。`update` は現在の root を保持して原本を更新します。

統合方針を調整する場合は、intent ファイルと root の routing を直接編集します。

```sh
mkdir -p .agents/skillctrl/intents
cp integration-requirements.md .agents/skillctrl/intents/combined.md
# Edit and verify .agents/skills/combined/SKILL.md
skillctrl record combined
```

統合先の intent は `combined.md` です。既存の入力 skill とそのローカル intent
は合成・削除しません。調整時には原本を保持し、未解決の内容は record しません。
指定した repo が作業先です。旧 CLI は複数 source の記録に対応しません。

単一の導入には `add owner/repo:upstream-name --name local-name` を使えます。ローカルの
ディレクトリ名・登録名を変え、原本の本文と frontmatter は保持します。
`update local-name` は登録した upstream 名を使い、`check local-name` は手元の受理 hash だけを確認します。
`--name` は選択した skill が1つの場合だけ使えます。複数の別 skill を導入するときは省略します。
取得 CLI と同じ `add owner/repo --skill upstream-name` も使えます。`--skill` は1つの repo を位置引数に指定する書式で使い、owner/repo:skill の入力とは併用できません。
複数の入力は異なる skill 名を指定します。

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
  変わっていなければ、その編集を上書きしません。main checkout も linked worktree も、
  指定した repo をその場で変更します。worktree の作成や作業ディレクトリの変更は行いません。
  別の repo を対象にする場合は、`cd` で移動してから実行します。
- **AI の実行は外部 workflow の責務です。** 通常の CLI は agent を起動しません。
  CI の準備コマンドは信頼済み設定とレビュー指示を提供し、採用側の workflow が agent を
  起動して成果物を渡します。`skillctrl ci apply` は検証済み patch を index にだけ適用します。
- **成果物は無信頼です。** agent の出力は、認証情報を持たない別の経路が
  再計算した plan と照合します。対象 skill 以外の変更、accepted / unresolved
  の不完全な分割、未確定 skill の変更、機密情報の混入（report・result・patch・
  変更 blob のすべて）を検査してからだけ hash を記録します。

## 開発中の CLI を実行する

この repo 内で mise が有効なら、`skillctrl` は `tools/dev/skillctrl` を実行します。
毎回現在の checkout をビルドし、元の作業ディレクトリ・引数・終了コードを保って
実行するため、ソースの変更をそのまま検証できます。ビルドには Go の cache を使います。
shell で mise を有効にしていない場合は、次のように実行します。

```sh
mise exec -- skillctrl --help
```

詳細な設計は [README.md](../../README.md)、CI での使い方と trust boundary は
[docs/ci.md](../ci.md)、確認済みのテストとの対応表は [docs/parity.md](../parity.md)
を参照してください。

[MIT ライセンス](../../LICENSE)で公開しています。
