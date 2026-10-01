# skillctrl (日本語)

agent skill を Git リポジトリで管理しつつ、ローカルで加えた適応の「意図」を
失わないようにするための CLI です。

詳細は英語 README を参照してください:
[README.md](../README.md)

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

## 使い方

```sh
skillctrl find browser --owner vercel-labs
skillctrl add vercel-labs/agent-browser --skill agent-browser
skillctrl update
skillctrl status
skillctrl record agent-browser
skillctrl schema
```

標準出力は JSON、ログとエラーは stderr に出ます。exit 2 は再適応の一部が未確定、
exit 1 は失敗です。作業内容は worktree に残るため、commit・push・PR 作成は
skillctrl 側では行いません。

## 仕組み

- **原本は Git から直接取得します。** shallow clone の追跡済みファイルだけを
  読み、外部の installer・script・hook は一切実行しません。symlink、submodule、
  `.gitignore` / `.gitattributes`、配置先で除外されるファイルを含む skill は
  拒否します。
- **意図ファイルが仕様です。** `.agents/skillctrl/intents/<name>.md` に「何を
  保ち続けるべきか」を書きます。更新で意図が壊れるときは、本文・description・
  examples を現在の意図に合わせて書き換えます。意図の変更・削除だけを理由に
  再適応が起動することはありません。
- **2 つの lock。** `.agents/.skill-lock.json`（version 3）は取得元の記録、
  `.agents/skillctrl/intents/lock.json`（version 2）は「確認済みの実体」の
  Git tree hash の記録です。skill ディレクトリ全体（本文・reference・実行属性）
  が hash に含まれ、意図ファイルは含まれません。
- **隔離。** installer は dirty な checkout を拒否し、元の checkout を書き換え
  ません。linked worktree はそのまま使い、そうでなければ一時ディレクトリに
  detached worktree を作成します（`--worktree-provider orca` で Orca 連携も
  選べます）。再適応は隔離された agent プロセスで実行され、その成果物は別の
  認証情報を持たない別の経路で検証されて初めて hash として記録されます。

詳細な設計、CI での使い方、確認済みのテストとの対応表はそれぞれ
[README.md](../README.md)、[docs/ci.md](ci.md)、[docs/parity.md](parity.md) に
あります。

Licensed under the [MIT license](../LICENSE).
