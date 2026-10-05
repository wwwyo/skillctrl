# 現在のリポジトリに skillctrl を導入する

これは [AI 向け導入手順](../start.md)の日本語訳です。
ユーザーから skillctrl CLI と skill の導入を依頼されたときに、この手順に従います。
README は手動で導入する人向けです。この手順だけで導入を進められます。

## 対象と既存の設定を確認する

現在のリポジトリの agent 向け指示と、ツール管理の設定を読みます。
ユーザーが選んだリポジトリで作業し、管理コマンドの実行前に `cd` で移動します。
skillctrl は現在のディレクトリを含む Git リポジトリの root を操作先にします。
`--repo` や skill をグローバルに導入するオプションはありません。

Git が使えることを確認し、`git rev-parse --show-toplevel` で checkout を特定します。
導入コマンドの例は、その root で実行します。

```sh
cd "$(git rev-parse --show-toplevel)"
```
無関係な編集、staging、ツールの固定バージョン、既存の skill 管理ツールの登録を保持します。
別の worktree を作ったり、個人の dotfiles に操作先を移したりしません。
ユーザーがリポジトリを選んでおらず、書き込み先の候補が複数ある場合は、導入前に対象を確認します。

## CLI と取得用ツールを導入する

必要に応じてリポジトリのツール管理方式を使い、`skillctrl --version` と
`skillctrl --help` を確認します。取り込み前に、その場で動作し reviewer を起動しない
現在のコマンド構成かを確認します。root の help に `list`、`check`、`record`、
`--adapter skills|gh|git` があり、`--repo`、`--worktree-provider`、ローカルの `intent`
コマンドがないことが条件です。`add` の構文が通っても、旧方式の worktree 作成や
reviewer 起動が残る CLI は、この手順には対応していません。

互換性を確認してから導入済み CLI を再利用し、互換性のあるツールの固定バージョンを保持します。
CLI がないか非対応の場合は、リポジトリが使っている導入方式を優先し、
対応する公開済みリリースを選びます。対応する配布経路は次のとおりです。

```sh
# mise: install locally and save an exact version, respecting a seven-day cooldown
mise use --path ./mise.toml --pin --minimum-release-age 7d github:wwwyo/skillctrl@latest
mise exec -- skillctrl --version

# Go: use when Go is the selected installation method
go install github.com/wwwyo/skillctrl@latest
"$(go env GOPATH)/bin/skillctrl" --version

# Homebrew: use when Homebrew is the selected installation method
brew tap wwwyo/tap
brew install wwwyo/tap/skillctrl
skillctrl --version
```

方式は 1 つ選びます。mise の例は、このリポジトリ内の設定ファイルを明示しています。
リポジトリの指示が別のファイルを指定していれば、`./mise.toml` の代わりにその repo 内の
既存ファイルを使います。親ディレクトリから継承した設定には書き込みません。

`@latest` は公開済みのバージョンを解決します。
バージョン固定と公開後の待機期間は、リポジトリの規約に従います。
リポジトリ内のセットアップのためだけに、既存のグローバル導入を置き換えたり版を変更したりしません。
以降は、特定した実行ファイルかツール管理コマンド経由で CLI を実行します。
導入後も、必要なコマンド構成かを再確認します。互換性と待機期間の両方を満たす
公開済みリリースがなければ、まだ導入を完了できないと報告します。
旧 `add` を実行したり、待機期間を回避したりしません。
この agent から実行ファイルにアクセスできることを確認します。導入に成功しても、
実行できなければセットアップは完了していません。

`skillctrl add --help` を確認します。既定の取得 adapter は `skills` であり、
PATH にある安定版の `skills` 1.x（1.7.0 以上）を再利用します。
見つからない、互換性がない、または版を確認できない場合は、
`npx --yes --ignore-scripts skills@1.7.0` を呼びます。skills の個別導入は任意です。
npx を使う場合は Node.js と npm が必要で、skills は Node.js 22.20.0 以上を要求します。
この挙動を使う前に、help に `pinned npx` があることを確認します。
旧版の skillctrl では skills の個別導入が必要です。
不足する Node.js はリポジトリのツール管理方式で導入します。
mise の場合、必要なツールがなく、互換性のある固定版もないときに使う例は次のとおりです。

```sh
mise use --path ./mise.toml --pin --minimum-release-age 7d node@lts
mise exec -- node --version
mise exec -- npx --version
```

ユーザーやリポジトリが明示的に `gh` または `git` を選んでいれば、その設定を使います。
`gh` は GitHub CLI、`git` は Git を直接使います。
ツールがない場合に adapter を黙って切り替えたり、バージョンを固定していない npx の
download を使ったりしません。固定版の npx 呼び出しは初回に取得する場合があり、
npm cache を使います。project の package.json や global CLI は変更しません。
cache の明示設定を尊重し、設定がなければ一時 staging 外の呼び出し元の ~/.npm を使います。
取得に失敗したらエラーを返し、別の backend で再試行しません。必要な機能が導入済み CLI にない場合は、
未対応の flag を推測せず、選んだツール管理方式で互換性のある公開済みバージョンを導入します。

## リポジトリ内に skill を導入する

選んだリポジトリで実行します。skill ディレクトリがなければ作成し、
前の手順で特定した CLI で、このパッケージだけを取り込みます。

```sh
mkdir -p .agents/skills
skillctrl add wwwyo/skillctrl:skillctrl
```

CLI と adapter を mise で管理している場合の取り込みコマンドは次のとおりです。

```sh
mise exec -- skillctrl add wwwyo/skillctrl:skillctrl
```

配布元は `wwwyo/skillctrl` の `.agents/skills/skillctrl/` です。
導入先は対象リポジトリの `.agents/skills/skillctrl/` とし、
`SKILL.md`、references、その他の同梱リソースを含めます。
既存のパッケージは置き換え前に確認し、未コミットの編集を理由に取り込みが拒否されたら、
ローカルのカスタマイズを保全します。セットアップを通すために編集を reset しません。

管理するパスは次のとおりです。

```text
.agents/skills/<name>/                  skill body and bundled resources
.agents/skillctrl/intents/<name>.md     optional local customization requirements
skills-lock.json                      native upstream registrations at repository root
.agents/skillctrl/upstreams.json        aliases, merged inputs, supplemental provenance
.agents/skillctrl/intents/lock.json     recorded hashes for upstream + intent skills
```

プロジェクト用 lock は root の `skills-lock.json` に保持します。
既存の version 1 の lock では、他の provider の登録と未知のフィールドを保持します。
旧 `.agents/.skill-lock.json` は root に lock がない場合だけ使い、取り込み成功後に移行します。
読み取り専用コマンドでは移動しません。

カスタマイズしていない導入に intent を作ったり、`record` を実行したりしません。
intent は任意であり、hash の lock は upstream 登録と保存した intent の両方がある
skill だけが対象です。導入に CI、scheduler、モデルの credential、ローカルで起動する reviewer は不要です。

## 確認して引き渡す

導入先の `.agents/skills/skillctrl/SKILL.md` を読み、相対パスで参照するリソースを確認します。
特定した CLI の実行方法で `skillctrl list` を実行し、`skillctrl` が一覧にあることを確認します。
新しいファイル、リポジトリの差分、root の `skills-lock.json` と、あれば
`.agents/skillctrl/upstreams.json` を確認します。
結果に含まれる `repo` は、選んだ checkout を示している必要があります。

現在の agent が skill を探索する設定を確認し、既存のリポジトリ用 skill ディレクトリの規約を使います。
相対 symlink が必要なら、既存の配置に従い、他の登録を保持します。
ファイルを導入できたことと、現在の agent で skill が利用可能だと確認できたことを区別します。
設定上必要な場合だけ reload や新しい session を案内し、ファイルをコピーしただけで
skill が有効になったと報告しません。

実際の checkout、CLI のバージョンと実行方法、adapter、導入先、検証結果、
残っている有効化の手順を報告します。
ユーザーやリポジトリの作業手順ですでに認められていなければ、commit・push・PR 作成・自動化の設定はしません。
以降の skill 管理には導入した skill を使います。通常の CLI コマンドはその場で動作し、AI agent を起動しません。
