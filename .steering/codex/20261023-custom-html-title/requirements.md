# HTML タイトル指定

## 機能

CLI の `--title` 引数でプレビューの HTML `<title>` を指定できるようにする。

## ユーザーストーリー

利用者として、複数のプレビュータブを識別しやすい名前にしたい。

## 受け入れ条件

- `--title` 指定時、snapshot と watch のページタイトルに指定文字列が表示される。
- 省略時は従来の「リポジトリ名 · Local pull request · act-as-pr」を表示する。
- HTML として解釈される文字列はテキストとして安全に表示される。
- `v0.5.0` を公開し、`go install github.com/YoshitsuguFujii/act-as-pr@latest` で取得できる。

## 制約

- 既存の base と `--watch` の引数順を維持する。
- リポジトリの差分表示内容を変えない。
