# xapp (Cloud Run Job for X Bot - Go)

アイルランドのニュースを取得・日本語訳して X へ自動投稿する **Go 製 Cloud Run Job** のひな形です。
外部ライブラリなしの標準ライブラリ構成で、OAuth 1.0a 署名による X API v2 投稿と Cloud Logging 向け JSON ログ出力に対応しています。

## ディレクトリ構成

```text
.
├── Dockerfile
├── .dockerignore
├── .env.example
├── .gitignore
├── go.mod
├── cmd/
│   └── job/
│       └── main.go              # Cloud Run Job エントリポイント
└── internal/
    ├── config/
    │   └── config.go            # 環境変数の読み込み・バリデーション
    ├── news/
    │   └── fetcher.go           # ニュース取得・翻訳ロジック（ここを実装）
    └── xclient/
        ├── client.go            # X API v2 投稿クライアント (OAuth 1.0a)
        └── client_test.go       # ユニットテスト
```

## ローカルでの動作確認（WSL）

```bash
cd /home/kenta/xapp

# テスト実行
go test ./...

# DRY_RUN=true でローカル実行（X には投稿されません）
DRY_RUN=true go run ./cmd/job
```

実際に X へテスト投稿する場合は、`.env.example` を参考に `.env` を作成して環境変数を読み込んで実行します。

```bash
cp .env.example .env
# .env に 4 つの X API キーを設定し、DRY_RUN=false に変更
set -a && source .env && set +a
go run ./cmd/job
```

## Cloud Run Job へのデプロイ

X のキーは **Secret Manager** に登録し、`--set-secrets` で環境変数として渡す構成を推奨します。

```bash
gcloud run jobs deploy ireland-news-bot \
  --source=. \
  --region=asia-northeast1 \
  --project=<PROJECT_ID> \
  --max-retries=1 \
  --task-timeout=5m \
  --set-env-vars=DRY_RUN=false \
  --set-secrets=X_API_KEY=x-api-key:latest,X_API_KEY_SECRET=x-api-key-secret:latest,X_ACCESS_TOKEN=x-access-token:latest,X_ACCESS_TOKEN_SECRET=x-access-token-secret:latest,GEMINI_API_KEY=gemini-api-key:latest \
  --quiet
```

## 手動実行テスト

```bash
gcloud run jobs execute ireland-news-bot \
  --region=asia-northeast1 \
  --project=<PROJECT_ID> \
  --wait \
  --quiet
```

定期実行したい場合は、Cloud Run Jobs の画面または Cloud Scheduler からスケジュールトリガーを設定できます。
