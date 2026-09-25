# Design

## 実装アプローチ
CLI が repository を発見し Git の merge-base と差分を取得する。`--name-status -z` でファイル情報を安全に読み、patch の各ファイルを順序で対応づけて hunk/line のモデルにする。Go の HTML template と埋め込み CSS/JS で単一 HTML を生成し OS 一時領域に保存、macOS の `open` で開く。Linux は `xdg-open` を使う。

## コンポーネント
- `main.go`: CLI、Git 呼び出し、差分パース、HTML 出力。
- `viewer.html`: 埋め込みテンプレート、Unified/Split、navigation、collapse。
- `main_test.go`: 一時 Git リポジトリを使った契約テスト。

## データ構造
ファイル単位の status/path/additions/deletions と hunk/line kind/line numbers を保持する。永続データ変更なし。

## 影響範囲
既存機能なし。Git は参照のみ。ファイル名と差分は HTML エスケープし、JS は静的コードのみとする。巨大な差分は単一 HTML によるメモリ負荷が残るため、Git 出力に 32 MiB の上限を設ける。

実装着手可否: 着手可
