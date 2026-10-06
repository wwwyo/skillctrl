<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../../assets/wordmark-dark.svg">
  <img src="../../assets/wordmark.svg" alt="skillctrl" width="312" height="88">
</picture>

[![Go 1.27](https://img.shields.io/badge/go-1.27-blue.svg)](../../go.mod) [![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](../../LICENSE)

[English](../../README.md) · [コマンドの詳細](usage.md) · [Agent workflow のガイド](skills/skillctrl/references/ci.md) · [開発](../requirements.md)

skillctrl は、導入した agent skill を自分のプロジェクトに合わせてカスタマイズするためのツールです。
指示を短くする、独自の確認手順を加える、複数の skill を1つの workflow にまとめる。

原本と変更の意図を記録することで、更新時にも普段の agent がカスタマイズを保ちながら
新しい内容を取り込めるようにします。

https://github.com/user-attachments/assets/48394b56-6be3-47a5-b21a-4e3b3c851d72

## QuickStart

対象リポジトリで、使っている AI に次の prompt を渡します。

```text
https://raw.githubusercontent.com/wwwyo/skillctrl/main/docs/start.md を読み、このリポジトリに skillctrl CLI と skill を導入してください。
```

### Manual

導入方式を1つ選び、CLI をグローバルに導入します。

- Go: `go install github.com/wwwyo/skillctrl@latest`
- Homebrew: `brew install wwwyo/tap/skillctrl`
- mise: `mise use --global github:wwwyo/skillctrl@latest`

互換性のある `skills` CLI か Node.js/npm を用意し、対象の Git リポジトリで skill を導入します。

```sh
skillctrl add wwwyo/skillctrl:skillctrl
```

## Features

- **自分のルールに ✏️**: プロジェクトのルールや確認手順に合った skill を使える。
- **カスタマイズを維持 🔄**: ローカルの要件を保ちながら、upstream の改善を取り込める。
- **ひとつの入口 🧩**: 複数の skill をまとめ、1つの入口から使える。
- **変化が見える 👀**: 受理後に変わったカスタマイズ済み skill が分かる。
- **CI でメンテ 🤖**: [レビューと定期更新](skills/skillctrl/references/ci.md)を自動化し、原本の変更を手で追い続けなくても skill をメンテナンスできる。

## License

[MIT](../../LICENSE) ライセンスで公開しています。
