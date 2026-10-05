# ローカルと自動化で同じコマンドを使う

skillctrl は原本の取得と明示的な hash の記録を担当します。
AI の起動、タイマー、commit、push、PR 作成は行いません。
手動操作、定期実行、CI の agent は、現在の checkout で同じコマンドを使います。

## 未記録の変更があれば CI を落とす

repo のツール設定でバージョンを固定した CLI と `jq` を導入し、repo 内で実行します。

```bash
set -euo pipefail
skillctrl check > skillctrl-check.json
jq -e '.local.lock_changed == false' skillctrl-check.json
```

`check` は skill ディレクトリ全体の hash と受理 lock を比較します。
upstream 登録と intent の両方がある skill だけが対象です。
整理が必要な対象外の古い受理記録も報告します。
upstream の参照や、intent を満たすかの判定は行いません。
差分があるだけでは正常終了するため、`jq` の条件で CI を落とします。
入力が不正、lock が読めないなどのエラーは `check` 自体が失敗します。

intent は自由に編集でき、その変更だけでは skill の hash は変わりません。
intent を変えたら、skill がそれに合うか確認・編集してから内容を明示的に記録します。
hash チェックの成功は記録した内容との一致を示し、intent を満たす証明にはなりません。

## 定期的に原本を更新する

scheduler も、人や agent と同じ手順を呼びます。
`chosen-skill` はローカル名に置き換えます。`update` の名前を省くと全登録 skill を取得します。

```bash
set -euo pipefail
skillctrl check chosen-skill
skillctrl update chosen-skill
git diff -- .agents/skills/chosen-skill skills-lock.json .agents/skillctrl/upstreams.json
# Review the saved intent and the whole changed skill, then edit and verify it.
skillctrl record chosen-skill
skillctrl check > skillctrl-check.json
jq -e '.local.lock_changed == false' skillctrl-check.json
```

最初の check で既存の hash 差分を確認し、取り込み前に内容を見ます。
未コミットの skill 編集を置き換える取り込みは拒否します。
原本が変わっていなければローカルの調整を保持します。
単体の原本が変わると skill 本文を置き換え、merged skill は登録済み references を更新して
ルートの routing 本文を保持します。diff は取り込み前後の比較で、旧原本と新原本を
別途再構築して比べるものではありません。references、scripts、実行属性も確認します。

`.agents/skillctrl/intents/chosen-skill.md` があれば読み、最終的な内容が intent に沿うか
検証してから `record chosen-skill` で hash を計算・保存します。未検証の結果は記録しません。
intent のない skill は受理 hash が不要なので record を省きます。
引数なしの `record` は対象外の hash の整理だけを行い、内容を受理しません。
`update` は受理 hash を更新しません。

scheduler、agent の設定、レビュー指示は呼び出し側で管理します。
agent は導入済みの skillctrl skill を使えますが、CLI はモデルを選択・起動しません。
段階ごとの独自プロトコルや環境変数の plan は不要です。

## PR を経由して公開する

ローカルと CI で同じ repo の検査を実行します。
許可したパスや原本の整合性は固定 script や lint で検査できます。
required check は信頼できる repo の設定に置き、agent の報告だけで判断しません。
hash チェックだけでは変更範囲の制限や intent の検証はできません。

確認後に Git や GitHub CLI でレビュー用ブランチを commit・push し、draft PR を作ります。
許可があれば agent がそのブランチへ push して構いません。job の分割は任意です。
default branch を保護し、検査とレビューを必須にして PR から merge します。
skillctrl は権限や公開を管理しません。

従来の `ci` / `schedule` コマンド群は削除しました。
既存の自動化は上記の基本コマンドへ切り替えてください。
内部のレビュー成果物や段階ごとの環境変数は、現在のインターフェースではありません。
