# ローカルの skill 管理

コマンドの詳しい挙動、adapter の違い、lock の互換性を説明します。概要と導入は [README](README.md) を参照してください。

## リポジトリ側の準備

`skillctrl` は Git リポジトリを前提に動作します。次の 2 つを用意します。

```
.agents/skills/<name>/                  imported and adapted skills
.agents/skillctrl/intents/<name>.md     what your customization must keep doing
```

native の project lock は root の `skills-lock.json` のままです。通常の登録は
`npx skills` の形式を保ち、別名・統合入力・追加の取得情報は
`.agents/skillctrl/upstreams.json` に保存します。独自の登録やフィールドを native の
lock に追加しません。native のコマンドは通常の登録を復元・更新でき、native にない
別名と統合の管理には skillctrl を使います。

`.agents/skillctrl/intents/lock.json` は、upstream 登録と intent の両方を持つ
skill ディレクトリ全体の確認済み hash を保存します。intent のない導入済み skill と
自作 skill は対象外です。取得は受理 hash を更新せず、`check` はローカルの乖離と
対象外の記録の削除待ちを offline で報告します。

既存の native lock は、version・他の provider・未知のフィールドを保持し、登録が
変わらなければ元のバイト列も保持します。追加の情報は対応する native 登録に紐付け、
native 側で独立に編集・削除された場合は古い追加情報を使いません。旧 version 3 と
skillctrl の独自登録も読み、取り込みが成功したときだけ、認識した別名・統合の登録を
専用ファイルへ移します。read-only のコマンドと dry run は移行しません。root の lock が
ない場合に限り、成功した取り込みで旧 `.agents/.skill-lock.json` を root に移します。


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
adapter は実行ごとに選びます。探索方法・取得元の解決・実行属性は backend ごとに
異なるため、その違いが重要な場合は明示的に選びます。配布元のこの repo は自身の
統合登録を専用ファイルに保存するため、配布パッケージを native の導入済みの依存として
記録しません。日本語の skill ガイドの metadata は文書として掲載し、導入用の
frontmatter を付けないため、翻訳が配布物として選ばれることを防ぎます。

CI は任意です。通常の CLI には reviewer やモデルの設定は不要です。
エディタや今使っている agent で直接編集し、確認後に record します。

`record NAME` は現在の skill ディレクトリ全体から hash を計算し、
`.agents/skillctrl/intents/lock.json` の NAME の値を作成・置換します。
NAME は対象 skill 名で、hash を渡す引数ではありません。同じ内容なら同じ hash になります。
native の登録と原本の追跡情報は更新しません。
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
git diff -- .agents/skills/chosen-skill skills-lock.json .agents/skillctrl/upstreams.json
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
原本の manifest は skillctrl の独立した skill の検索対象にはしません。
有効化時には対象 agent の探索の挙動も確認してください。配下の manifest まで探索する
agent は、保存した原本も別の skill として公開する場合があります。異なる取得元でも元の skill 名が
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
指定した repo が作業先です。旧 skillctrl CLI は専用の追跡ファイルを読みません。

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
- **受理 hash。** native の登録は root の `skills-lock.json`、別名・統合・追加の取得情報は
  `.agents/skillctrl/upstreams.json` に保存します。
  `.agents/skillctrl/intents/lock.json`（version 2）は受け入れ済みの実体の
  Git tree hash です。skill ディレクトリ全体（本文・reference・実行属性）が
  hash に含まれ、意図ファイルは含まれません。意図の変更・削除だけを理由に
  再適応が起動することはありません。判断がつかない skill はそのまま残し、古い
  hash を保持して未確定として報告します。skill が削除されても intent が残っている場合は、
  自動復旧せず報告します。
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
