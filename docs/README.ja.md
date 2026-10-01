![skillctrl](wordmark.svg)

# skillctrl

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

3 つの経路は同じ binary を入れます。`skillctrl --version` が版を表示します。

## 使い方

```sh
skillctrl find browser --owner vercel-labs   # 公開 index を検索。導入はしない
skillctrl add vercel-labs/agent-browser --skill agent-browser
skillctrl update                            # 登録済みの skill をすべて更新
skillctrl update agent-browser              # 1 つだけ更新
skillctrl remove agent-browser
skillctrl status                            # accepted lock との差分
skillctrl record agent-browser              # 意図的な手動編集を確定
skillctrl schema                            # 機械可読なコマンド仕様
```

標準出力は JSON、ログとエラーは stderr に出ます。exit 2 は再適応の一部が未確定、
exit 1 は失敗です。作業内容は worktree に残るため、commit・push・PR 作成は
skillctrl 側では行いません。

## 仕組み

- **原本は Git から直接取得します。** shallow clone の追跡済みファイルだけを
  読み、外部の installer・script・hook は一切実行しません。symlink、submodule、
  `.gitignore` / `.gitattributes`、配置先で除外されるファイルを含む skill は
  拒否します。実行ビットとバイナリ内容も保持します。
- **意図ファイルが仕様です。** `.agents/skillctrl/intents/<name>.md` に「何を
  保ち続けるべきか」を書きます。更新で意図が壊れるときは、本文・description・
  examples を現在の意図に合わせて書き換えます。意図の変更・削除だけを理由に
  再適応が起動することはありません。hash が一致済みの skill は再検討しません。
- **2 つの lock。** `.agents/.skill-lock.json`（version 3）は取得元の記録、
  `.agents/skillctrl/intents/lock.json`（version 2）は「受け入れ済みの実体」の
  Git tree hash の記録です。skill ディレクトリ全体（本文・reference・実行属性）
  が hash に含まれ、意図ファイルは含まれません。判断がつかない skill は
  そのまま残し、古い hash を保持して未確定として報告します。削除された skill
  と意図の組み合わせは自動復旧せず、報告します。
- **隔離。** installer は dirty な checkout を拒否し、元の checkout を書き換え
  ません。linked worktree はそのまま使い、そうでなければ一時ディレクトリに
  detached worktree を作成します（`--worktree-provider orca` で Orca 連携も
  選べます）。再適応は隔離された agent プロセスで実行されます。その toolchain は
  検証済みの設定から解決し、レビュー対象の repository の設定は評価しません。
- **検証は別経路で行います。** agent の成果物は無信頼とみなされ、認証情報を持た
  ない別の経路が plan を再計算し、対象 skill 以外の変更、accepted / unresolved
  の不完全な分割、機密情報の混入を検査してからだけ hash を記録します。

詳細な設計は [README.md](../README.md)、CI での使い方は [docs/ci.md](ci.md)、
確認済みのテストとの対応表は [docs/parity.md](parity.md) を参照してください。

[MIT ライセンス](../LICENSE)で公開しています。
