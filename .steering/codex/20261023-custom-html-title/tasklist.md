# タスク

- [x] 対象箇所とベースブランチの鮮度を確認する（main 自身がベース）
- [x] 仕様テストを追加し、失敗を確認する
- [x] `--title` を実装し、Go テストを通す
- [x] README とブラウザ表示、スクリーンショットを確認する
- [x] 敵対的レビューで見つかった `--` 始まりのタイトルを受け付け、再検証する
- [x] 変更をコミットして main と v0.5.0 を push する
- [x] Go モジュールプロキシの `@latest` とインストール済みバイナリの版を確認する

公開確認: Go モジュールプロキシの `go list -m github.com/YoshitsuguFujii/act-as-pr@latest` は `v0.5.0` を返した。一時ディレクトリへの `go install ...@latest` が成功し、バイナリのモジュール版と生成 HTML の `<title>--urgent</title>` を確認した。

完了条件: 指定時と省略時の snapshot / watch タイトルを確認し、Go テストが通ること。
