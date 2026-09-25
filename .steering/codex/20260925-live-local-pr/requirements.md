# Live local PR viewer

## 機能
既存の `act-as-pr <base>` は file:// の自己完結 snapshot として維持する。同一 HTML に Files changed、Commits（各 commit の差分と Uncommitted changes）、All changes を含める。`--watch` は loopback のみの一時 HTTP サーバーで自動更新する。

## ユーザーストーリー
開発者として、push 前のローカルブランチを PR のように閲覧し、commit 単位と未コミットを区別しつつ、編集中の変化をブラウザで追いたい。

## 受け入れ条件
- Files changed: merge-base(base, HEAD) → HEAD。未コミットを含まない。
- Commits: 同範囲の commit を列挙し、各 commit は第一親 → commit の差分を表示。root commit は空 tree → commit。未コミットは HEAD → 作業状態の疑似 commit。
- commit 詳細で Commits タブを再度押すと一覧へ戻る。
- All changes: merge-base → 作業状態。追跡済みの staged/unstaged と、ignore 対象外の untracked を含む。
- All changes と未コミット詳細のファイル見出し・サイドバーで staged/unstaged/untracked を区別する。同じファイルに staged と unstaged があれば両方表示する。
- snapshot は自己完結 file://、即時終了、ネットワーク・サーバーなし。
- watch は 127.0.0.1:0、crypto/rand token、GET/HEAD のみ、SSE で更新通知。編集中の二度目の変更、stage、commit を検出し、表示位置・タブ・モード・開閉状態を可能な範囲で維持する。
- Git/index/refs を通常動作で変更せず、既存の XSS 対策と機能を保持する。
- 全自動テスト、vet、ビルド、snapshot/watch のブラウザ E2E を完了する。
- 最終画面のスクリーンショットを README に掲載する。

## 制約
Go 標準ライブラリ、1 実行ファイル、Git 引数配列、埋め込み UI。コメント・編集・GitHub API・外部通信なし。比較データ全体に明示的な上限を設け、超過時はエラー。push/tag/release はしない。
