<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../../assets/wordmark-dark.svg">
  <img src="../../assets/wordmark.svg" alt="skillctrl" width="312" height="88">
</picture>

# skillctrl

[![Go 1.27](https://img.shields.io/badge/go-1.27-blue.svg)](../../go.mod) [![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](../../LICENSE)

[English](../../README.md) · [コマンドの詳細](usage.md) · [CI での使い方](ci.md)

ローカルでカスタマイズした意図を保ちながら、Git リポジトリで agent skill を管理します。
skillctrl は upstream の原本と確認済みの内容を記録し、編集は普段のエディタや agent で行います。

## skill を使う

### skill のそばに intent を保存する

導入した skill の指示をプロジェクトに合わせて変えたい場合は、維持したい振る舞いを
`.agents/skillctrl/intents/<name>.md` に書き、skill を直接編集します。
例えば「指示は簡潔にし、コード変更の完了前にテストを要求する」と書きます。
保存した patch が適切でなくなっても、intent をもとに自分や agent が変更を判断できます。

[skillctrl skill の日本語訳](skills/skillctrl/SKILL.md) は、検索・導入・作成・改善・更新・削除の
手順を agent に伝えます。作成ガイドは必要なときだけ読み込み、検索用・作成用の skill を
別々に導入する必要はありません。CLI 自体が AI を起動することはありません。

### 確認済みの内容を明示的に記録する

`add` は原本を取り込み、取得元を登録します。`record NAME` は確認後の skill ディレクトリ全体から
hash を計算して保存します。references・scripts・実行属性を含み、skill 本文は変更しません。
記録するのは upstream 登録と保存した intent の両方がある skill だけです。
手書きの skill やカスタマイズしていない導入 skill に、追加の受理操作は不要です。

```sh
skillctrl add owner/repo:chosen-skill
mkdir -p .agents/skillctrl/intents
# Write the intent and edit the skill with your editor or existing agent
# Verify the behavior and inspect the complete skill directory
skillctrl record chosen-skill
skillctrl check chosen-skill
```

`check` はファイルを変更せず、upstream にも接続せず、手元の差分を報告します。
intent に沿うかの判断は行わず、hash の不一致だけではコマンドは失敗しません。
編集しただけで記録済み hash が更新されることもありません。intent 自体は hash の対象外です。

### 原本を更新してからカスタマイズを確認する

`update NAME` は登録された原本を取得します。原本が変わっていなければ、ローカルの調整を保ちます。
変わった場合は取り込んだ差分を確認し、保存した intent に沿って skill を調整・検証してから
明示的に hash を記録します。

```sh
skillctrl update chosen-skill
git diff -- .agents/skills/chosen-skill skills-lock.json .agents/skillctrl/upstreams.json
# Edit and verify the updated skill against its intent
skillctrl record chosen-skill
```

コマンドは現在のリポジトリをその場で変更します。無関係な編集と staging は保持し、
未コミットの編集がある skill を置き換える取り込みは拒否します。別のリポジトリは `cd` で選びます。

### 取得元を追跡したまま複数の skill を統合する

`merge` は完全な原本を1つの skill の配下に保存し、`references/<upstream-skill-name>/` へ
routing する root の `SKILL.md` を作ります。原本ごとの資源と取得元の登録を保持します。
routing とその intent は直接調整でき、後の `update` は root を保持して原本を更新します。
`merge` を明示的に再実行すると、取得元の一覧を置き換えて routing を再生成します。

```sh
skillctrl merge owner/first:first-skill owner/second:second-skill --name combined
```

`add` と `merge` は共通の `owner/repo:skill` を使います。1つの skill を導入するときは
`add --name NAME` でローカル名を指定でき、元の upstream 名も追跡します。
例の入力は仮名なので、内容を確認した取得元に置き換えてください。

取得は `skills` が既定です。PATH に互換性のある CLI があれば再利用し、なければ
npx を使います。skills の個別導入は任意で、
npx を使う場合は Node.js と npm が必要です。
`--adapter gh` は GitHub CLI、`--adapter git` は直接 Git を使います。
通常の取得元は native 形式の root `skills-lock.json` に登録します。
別名・統合・追加の取得情報は `.agents/skillctrl/upstreams.json`、確認済み hash は別の
`.agents/skillctrl/intents/lock.json` に保存します。検索・一覧・削除には `find`・`list`・`remove` を使います。
adapter・lock・細かな挙動は[コマンドの詳細](usage.md)を参照してください。

## AI に導入を任せる

対象リポジトリで、使っている AI に次の prompt を渡します。

```text
https://raw.githubusercontent.com/wwwyo/skillctrl/main/docs/start.md を読み、このリポジトリに skillctrl CLI と skill を導入してください。
```

[docs/start.md](../start.md) は CLI・取得用ツール・skill 本体の導入と、agent が読み込めるかの
確認までを扱います。[日本語訳](start.md)もあります。

## 手動で導入する

対象の Git リポジトリで CLI を導入します。

```sh
mise use --path ./mise.toml github:wwwyo/skillctrl@latest
mise exec -- skillctrl --help
```

取り込み前に、help に `list`・`check`・`record`・`--adapter skills|gh|git` があり、
`--repo`・`--worktree-provider`・`intent` コマンドがないことを確認します。
npx を使う場合は、help に `pinned npx` があることも確認します。
Go・Homebrew の導入方法と互換性の確認手順は[導入ガイド](start.md)に記載しています。

既定の adapter は、既存の互換 skills CLI か、固定版を呼ぶ npx を使います。
Node.js が必要な場合だけ導入し、既存の互換 pin は保持します。

```sh
mise use --path ./mise.toml node@lts
mkdir -p .agents/skills
mise exec -- skillctrl add wwwyo/skillctrl:skillctrl
```

CLI と skill は別の配布物です。stdout は JSON、stderr は診断です。
終了コードは成功が `0`、失敗が `1` です。

## 必要に応じて自動化する

CI は `check` の JSON を判定し、scheduler は `update` を呼びます。
確認・編集・`record NAME` はローカルと同じ手順です。
agent の実行と draft PR の作成は外部 workflow が担当します。[CI の文書](ci.md)を参照してください。

## 開発する

```sh
mise install
mise exec -- go test ./...
mise exec -- go vet ./...
mise exec -- go build .
```

この repo の `mise exec -- skillctrl` は、global の導入を置き換えず、現在の checkout を
ビルドして実行します。開発の詳細は[要件](../requirements.md)・[テストとの対応表](../parity.md)・
[リリース手順](../releasing.md)にまとめています。[MIT](../../LICENSE)で公開しています。

配布する skill 自体も、この repo の `.agents/skills/skillctrl/` で管理します。
`.agents/skillctrl/upstreams.json` は検索・作成の取得元を追跡し、
`.agents/skillctrl/intents/skillctrl.md` は統合方針を保存します。
同じ `update skillctrl`・直接編集・`record skillctrl`・`check skillctrl` の流れを使います。
