# Design

## 実装アプローチ
`inspect` の Git 発見・merge-base 決定を残し、任意の commit/tree と作業状態を扱う共通 `compare` に分ける。名前一覧 (`-z`) と patch は既存 parser を共用する。`git diff <commit>` は作業ツリー全体（staged/unstaged を含む）を比較し、`git ls-files --others --exclude-standard -z` のパスごとに `git diff --no-index /dev/null <path>` から追加ファイルを作る。ファイルを index に加えない。

`App` に Files changed、All changes、commit 一覧・詳細、未コミット詳細、stage/unstage/untracked 件数を保持する。HTML template は差分部分を共通定義として複数回描画し、snapshot はすべてを単一ファイルに埋め込む。各ファイル ID は view 固有の prefix を持つ。

作業状態の各パスに staged/unstaged/untracked のフラグを付け、共通ファイル見出しにフルラベル、サイドバーに短い色付きラベルを出す。Commits タブのクリック時はコミット一覧へ戻し、watch による再描画時は選択中コミットを復元する。

watch は同じ `App` を約750msごとに再計算し、状態ハッシュが変わったときだけ HTML を差し替えて SSE に version を送る。ブラウザは sessionStorage に UI の選択・表示モード・開閉・scroll を保持して reload し、復元する。初回 SSE は現在 version を送り、ページ取得後の更新取りこぼしを防ぐ。

## コンポーネント
- `main.go`: 既存 parser/CLI の共通化。
- `inspection.go`: PR 全体、commit、作業状態、未追跡ファイルの組み立て。
- `watch.go`: loopback listener、session token、read-only routes、poll/SSE、shutdown。
- `viewer.html/css/js`: 共通 diff 部分、タブ/commit 選択、watch 状態復元。
- `*_test.go`: temp Git repositories と HTTP/SSE 契約テスト。
- `README.md`: 2 mode、3 view、安全性と上限。

## データ構造の変更
既存 `View` は一つの比較結果として継続使用。`App` は複数 `View` と `Commit` をまとめる。永続 DB/設定変更なし。

## 影響範囲とリスク
- Git race: watch 中の書き換えで diff 取得が失敗したら最後の正常表示を維持し、次回 poll で再試行する。
- 巨大 branch: commit 数と aggregate patch bytes に上限を設け、黙って省かない。
- 未追跡 symlink/特殊名: NUL 区切りを使い、ファイルを直接 HTML へ挿入しない。
- localhost: 127.0.0.1 へ固定し、token path と Host 検査、CSP、no-store。外部公開しない。
- snapshot 互換: 既存 `inspect/render/writePreview` と CLI の動作を維持し、watch のみ新経路を追加する。

実装着手可否: 着手可
