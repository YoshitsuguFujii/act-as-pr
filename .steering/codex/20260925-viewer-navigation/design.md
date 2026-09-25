# Design

## 実装アプローチ
既存の `selectTab` / `showCommit` を表示更新に使い、ユーザー操作時だけ `history.pushState` でタブとコミット ID を記録する。初期表示と watch 復元時は現在の履歴項目へ `replaceState` し、`popstate` では履歴の状態を表示へ反映する。URL を新しい route に変えないので、file:// と token 付き watch URL の両方で動く。ファイルリンクのハッシュ履歴はブラウザに任せる。

上矢印の固定ボタンは HTML に一つ置き、scroll 位置で hidden を切り替える。押下時は reduced motion 設定を尊重してページ先頭へ移動する。CSS は既存の色変数を使う。

## 変更するコンポーネント
- `viewer.html`: 上矢印ボタン。
- `viewer.css`: 右下固定、light/dark、focus と狭い画面への対応。
- `viewer.js`: スクロール状態、履歴の記録・復元、watch との整合。
- `main_test.go` と実ブラウザ smoke: 出力と利用者操作の検証。

## データ構造の変更
Git モデル・保存形式に変更なし。履歴項目に画面の tab/commit を保持する。watch 既存の sessionStorage はスクロール等の復元に継続使用する。

## 影響範囲
Git 処理、HTTP endpoint、snapshot の自己完結性には触れない。既存の file anchor、コミット選択、watch reload が履歴と競合しないか実ブラウザで検証する。
