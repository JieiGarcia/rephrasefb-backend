# rephraseFB (Backend API)
🌐 **[Frontend Live Demo (アプリを試す) ](https://rephrase-fb.netlify.app)**

英語を学習する日本語母語話者のためのリアルタイム・ライティング支援アプリ「rephraseFB」のバックエンドAPIです。フロントエンドからのリクエストを受け、AI処理やデータベースへのログ保存を行います。

👉 **フロントエンド（React）のリポジトリは[こちら](https://github.com/JieiGarcia/rephraseFB-frontend)**

## 🌟 役割と機能

- **REST API提供**: フロントエンドからのリクエスト（ユーザー認証、タスク管理、提案生成、ログ保存）を処理。
- **AI統合 (Google Gemini API)**: 高度なプロンプトエンジニアリングにより、学習者の文脈に合わせた自然な英語表現と解説を生成。
- **音声生成 (Google Cloud TTS API)**: 生成された英文テキストから、高品質な音声（MP3）を動的に生成して返却。
- **データ永続化と分析基盤**: PostgreSQLを使用し、研究目的のためのユーザー操作ログ（提案の表示、採用、無視、音声再生など）をトラッキングIDベースで記録。

## 🛠 技術スタック (Backend)

- **Language**: Go (Golang)
- **Router**: go-chi/chi
- **Database**: PostgreSQL
- **Database Driver**: pgx, database/sql
- **External APIs**: Google Gemini 2.5 Flash API, Google Cloud Text-to-Speech API
- **Deployment**: Render

## 💻 ローカルでの開発環境セットアップ

### 前提条件
- Go (v1.21+)
- PostgreSQL (稼働中のローカルまたはリモートDB)
- Google AI API Key
- Google Cloud Service Account JSON (TTS用)

### インストールと起動

1. **リポジトリのクローン**
   ```bash
   git clone <repository-url>
   cd rephrasefb-backend

2. **依存関係のインストール**
    ```bash
    go mod tidy
    
3. **環境変数の設定**
ルートディレクトリに .env を作成します。

GEMINI_API_KEY=your_google_ai_api_key
DATABASE_URL=postgres://user:password@localhost:5432/rephrasefb?sslmode=disable
GOOGLE_SERVICE_ACCOUNT_JSON={"type": "service_account", ...}

4. **サーバーの起動**
    ```bash
    go run main.go

起動時に自動でデータベースのマイグレーションが実行され、ポート 8080 でサーバーが立ち上がります。

### 🗄 データベーススキーマ
- users: 外部ユーザーID（被験者ID/ゲストID）と内部UUIDの紐付け

- tasks: タスク条件（control/experimental）と最終提出テキストの保存

- suggestions: 各提案のトラッキングID、カテゴリ、AIの出力結果、ユーザーの操作（action）の記録

---