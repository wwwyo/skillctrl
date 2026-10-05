# PR の変更チェックと定期更新を分ける

skill の自動化には GitHub Agentic Workflows（`gh-aw`）を使う。
PR のチェックは導入済みの内容を審査し、定期更新は新しい原本を取得して draft PR を作る。
この二つは別の workflow にする。どちらも同じ skillctrl の基本コマンドを使い、
agent の実行・権限・チェック・公開は呼び出し側で管理する。

## PR の hash チェックが落ちたときだけ審査する

agent を起動する前に、PR の checkout で `skillctrl check` を実行する。
受理済み hash の対象は、upstream 登録と保存した intent の両方がある skill に限る。
skillctrl と `jq` を含むツールを repo の設定で固定して導入し、次を実行する。

```bash
set -euo pipefail
skillctrl check > skillctrl-check.json
jq -e '.local.lock_changed == false' skillctrl-check.json
```

hash の不一致は JSON に報告され、コマンド自体は exit 0 で終了する。
`jq` の条件で CI の判定を落とす。コマンドのエラーや不正な結果は実行上の失敗であり、
内容を受理する理由にはしない。一致していれば AI 審査なしで成功とする。
この workflow では upstream を取得せず、intent の変更検知も加えない。
intent だけを編集しても審査は起動しない。

hash の不一致が報告されたら、該当する skill だけを agent に渡す。
現在の intent、PR の差分、skill のディレクトリ全体を読む。
references・scripts・実行権限も確認し、次の表に従って判定する。

| 状態 | 処理 |
| --- | --- |
| lock と一致 | AI 審査なしで成功 |
| 不一致だが intent に適合 | 根拠を示し、確認済みの対象だけ `record` |
| intent 違反が疑われる | 該当する intent・差分・修正案をコメントして失敗 |
| 判断できない | 不明点をコメントして失敗 |

受理する場合は、関連する intent の要件と、それを満たす skill の内容や検証結果を示す。
審査した PR の commit と skill 名も記録し、確認済みの対象だけ受理する。

```bash
skillctrl record chosen-skill
```

`record` は現在のディレクトリ全体の hash を計算する操作であり、審査は行わない。
不一致を消すためだけに実行しない。名前を指定した `record` でも、通常の lock 整理として
対象外の古い記録は削除される。ほかの対象内 skill を受理してはいけない。
一部を受理しても未解決の skill が残っていれば、チェック全体は失敗のままとする。
対象外の古い記録を整理するだけの場合は、名前なしの `skillctrl record` を使う。
これは内容を受理せず、intent の適合判定も必要としない。

根拠のコメントと lock の変更を PR ブランチに保存し、最終 commit に対して
hash の判定と repo のチェックを再実行する。公開前に PR の head を再確認し、
審査中に変わっていれば、新しい内容を審査してから記録・成功報告する。
違反の疑いや判断不能の場合は、その skill を記録せず、内容も変更しない。
自動修正せず、コメントで修正案を示す。

判定結果は CI の必須チェックにする。失敗のコメントだけでは workflow は失敗にならない。
違反の疑い・判断不能・実行上のエラーは失敗にする。不一致を受理した場合は、
lock の変更が保存され、最終 commit の検証が通った時点で成功とする。

## 定期実行で upstream を取得して調整する

schedule と任意の手動トリガーを持つ別の workflow を使う。
default branch の clean な checkout から始め、既存の hash の不一致を確認してから
指定した範囲を `update` する。PR のチェックと異なり、この workflow では
原本を取得し、intent を維持するために skill を編集できる。

```bash
set -euo pipefail
skillctrl check chosen-skill
skillctrl update chosen-skill
git diff -- .agents/skills/chosen-skill skills-lock.json .agents/skillctrl/upstreams.json
```

すべての登録済み skill が対象のときだけ、`update` の名前を省略する。
原本に変更がなければローカルの調整は保持される。単一の原本が変われば skill の内容が置き換わり、
統合 skill では登録済みの参照原本が更新され、routing 本文は保持される。
統合 routing を調整するときは、登録済みの原本 snapshot を変更しない。

変更された skill ごとに、現在の intent と取得したディレクトリ全体を読み、
編集できる内容を調整・検証する。確認後にだけ `skillctrl record chosen-skill` を実行する。
intent がない skill は記録しない。公開前に hash の判定と repo のチェックを実行し、
調整が未解決なら問題を報告して受理しない。

確認済みの変更があれば、skill の変更・登録・受理済み hash・根拠を含む draft PR を作る。
変更がなければ PR を作らず終了する。その PR は通常の変更チェックを通す。
upstream の新しい release だけを理由に、無関係な PR を失敗にしない。

## 呼び出し側の repo に agent workflow を設定する

[公式の作成手順](https://docs.github.com/en/copilot/how-tos/github-agentic-workflows/creating-github-agentic-workflows)
と [gh-aw の文書](https://github.github.com/gh-aw/)に従い、`.github/workflows/` に
二つの Markdown workflow を作る。それぞれのトリガーと、上記の実行指示を設定する。
ツールと取得用の依存を固定し、engine・認証・checkout とコマンドへのアクセスを設定する。
資格情報は repo の secret 管理に置く。

一致時に推論を起動しないよう、agent の実行前に固定の hash 判定を置く。
コメント、PR ブランチへの lock の保存、draft PR の作成、チェック結果には
[safe outputs](https://github.github.com/gh-aw/reference/safe-outputs/) を使う。
PR のチェックが書き込める範囲は受理済み lock に限り、変更可能なパスと登録済み原本の整合性は
信頼できる repo のチェックで検証する。必須チェックは、未検証の新しい head ではなく、
実際に確認した commit に対する結果として保存する。

`gh aw compile` で生成し、Markdown の原本と生成した `.lock.yml` の両方を commit する。
編集は Markdown に対して行い、YAML を再生成する。運用前に Actions 上で、一致・適合する不一致・
違反の疑い・判断不能・定期更新を確認する。compile だけでは認証や公開の動作は確認できない。

bot が書き戻した commit の検証方法も設定する。`GITHUB_TOKEN` による push では、
通常は別の workflow が起動しない。適切な GitHub App token か、対応する明示的な dispatch を使い、
新しい commit のチェックを確認する。
[GitHub の起動規則](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow)
と [gh-aw の CI 起動](https://github.github.com/gh-aw/reference/triggering-ci/)を参照する。
