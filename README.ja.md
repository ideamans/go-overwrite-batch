# OverwriteBatch

様々なファイルシステムタイプに対応した統一的なバッチ処理を提供するGoライブラリです。大量のファイルセットの効率的なスキャン、フィルタリング、ダウンロード、処理、上書きアップロードを実現します。

## 特徴

- **統一ファイルシステムインターフェース**: ローカル、FTP、SFTP、S3、WebDAVファイルシステムをサポート
- **効率的なバッチ処理**: 最適なパフォーマンスのための2フェーズワークフロー
- **状態管理**: 永続的なステータストラッキングによる重複処理の防止
- **圧縮バックログ**: 効率的なストレージのためのGzip圧縮ファイルリスト
- **並行処理**: 並列ファイル操作のための設定可能なワーカープール
- **リトライロジック**: 指数バックオフ付きの組み込みリトライメカニズム
- **国際化**: 多言語サポート（英語と日本語）

## インストール

```bash
go get github.com/ideamans/go-overwrite-batch
```

## クイックスタート

```go
package main

import (
    "context"
    "log"
    
    uobf "github.com/ideamans/go-overwrite-batch"
    "github.com/ideamans/go-overwrite-batch/backlog"
    "github.com/ideamans/go-overwrite-batch/filesystem"
    "github.com/ideamans/go-overwrite-batch/status"
)

func main() {
    // ファイルシステムインスタンスを作成
    fs := filesystem.NewLocalFileSystem("/source/path")
    
    // LevelDBでステータスメモリを作成
    statusMem, err := status.NewLevelDBStatusMemory("/tmp/status.db")
    if err != nil {
        log.Fatal(err)
    }
    defer statusMem.Close()
    
    // バックログマネージャーを作成
    backlogMgr := backlog.NewGzipBacklogManager("/tmp/backlog.gz")
    
    // ワークフローを作成
    workflow := &uobf.OverwriteWorkflow{
        SourceFS:       fs,
        DestFS:         fs,
        StatusMemory:   statusMem,
        BacklogManager: backlogMgr,
        Concurrency:    4,
    }
    
    // ウォークオプションを定義
    walkOpts := &uobf.WalkOptions{
        Include: []string{"*.txt", "*.log"},
        Exclude: []string{"temp/*"},
    }
    
    // 処理関数を定義
    processFn := func(ctx context.Context, relPath string, content []byte) ([]byte, error) {
        // ここに処理ロジックを記述
        return content, nil
    }
    
    // ワークフローを実行
    ctx := context.Background()
    err = workflow.Execute(ctx, walkOpts, processFn)
    if err != nil {
        log.Fatal(err)
    }
}
```

## アーキテクチャ

ライブラリは2フェーズワークフローを実装しています：

### フェーズ1: スキャン＆フィルタ

- ソースファイルシステムをトラバース
- include/excludeパターンに基づいてファイルをフィルタリング
- 重複を避けるため処理ステータスをチェック
- ファイルパスを圧縮バックログに書き込み

### フェーズ2: ファイル処理

- バックログからファイルパスを読み取り
- ワーカープールを使用して並列でファイルをダウンロード
- ユーザー定義の処理関数を適用
- 処理済みファイルを宛先にアップロード
- 処理ステータスを更新

## コンポーネント

### FileSystemインターフェース

異なるストレージタイプに対する統一的な操作を提供：

- `Walk`: フィルタリングオプション付きでディレクトリ構造をトラバース
- `Overwrite`: ダウンロード、コールバック経由の処理、オプションのアップロードを1つの原子的操作で実行
  - コールバックはファイルメタデータとダウンロードしたファイルパスを受け取る
  - 処理済みファイルパス、autoRemoveフラグ、エラーを返す
  - 処理済みパスが空の場合、アップロードはスキップされる（意図的なスキップ）
  - autoRemoveがtrueで処理済みパスがソースと異なる場合、アップロード後に処理済みファイルは自動削除される
  - エラーハンドリングと優雅なクリーンアップをサポート
- `GetURL`: ファイルシステムのベースURL表現を取得
  - ローカルファイルシステムの例：
    - Unix: `/home/user/data` → `file:///home/user/data`
    - Windows: `C:\Users\data` → `file:///C:/Users/data`
    - Windows UNC: `\\server\share` → `file:////server/share`
  - リモートファイルシステムはプロトコルURLを返す（例：`ftp://host/path`、`s3://bucket/prefix`）

### StatusMemoryインターフェース

ファイル処理状態を追跡：

- `NeedsProcessing`: ファイルを処理すべきかチェック
- `MarkAsProcessed`: 処理成功を記録
- `MarkAsFailed`: 処理失敗を記録

### BacklogManagerインターフェース

圧縮ファイルリストを管理：

- `StartWriting`: バックログにファイルパスを書き込み
- `StartReading`: バックログからファイルパスを読み取り
- `CountRelPaths`: 進捗追跡のための総ファイル数をカウント

## 例

完全な動作例については[examples/basic](examples/basic)ディレクトリを参照してください。

## テスト

```bash
# ユニットテストを実行
go test ./...

# 統合テストを実行（Dockerが必要）
go test -tags=integration ./tests/integration/...
```

## ライセンス

このプロジェクトはMITライセンスの下でライセンスされています。詳細はLICENSEファイルを参照してください。
