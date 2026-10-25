# PR の変更チェックと定期更新を分ける

skill の自動化には GitHub Agentic Workflows（`gh-aw`）を使う。
PR のチェックは導入済みの内容を審査し、定期更新は新しい原本を取得して draft PR を作る。
この二つは別の workflow にする。どちらも同じ skillctrl の基本コマンドを使い、
agent の実行・権限・チェック・公開は呼び出し側で管理する。

## PR の hash チェックが落ちたときだけ審査する

agent を起動する前に、PR の checkout で `skillctrl check` を実行する。
受理済み hash の対象は、upstream 登録と保存した intent の両方がある skill に限る。
skillctrl と `jq` を含むツールを repo の設定で固定して導入する。
GitHub Actions の step で、審査が必要かを出力する。

```bash
set -euo pipefail
skillctrl check > skillctrl-check.json
lock_changed="$(jq -er '.local.lock_changed | if type == "boolean" then tostring else error("invalid lock_changed") end' skillctrl-check.json)"
if [ "$lock_changed" = false ]; then
  echo 'review_needed=false' >> "$GITHUB_OUTPUT"
else
  echo 'review_needed=true' >> "$GITHUB_OUTPUT"
fi
```

hash の不一致は JSON に報告され、コマンド自体は exit 0 で終了する。
`review_needed=true` なら agent に渡し、`false` なら AI 審査なしで成功とする。
この事前判定では、不一致でシェルを終了させず、審査に進めるようにする。
コマンドのエラーや不正な結果では step を失敗にし、内容を受理する理由にはしない。
この workflow では upstream を取得せず、intent の変更検知も加えない。
intent だけを編集しても審査は起動しない。

hash の不一致が報告されたら、該当する skill だけを agent に渡す。
現在の intent と、該当 skill の PR の差分（実行権限の変更を含む）を読む。
差分から始め、ほかのファイルは差分や intent が必要とするときだけ読む。たとえば、
変更箇所が参照する resource や、変更された script である。「特定の語を一切含めない」
のような skill 全体に関わる intent の要件は、全ファイルを読まずに skill 内を検索して確認する。
skill のディレクトリ全体を読むのは、新しく登録した skill のように比較対象の旧版がないときだけにする。
次の表に従って判定する。

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

根拠のコメントと lock の変更を PR ブランチに保存し、最終的な hash の判定と
repo のチェックが通ることを必須にする。

```bash
set -euo pipefail
skillctrl check > skillctrl-check.json
jq -e '.local.lock_changed == false' skillctrl-check.json
```

これらのチェックは最終 commit に対して実行する。公開前に PR の head を再確認し、
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
skillctrl update chosen-skill > skillctrl-update.json
jq -e '.updates | type == "array"' skillctrl-update.json
```

すべての登録済み skill が対象のときだけ、`update` の名前を省略する。
原本に変更がなければローカルの調整は保持され、`updates` にも現れない。
単一の原本が変わると、現在の skill へ 3-way merge される。upstream の変更は適用され、
それ以外の箇所のローカル編集は残り、同じ箇所の編集は衝突として報告される。
統合 skill では登録済みの参照原本が更新され、routing 本文は保持される。
統合 routing を調整するときは、登録済みの原本 snapshot を変更しない。

`updates` の各要素は、status に応じて必要なものだけを agent に渡す。
すべての skill の patch 全体を渡さない。

| `status` | agent に渡すもの | agent の作業 |
| --- | --- | --- |
| `merged` | intent と `git diff -- <path>` | 適用された upstream の変更を intent と照合し、intent に反する箇所だけ編集する |
| `conflicted` | intent・`conflicts`・`git diff -- <path>` | 報告された箇所を intent に沿って解消する。バイナリや削除の衝突では、登録済み upstream の source と skill path で新しい原本を確認する |
| `replaced` | intent と skill のディレクトリ全体 | `reason` で復元失敗を確認し、消えたローカル専用ファイルと置き換え後の内容を見直す。tree が到達可能なら次回以降は merge できる |
| `refreshed` | 統合の intent・ルートの `SKILL.md`・変わった入力パスの `git diff` | 更新された原本に routing が合っているか確認する |

`path` は skill のディレクトリ、`changes` は upstream のファイル変更を示す。
base は repo の Git object、upstream 履歴、記録済み Git source commit から探す。
履歴からの復元は全 adapter に対応し、commit の記録は不要である。
update はネットワークを必要とするが、PR の `check` はオフラインのままである。差分以外は
必要なときだけ読む。変更箇所が参照する resource をたどり、skill 全体に関わる intent の要件は
skill 内の検索で確認する。record の前に、
`git grep -n -e '^<<<<<<< local' -- .agents/skills/chosen-skill` などで衝突マーカーが
残っていないことを確認する。確認後にだけ `skillctrl record chosen-skill` を実行する。
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

一致時に推論を起動しないよう、agent の実行前に固定の hash の事前判定を置く。
コメント、PR ブランチへの lock の保存、draft PR の作成、チェック結果には
[safe outputs](https://github.github.com/gh-aw/reference/safe-outputs/) を使う。
定期更新の draft PR で後述の token の使い分けを行うには、信頼できる custom
safe-output publish job を使う。組み込みの `create-pull-request` handler は
PR 用の資格情報を Git 操作にも使うため、PR 作成専用 App token では branch を公開できない。
PR のチェックが書き込める範囲は受理済み lock に限り、変更可能なパスと登録済み原本の整合性は
信頼できる repo のチェックで検証する。必須チェックは、未検証の新しい head ではなく、
実際に確認した commit に対する結果として保存する。

agent job から publish job へ AI の修正を渡すときは、許可された修正を stage し、
`git diff --cached --binary` の出力を、上限付きのメモリ内 stdout バッファを
経由させず patch ファイルへ直接書き出す。
diff コマンドや patch の書き出しに失敗したら、その場で agent job を失敗にする。
patch が空かどうかはファイルサイズで判定する。空でない patch を渡す前に、
agent job 内で、publish job が適用先にするのと同じ基準状態に対して
`git apply --check` を実行し、検証に失敗したら job を失敗にする。
検証先は修正済みの checkout ではなく、その基準状態にする。

`replaced` 後の初回適応や upstream の取り込み全体を戻す修正など、
修正 patch が大きくなりうることを前提にする。agent の入力を差分に絞っても、
出力サイズに上限はつかない。バイナリを含む skill にも publish job が修正を
適用できるよう、バイナリの変更を保持する。

`gh aw compile` で生成し、Markdown の原本と生成した `.lock.yml` の両方を commit する。
編集は Markdown に対して行い、YAML を再生成する。運用前に Actions 上で、一致・適合する不一致・
違反の疑い・判断不能・定期更新を確認する。compile だけでは認証や公開の動作は確認できない。

### 定期更新の draft PR は専用 App の token で作る

**Allow GitHub Actions to create and approve pull requests** が無効だと、
`GITHUB_TOKEN` による `gh pr create` は
`GitHub Actions is not permitted to create or approve pull requests (createPullRequest)`
で失敗する。この repo 設定を有効にすると、十分な token 権限を持つ全 workflow で
PR を作成できるようになり、一つの workflow には限定できない。また、有効にしても
CI・レビューの自動起動は保証されない。`GITHUB_TOKEN` によるイベントは後続の実行を
抑止するか、対応する PR イベントで手動承認を必要とする。

PR の作成には、専用 GitHub App の installation token を推奨する。

1. Repository permissions は **Contents: Read-only** と **Pull requests: Read and
   write** だけにする。Webhook を無効にし、install できる account は App の owner
   だけに限定する。**Only select repositories** で対象 repo だけに install する。
2. Client ID を repo の variable（例: `SKILLCTRL_APP_CLIENT_ID`）、生成した `.pem`
   秘密鍵を repo の secret（例: `SKILLCTRL_APP_PRIVATE_KEY`）に登録する。
   登録後はローカルの秘密鍵ファイルを削除し、決して commit しない。
3. 信頼できる publish step の直前で、現在の repo に限定した token を発行する。
   信頼できる custom publish job では次のように設定する。

   ```yaml
   - name: Mint the PR app token
     id: app-token
     uses: actions/create-github-app-token@64ab729a6294000f5aa4a4bc0b73d1f32bc45510 # v3.3.0
     with:
       client-id: ${{ vars.SKILLCTRL_APP_CLIENT_ID }}
       private-key: ${{ secrets.SKILLCTRL_APP_PRIVATE_KEY }}
       repositories: ${{ github.event.repository.name }}
       permission-pull-requests: write
   - name: Publish the draft PR
     env:
       APP_TOKEN: ${{ steps.app-token.outputs.token }}
     run: |
       GH_TOKEN="$APP_TOKEN" gh pr create --draft --base "$DEFAULT_BRANCH" --head "$UPDATE_BRANCH" --title "chore(skills): update upstreams" --body-file "$REPORT_FILE"
   ```

   publish job は検証と branch の公開を済ませたうえで、`DEFAULT_BRANCH`・
   `UPDATE_BRANCH`・`REPORT_FILE` を設定する。`repositories` は必ず明示する。
   この入力がないと gh-aw の strict mode は compile を拒否する。
   `APP_TOKEN` は `gh pr create` にだけ渡す。branch の push は `contents: write` の
   `GITHUB_TOKEN` のままでよいため、App に Contents の write 権限は不要である。

App が作成した draft PR の head に対して、CI・レビューが起動することを確認する。
その後 bot が書き戻す commit では、`GITHUB_TOKEN` の push は通常、別の workflow を
自動起動しない。新たなチェックが必要なら、適切な権限を持つ App token か、対応する
明示的な dispatch を使う。上記の PR 作成専用 App では push できない。
新しい commit ごとにチェックを確認する。
[GitHub の起動規則](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow)
と [gh-aw の CI 起動](https://github.github.com/gh-aw/reference/triggering-ci/)を参照する。
