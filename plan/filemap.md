# UnifiedOverwriteBatchFlow - ファイル構成

```
unified-overwrite-batch-flow/
├── README.md
├── go.mod
├── go.sum
├── LICENSE
│
├── uobf.go                     # メインのインターフェース定義 (上記のコード)
├── workflow.go                 # ProcessingWorkflowの詳細実装
├── logger.go                   # Logger関連のユーティリティ
├── errors.go                   # エラー型とハンドリング
│
├── filesystem/                 # ファイルシステム実装
│   ├── filesystem.go           # FileSystemインターフェースの基底実装
│   ├── local.go               # ローカルファイルシステム実装
│   ├── ftp.go                 # FTP/FTPS実装
│   ├── sftp.go                # SFTP実装
│   ├── s3.go                  # Amazon S3実装
│   ├── webdav.go              # WebDAV実装
│   └── factory.go             # ファイルシステムファクトリー
│
├── status/                     # ステータス管理実装
│   ├── status.go              # StatusMemoryインターフェース基底実装
│   ├── kvs.go                 # KVSベースのStatusMemory実装
│   ├── memory.go              # インメモリ実装（テスト用）
│   └── redis.go               # Redis実装
│
├── backlog/                    # バックログ管理実装  
│   ├── backlog.go             # BacklogManagerインターフェース基底実装
│   ├── compressed.go          # gzip圧縮ファイル実装
│   └── json.go                # JSON形式のヘルパー
│
├── config/                     # 設定管理
│   ├── config.go              # 設定構造体
│   ├── loader.go              # YAML/JSON設定ファイル読み込み
│   └── validation.go          # 設定値検証
│
├── internal/                   # 内部パッケージ（外部に公開しない）
│   ├── worker/                # ワーカープール実装
│   │   ├── pool.go
│   │   └── worker.go
│   ├── retry/                 # 再試行ロジック
│   │   └── retry.go
│   ├── progress/              # 進捗管理
│   │   └── tracker.go
│   └── minimatch/             # minimatchパターンマッチング
│       └── matcher.go
│
├── examples/                   # 使用例
│   ├── basic/                 # 基本的な使用例
│   │   └── main.go
│   ├── advanced/              # 高度な設定例
│   │   └── main.go
│   ├── config-files/          # 設定ファイルサンプル
│   │   ├── basic.yaml
│   │   └── production.yaml
│   └── adapters/              # ロガーアダプター例
│       ├── logrus_adapter.go
│       ├── zap_adapter.go
│       └── slog_adapter.go
│
├── tests/                      # テストファイル
│   ├── integration/           # 統合テスト
│   │   ├── filesystem_test.go
│   │   ├── workflow_test.go
│   │   └── end_to_end_test.go
│   ├── mocks/                 # モックオブジェクト
│   │   ├── filesystem_mock.go
│   │   ├── status_mock.go
│   │   └── backlog_mock.go
│   └── testdata/              # テストデータ
│       ├── sample_files/
│       └── config_samples/
│
└── docs/                       # ドキュメント
    ├── architecture.md        # アーキテクチャ設計
    ├── configuration.md       # 設定ガイド
    ├── filesystem_guide.md    # ファイルシステム別設定
    ├── deployment.md          # デプロイメントガイド
    └── troubleshooting.md     # トラブルシューティング
```

## パッケージ構成の設計思想

### 📦 **パッケージ分離**

- **uobf.go**: 全体のインターフェース定義（公開API）
- **filesystem/**: 各種ファイルシステム実装
- **status/**: ステータス管理の各種実装
- **backlog/**: バックログファイル管理

### 🔒 **internal/パッケージ**

- 外部からアクセスされたくない実装詳細
- ワーカープール、再試行ロジック、進捗管理など

### 📝 **設定ファイル対応**

- YAML/JSON設定ファイルのサポート
- 環境別設定の管理

### 🧪 **テスト構成**

- 単体テスト（各パッケージ内の *_test.go）
- 統合テスト（tests/integration/）
- モック（tests/mocks/）

### 📖 **ドキュメント**

- アーキテクチャから運用まで網羅
- 設定例とトラブルシューティング

## go.mod の例

```go
module github.com/your-org/unified-overwrite-batch-flow

go 1.21

require (
    github.com/aws/aws-sdk-go-v2 v1.24.0
    github.com/pkg/sftp v1.13.6
    github.com/studio-b12/gowebdav v0.9.0
    github.com/go-redis/redis/v8 v8.11.5
    // その他の依存関係
)
```

この構成はいかがでしょうか？追加したい機能や変更したい構成があれば教えてください！
