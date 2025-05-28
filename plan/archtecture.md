# UnifiedOverwriteBatchFlow - アーキテクチャドキュメント

## 概要

UnifiedOverwriteBatchFlow (UOBF) は、様々なファイルシステム（ローカル、FTP、SFTP、S3、WebDAV）に対して統一的なバッチ処理を提供するGoライブラリです。大量のファイルを効率的にスキャン・フィルタリング・処理・アップロードするワークフローを実現します。

## アーキテクチャ概要

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              ProcessingWorkflow                             │
│  ┌─────────────────┐  ┌──────────────────┐  ┌─────────────────┐ ┌─────────┐ │
│  │  ScanAndFilter  │  │  ProcessFiles    │  │  Logger         │ │  l10n   │ │
│  │      Phase      │  │      Phase       │  │  Integration    │ │ Package │ │
│  └─────────────────┘  └──────────────────┘  └─────────────────┘ └─────────┘ │
└─────────────────────────────────────────────────────────────────────────────┘
                               │
          ┌────────────────────┼────────────────────┐
          │                    │                    │
┌─────────▼─────────┐ ┌────────▼────────┐ ┌────────▼────────┐
│   FileSystem      │ │  StatusMemory   │ │ BacklogManager  │
│   Interface       │ │  Interface      │ │  Interface      │
├───────────────────┤ ├─────────────────┤ ├─────────────────┤
│ • Walk            │ │ • NeedsProcess  │ │ • WriteBacklog  │
│ • Download        │ │ • ReportDone    │ │ • ReadBacklog   │
│ • Upload          │ │ • ReportError   │ │ • CountBacklog  │
└───────┬───────────┘ └─────────────────┘ └─────────────────┘
        │
┌───────▼───────────────────────────────────────────────────────┐
│                 FileSystem Implementations                    │
├─────────┬─────────┬─────────┬─────────┬─────────┬─────────────┤
│ Local   │   FTP   │  FTPS   │  SFTP   │   S3    │   WebDAV    │
└─────────┴─────────┴─────────┴─────────┴─────────┴─────────────┘
```

## コアコンポーネント

### 1. ProcessingWorkflow (中央制御)

**役割**: 全体のワークフロー制御と各コンポーネント間の調整

```go
type ProcessingWorkflow struct {
    fs             FileSystem
    statusMemory   StatusMemory  
    backlogManager BacklogManager
    logger         Logger
}
```

**責任範囲**:

- **フェーズ管理**: スキャン→フィルタリング→処理の2段階実行
- **エラーハンドリング**: 各フェーズでのエラー処理と継続判断
- **ログ統合**: 全コンポーネントへのLogger伝播
- **進捗管理**: ProgressCallbackによる進捗レポート

**主要メソッド**:

- `ScanAndFilter()`: ファイルスキャンとバックログ作成
- `ProcessFiles()`: バックログからの並列処理実行
- `SetLogger()`: 全コンポーネントへのLogger設定

### 2. FileSystem Interface (ファイルシステム抽象化)

**役割**: 異なるファイルシステムプロトコルの統一インターフェース

```go
type FileSystem interface {
    Walk(ctx context.Context, rootPath string, options WalkOptions, ch chan<- FileInfo) error
    Download(ctx context.Context, remotePath, localPath string) error
    Upload(ctx context.Context, localPath, remotePath string) (*FileInfo, error)
    Close() error
    SetLogger(logger Logger)
}
```

**責任範囲**:

- **プロトコル抽象化**: FTP、SFTP、S3等の違いを隠蔽
- **接続管理**: プロトコル固有の接続確立・維持・切断
- **ファイル操作**: 統一的なファイル操作API提供
- **エラー変換**: プロトコル固有エラーを共通エラー型に変換

**WalkOptionsによるフィルタリング**:

```go
type WalkOptions struct {
    Include        []string  // open-match.dev/open-matchを使用したパターンでの包含
    Exclude        []string  // open-match.dev/open-matchを使用したパターンでの除外  
    FollowSymlinks bool      // シンボリックリンク追跡
    MaxDepth       int       // 探索深度制限
    FilesOnly      bool      // ファイルのみ（ディレクトリ除外）
}
```

**実装バリエーション**:

- **LocalFileSystem**: os.File、filepath.Walk使用
- **FTPFileSystem**: FTPクライアントライブラリ使用
- **SFTPFileSystem**: SSH + SFTPプロトコル
- **S3FileSystem**: AWS SDK使用、ListObjects + GetObject/PutObject
- **WebDAVFileSystem**: HTTP + WebDAVプロトコル

### 3. StatusMemory Interface (処理状態管理)

**役割**: ファイル処理状態の永続化とトリアージ

```go
type StatusMemory interface {
    NeedsProcessing(ctx context.Context, entries <-chan FileInfo) (<-chan FileInfo, error)
    ReportDone(ctx context.Context, fileInfo FileInfo) error
    ReportError(ctx context.Context, fileInfo FileInfo, err error) error
    SetLogger(logger Logger)
}
```

**責任範囲**:

- **処理判定**: 過去の処理履歴に基づく再処理要否判断
- **状態永続化**: 成功・失敗情報のKVS保存
- **冪等性保証**: 同じファイルの重複処理防止
- **リトライ制御**: 失敗ファイルの再処理管理

**データモデル例**:

```
Key: "completed:/path/to/file.txt"
Value: {
    "name": "file.txt",
    "size": 1024,
    "mod_time": "2025-05-28T10:00:00Z",
    "processed_at": "2025-05-28T10:05:00Z"
}

Key: "error:/path/to/problematic.txt"  
Value: {
    "file": {...},
    "error": "network timeout during upload",
    "timestamp": "2025-05-28T10:03:00Z",
    "retry_count": 3
}
```

**実装バリエーション**:

- **KVSStatusMemory**: 汎用KVSストレージ使用
- **LevelDBStatusMemory**: LevelDB使用、組み込み型ストレージ
- **MemoryStatusMemory**: インメモリ、テスト用途

### 4. BacklogManager Interface (処理待ちファイル管理)

**役割**: 処理対象ファイルリストの圧縮保存・読込

```go
type BacklogManager interface {
    StartWriting(ctx context.Context, entries <-chan FileInfo) error
    StartReading(ctx context.Context) (<-chan FileInfo, error)
    CountEntries(ctx context.Context) (int64, error)
    SetLogger(logger Logger)
}
```

**責任範囲**:

- **大容量対応**: 数万〜数十万件のファイルリスト管理
- **圧縮効率**: gzip圧縮によるディスク使用量削減
- **ストリーミング**: メモリ効率的な逐次読み書き
- **進捗算出**: 事前カウントによる正確な進捗表示

**ファイル形式**:

```json
// /tmp/backlog.json.gz (gzip圧縮)
{"name":"file1.txt","size":1024,"path":"/data/file1.txt",...}
{"name":"file2.txt","size":2048,"path":"/data/file2.txt",...}
```

### 5. l10n Package (国際化・多言語対応)

**役割**: ログメッセージ、エラーメッセージ、ユーザー向けテキストの多言語化

```go
// l10n/l10n.go
func T(phrase string) string         // 翻訳関数
func Register(lang string, lex LexiconMap) // 翻訳辞書登録
func DetectLanguage()                // 環境変数からの言語自動検出
```

**責任範囲**:

- **言語検出**: 環境変数（LANGUAGE, LC_ALL, LC_MESSAGES, LANG）からの自動言語判定
- **翻訳管理**: 基本フレーズと翻訳フレーズのマッピング管理
- **フォールバック**: 翻訳が存在しない場合のオリジナルフレーズ返却
- **拡張性**: 追加言語・翻訳の動的登録

**対応言語**:

- **English (en)**: デフォルト言語
- **Japanese (ja)**: 第一対応言語

**アーキテクチャでの統合**:

```go
// 全コンポーネントでl10n.T()を使用
logger.Info(l10n.T("Starting file processing"), "count", fileCount)
logger.Error(l10n.T("Failed to connect to filesystem"), "error", err)

// エラーメッセージの国際化
return fmt.Errorf(l10n.T("file not found: %s"), filename)

// 翻訳辞書の登録
l10n.Register("ja", l10n.LexiconMap{
    "Starting file processing": "ファイル処理を開始します",
    "Failed to connect to filesystem": "ファイルシステムへの接続に失敗しました",
    "file not found: %s": "ファイルが見つかりません: %s",
})
```

**設計原則**:

1. **全ユーザー向けメッセージの国際化**: ログ、エラー、進捗表示など
2. **英語ベース**: 基本フレーズは英語で記述、他言語は翻訳として追加
3. **透過的統合**: 既存コードへの最小限の変更で国際化対応
4. **拡張可能性**: 新言語・翻訳の容易な追加

## データフロー

### Phase 1: Scan and Filter

```
FileSystem.Walk() 
    ↓ (FileInfo channel)
StatusMemory.NeedsProcessing()
    ↓ (Filtered FileInfo channel)  
BacklogManager.StartWriting()
    ↓
Compressed backlog file
```

**詳細フロー**:

1. **FileSystem.Walk()**: open-match.dev/open-matchパターンでファイルをフィルタリング
2. **Channel Pipeline**: バッチサイズでFileInfoをチャネル送信
3. **StatusMemory**: KVSと照合して処理要否を判定
4. **BacklogManager**: 処理が必要なファイルをgzip圧縮で保存

### Phase 2: Process Files

```
BacklogManager.StartReading()
    ↓ (FileInfo channel)
Worker Pool (Concurrent Processing)
    ├─ FileSystem.Download()
    ├─ ProcessFunc() (User Logic)
    ├─ FileSystem.Upload() → FileInfo
    ├─ StatusMemory.ReportDone()
    └─ StatusMemory.ReportError() (on failure)
```

**並列処理詳細**:

1. **Worker Pool**: 指定同時実行数でワーカー起動
2. **Download**: 一時ディレクトリにファイルダウンロード
3. **Process**: ユーザー定義のProcessFuncでファイル加工
4. **Upload**: 加工済みファイルを元の場所に上書きアップロード
5. **Status Update**: 成功・失敗をStatusMemoryに記録

## エラーハンドリング戦略

### 再試行可能エラー

```go
type RetryableError interface {
    error
    IsRetryable() bool
}

type NetworkError struct {
    Operation string
    Cause     error
}
```

**再試行対象**:

- ネットワークタイムアウト
- 一時的な接続エラー
- HTTP 5xx系エラー
- DNS解決失敗

**再試行戦略**:

- 指数バックオフ (1s, 2s, 4s, 8s...)
- 最大試行回数制限
- コンテキストキャンセル対応

### 継続処理

```go
// 個別ファイルの処理失敗は全体を止めない
for fileInfo := range backlogChannel {
    if err := processFile(fileInfo); err != nil {
        logger.Error("Processing failed", "file", fileInfo.Path, "error", err)
        statusMemory.ReportError(ctx, fileInfo, err)
        continue // 次のファイルを処理
    }
}
```

## ログ統合アーキテクチャ

### Logger Interface

```go
type Logger interface {
    Debug(msg string, fields ...interface{})
    Info(msg string, fields ...interface{})  
    Warn(msg string, fields ...interface{})
    Error(msg string, fields ...interface{})
    WithFields(fields map[string]interface{}) Logger
}
```

### ログ伝播パターン

```go
// WorkflowレベルでLogger設定
workflow.SetLogger(logger)

// 各コンポーネントに適切なコンテキストで伝播
fs.SetLogger(logger.WithFields(map[string]interface{}{
    "component": "filesystem",
    "type": "s3"
}))

statusMemory.SetLogger(logger.WithFields(map[string]interface{}{
    "component": "status_memory", 
    "backend": "leveldb"
}))
```

**構造化ログ出力例**:

```json
{
  "timestamp": "2025-05-28T10:00:00Z",
  "level": "info",
  "message": "File uploaded successfully",
  "component": "filesystem",
  "type": "s3",
  "file": "/data/processed/file.txt",
  "size": 2048,
  "duration": "1.2s"
}
```

## 拡張性とプラガビリティ

### 新ファイルシステム追加

```go
type CustomFileSystem struct {
    config CustomConfig
    logger Logger
}

func (c *CustomFileSystem) Walk(ctx context.Context, rootPath string, options WalkOptions, ch chan<- FileInfo) error {
    // カスタムプロトコルの実装
}

// 既存コードは変更不要
workflow := uobf.NewProcessingWorkflow(
    &CustomFileSystem{}, // 新しい実装
    statusMemory,
    backlogManager,
)
```

### 新StatusMemory実装

```go
type DatabaseStatusMemory struct {
    db     *sql.DB
    logger Logger
}

func (d *DatabaseStatusMemory) NeedsProcessing(ctx context.Context, entries <-chan FileInfo) (<-chan FileInfo, error) {
    // SQLデータベースベースの実装
}
```

## パフォーマンス特性

### スケーラビリティ要因

- **ファイル数**: 数百万件まで対応（BacklogManagerの圧縮効率）
- **同時実行数**: CPUコア数 × 2-4程度が最適
- **メモリ使用量**: ストリーミング処理によりファイル数に依存しない
- **ディスク使用量**: gzip圧縮により1/5-1/10に削減

### ボトルネック対策

- **StatusMemory**: LevelDBの最適化、データベースファイルの配置
- **FileSystem**: 接続プール、Keep-Alive
- **BacklogManager**: SSDの使用、一時ファイルの配置最適化

## セキュリティ考慮事項

### 認証・認可

- **SSH鍵認証**: SFTP接続でのキーベース認証
- **IAM**: S3アクセスでの最小権限の原則
- **TLS**: FTPS、WebDAVでの暗号化通信

### 機密情報管理

- **接続情報**: 環境変数での管理推奨
- **一時ファイル**: 処理後の確実な削除
- **ログ**: 機密情報のマスキング

## グレースフルシャットダウン

### 基本方針

システムの安全な停止を保証し、特にファイルアップロード中の中断を防ぐ仕組みを提供します。

### シャットダウンフロー

```go
// シャットダウン処理の例
func (w *ProcessingWorkflow) Shutdown(ctx context.Context) error {
    // 1. 新しいタスクの受付停止
    w.stopAcceptingNewTasks()
    
    // 2. 現在進行中のタスクを監視
    // ダウンロード・処理フェーズは即座に停止可能
    // アップロードフェーズは完了を待つ
    
    // 3. リソースクリーンアップ
    return w.cleanup(ctx)
}
```

### アップロード保護

**要件**: ファイルアップロード中はシャットダウン信号に応じない

```go
// アップロード処理では独立したコンテキストを使用
func (w *ProcessingWorkflow) uploadFile(fileInfo FileInfo, localPath string) error {
    // アップロード専用コンテキスト（キャンセル不可）
    uploadCtx := context.Background()
    
    // アップロード完了まで中断されない
    uploadedInfo, err := w.fs.Upload(uploadCtx, localPath, fileInfo.Path)
    if err != nil {
        return err
    }
    
    // ステータス更新
    return w.statusMemory.ReportDone(uploadCtx, *uploadedInfo)
}
```

### ワーカープール管理

```go
type WorkerPool struct {
    workers       []*Worker
    shutdownCh    chan struct{}
    uploadingTasks sync.WaitGroup
}

func (p *WorkerPool) Shutdown(timeout time.Duration) error {
    // シャットダウン開始
    close(p.shutdownCh)
    
    // アップロード中のタスク完了を待機
    done := make(chan struct{})
    go func() {
        p.uploadingTasks.Wait()
        close(done)
    }()
    
    select {
    case <-done:
        return nil
    case <-time.After(timeout):
        return errors.New("shutdown timeout: uploads still in progress")
    }
}
```

### リソースクリーンアップ

1. **ファイルシステム接続**: 全接続の適切なクローズ
2. **一時ファイル**: 未完了タスクの一時ファイル削除
3. **StatusMemory**: データベース接続のクローズ
4. **ログファイル**: バッファのフラッシュ

## テスト戦略

### 単体テスト

各パッケージの基本機能をテストし、インターフェースの動作を検証します。

### Dockerコンテナを使用した統合テスト

リモートファイルシステムの実装は、実際のプロトコルサーバーを模擬したDockerコンテナを使用してテストします。

#### 対応プロトコル

- **SFTP**: `atmoz/sftp` コンテナ
- **FTP**: `stilliard/pure-ftpd` コンテナ  
- **WebDAV**: `bytemark/webdav` コンテナ
- **S3**: `minio/minio` コンテナ（S3互換API）

#### testcontainers-goを使用したテスト実行フロー

```go
//go:build integration

import (
    "context"
    "testing"
    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/wait"
)

func TestSFTPFileSystem_E2E(t *testing.T) {
    ctx := context.Background()
    
    // 1. SFTPコンテナを動的に起動
    req := testcontainers.ContainerRequest{
        Image:        "atmoz/sftp",
        ExposedPorts: []string{"22/tcp"},
        Cmd:          []string{"testuser:testpass:1001"},
        WaitingFor:   wait.ForListeningPort("22/tcp"),
    }
    
    container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
        ContainerRequest: req,
        Started:          true,
    })
    require.NoError(t, err)
    defer container.Terminate(ctx)
    
    // 2. 動的ポート取得
    host, _ := container.Host(ctx)
    port, _ := container.MappedPort(ctx, "22")
    
    // 3. ファイルシステム接続
    config := SFTPConfig{
        Host:     host,
        Port:     port.Int(),
        Username: "testuser",
        Password: "testpass",
    }
    fs := NewSFTPFileSystem(config)
    
    // 4. 実際のファイル操作テスト
    testFileOperations(t, fs)
}
```

#### dockertest/v3を使用した軽量テスト

```go
//go:build integration

import (
    "github.com/ory/dockertest/v3"
    "github.com/ory/dockertest/v3/docker"
)

func TestFTPFileSystem_E2E(t *testing.T) {
    pool, err := dockertest.NewPool("")
    require.NoError(t, err)
    
    // 1. FTPコンテナ起動
    resource, err := pool.RunWithOptions(&dockertest.RunOptions{
        Repository: "stilliard/pure-ftpd",
        Tag:        "hardened",
        Env: []string{
            "PUBLICHOST=localhost",
            "FTP_USER_NAME=testuser",
            "FTP_USER_PASS=testpass",
        },
    })
    require.NoError(t, err)
    defer pool.Purge(resource)
    
    // 2. 接続確認
    pool.Retry(func() error {
        config := FTPConfig{
            Host:     "localhost",
            Port:     resource.GetPort("21/tcp"),
            Username: "testuser",
            Password: "testpass",
        }
        fs := NewFTPFileSystem(config)
        return fs.Connect()
    })
    
    // 3. テスト実行
    testFileOperations(t, fs)
}

### E2Eテスト

実際のワークフロー全体（スキャン→フィルタリング→処理）をDockerコンテナ環境で実行し、エンドツーエンドの動作を検証します。

このアーキテクチャにより、大規模なファイル処理を効率的かつ安全に実行できるフレームワークを提供します。
