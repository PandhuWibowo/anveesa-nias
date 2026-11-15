# Anveesa Nias

[English](README.md) · [Bahasa Indonesia](README.id.md) · [简体中文](README.zh-CN.md) · **日本語**

Anveesa Nias はオープンソースの社内開発者プラットフォームです。データベースの安全な閲覧と変更、ダッシュボードの構築、インフラツールの操作、そして「誰が何をしたか」の監査を、ひとつの権限モデルとひとつの承認フローのもとで、ひとつの Web アプリにまとめています。

ひとつの Go バイナリ（標準ライブラリ `net/http`）が API と Vue 3 + Vite のフロントエンドの両方を配信します。

📖 **[Wiki](https://github.com/PandhuWibowo/anveesa-nias/wiki/JA-Home)**：アーキテクチャ、設計判断、既知のペインポイント、ロードマップ（English、Bahasa Indonesia、简体中文、日本語）。

## 主な機能

**データベースとデータストア**
- PostgreSQL、MySQL/MariaDB、SQL Server、SQLite への接続（SSH トンネル経由も可）。認証情報は AES で暗号化して保存。
- SQL Studio：スキーマを考慮した補完付きエディタ、履歴、スニペット、トランザクション、インポート、行の編集、EXPLAIN、ER 図、スキーマ差分、行の変更履歴。
- PostgreSQL/MySQL の運用：アクティブなセッション、スロー/エラー/監査ログ、DB ユーザーと権限（計画 → プレビュー → 適用または承認申請）、レプリケーション、サーバーパラメーター。
- Redis、Memcache、MongoDB、Cassandra、Kafka、Laravel キュー、Elasticsearch/OpenSearch、Alibaba Cloud SLS のログ、オブジェクトストレージのバケット。

**分析**
- テーブル、KPI、棒、折れ線、面、散布図、円、ドーナツのグラフを使えるダッシュボード。PDF、PNG、Excel、CSV、SQL、JSON へのエクスポート、公開リンク、iframe 埋め込み。
- フォルダ付きの保存クエリと、AI による「Ask your data」（OpenAI 互換の任意のプロバイダー）。

**変更管理と自動化**
- 書き込み SQL、change set、データスクリプト、DB 権限付与のための多段階承認ワークフロー。
- 定期クエリ、データパイプライン、バケットへのバックアップ、データの watcher。

**インフラ**
- SSH 経由の Docker、Nginx、crontab のネイティブ編集、SFTP ファイルブラウザ（制限されたパスには sudo 昇格）。
- ライブログと exec ターミナル付きの読み取り専用 Kubernetes エクスプローラー。

**監視とガバナンス**
- ヘルス、クエリパフォーマンス、ログ検索、死活監視、インフラメトリクス、アラート。
- ユーザー、ロール、アクセスグループ、接続ごとの SQL 権限、MFA の強制、すべてのツールをカバーする監査ログ。
- UI は English と简体中文に対応。

## クイックスタート

前提条件：Go 1.25+ と Node.js 20+。

```bash
git clone https://github.com/PandhuWibowo/anveesa-nias.git
cd anveesa-nias
cp .env.example .env
make secrets      # JWT_SECRET、NIAS_ENCRYPTION_KEY、DEFAULT_ADMIN_PASSWORD を出力するので .env に貼り付ける
make install      # npm install + go mod tidy
make dev          # バックエンド :8080、フロントエンド :5173
```

http://localhost:5173 を開きます。デフォルトの `.env.example` は SQLite（`DB_DRIVER=sqlite`、`DATABASE_URL=data.db`）を使うため、試すだけならデータベースサーバーは不要です。

### Docker Compose で起動（PostgreSQL）

```bash
cp .env.example .env   # JWT_SECRET、NIAS_ENCRYPTION_KEY、DEFAULT_ADMIN_PASSWORD を設定
docker compose -f deploy/compose/docker-compose.yml --env-file .env up -d --build
```

http://localhost:8080 を開きます。その他の構成（production、RDS、MySQL、Kafka、Cassandra）は [`deploy/compose/`](deploy/compose/) にあります。

## 初回ログイン

初回起動時に `users` テーブルが空であれば、Anveesa Nias は `.env` から管理者アカウントを 1 つ作成します：

1. `make secrets` を実行し、出力された `DEFAULT_ADMIN_PASSWORD` を `.env` に設定します（`admin` 以外にしたい場合は `DEFAULT_ADMIN_USERNAME` も設定）。
2. アプリを起動し、その認証情報でログインします。
3. ログインしたらすぐにパスワードを変更します。

`DEFAULT_ADMIN_PASSWORD` が未設定の場合はプレースホルダー `admin123` が使われますが、`NIAS_ENV=production` ではサーバーがこれを拒否します。これは空のデータベースにのみ適用されます。

## 設定

`.env.example` から始め、本物のシークレットは決してコミットしないでください。主な変数：

| 変数 | 用途 |
| --- | --- |
| `DB_DRIVER` | アプリの内部データベース：`sqlite`、`postgres`、`mysql`。 |
| `DATABASE_URL` | SQLite のファイルパス、または PostgreSQL/MySQL の接続文字列。 |
| `JWT_SECRET` | ログイントークンの署名に使用。本番では 32 文字以上（`openssl rand -hex 32`）。 |
| `NIAS_ENCRYPTION_KEY` | ちょうど 32 文字。保存する認証情報を暗号化します（`openssl rand -hex 16`）。変更すると保存済みの認証情報が読めなくなります。 |
| `DEFAULT_ADMIN_USERNAME` / `DEFAULT_ADMIN_PASSWORD` | 最初の管理者アカウント。ユーザーが存在しない場合のみ作成。 |
| `NIAS_ENV` | `development` または `production`（より厳しい検証）。 |
| `CORS_ORIGIN` | 許可するブラウザのオリジン。 |
| `REDIS_URL` | キャッシュとレート制限用の任意の Redis。未設定ならプロセス内メモリを使用。 |
| `AI_API_KEY`、`AI_BASE_URL`、`AI_MODEL` | 「Ask your data」用の任意の AI プロバイダー。 |

## 開発

```bash
make dev           # バックエンド + フロントエンド
make dev-server    # バックエンドのみ（:8080）
make dev-web       # フロントエンドのみ（:5173）
make test          # go test + vue-tsc の型チェック
make build         # web/dist + bin/nias
```

バックエンドのテスト：`cd server && go test ./...`。`_local_test.go` で終わるテストにはローカルのデータベースが必要です。

## リポジトリ構成

```text
.
├── server/          Go バックエンド（main.go、handlers/、middleware/、db/、config/、cache/）
├── web/             Vue 3 フロントエンド（views/、components/、composables/、i18n/）
├── deploy/compose/  Docker Compose の構成
├── docs/            ユーザー向け・機能ガイド
├── DESIGN.md        UI デザインの方針
└── Makefile
```

## プロジェクトの状況

Anveesa Nias は `v1.0.0` から安定版です。REST API のルート、権限定数、接続/認証情報の保存スキーマは [semver](https://semver.org/) に従い、互換性のない変更ではメジャーバージョンが上がります。リリース履歴は [CHANGELOG.md](CHANGELOG.md)、今後の予定は [Wiki のロードマップ](https://github.com/PandhuWibowo/anveesa-nias/wiki/JA-Roadmap) を参照してください。

## ドキュメント

- [Wiki](https://github.com/PandhuWibowo/anveesa-nias/wiki/JA-Home)
- [ドキュメント索引](docs/README.md)
- [機能ガイド](docs/FEATURES.md)
- [オープンソースプロジェクトガイド](docs/OPEN_SOURCE.md)

## コントリビュート

コントリビューションを歓迎します。[コントリビューションガイド](CONTRIBUTING.md)、[行動規範](CODE_OF_CONDUCT.md)、[Wiki のコントリビューターガイド](https://github.com/PandhuWibowo/anveesa-nias/wiki/JA-Contributor-Guide) をお読みください。[`good first issue`](https://github.com/PandhuWibowo/anveesa-nias/labels/good%20first%20issue) ラベルの issue から始めるのがおすすめです。

**セキュリティ：** 脆弱性は公開 issue で報告しないでください。[セキュリティポリシー](SECURITY.md) に従ってください。

## 寄付

Anveesa Nias が役に立ち、インドネシアにお住まいの方は、銀行振込で寄付いただけます：

| 銀行 | 口座名義 | 口座番号 |
| --- | --- | --- |
| BCA | Pandhu Wibowo | `6043081611` |
| BNI | Pandhu Wibowo | `1487723030` |

PayPal：<https://www.paypal.com/paypalme/pandhuwibowo>

## ライセンス

Anveesa Nias は [MIT ライセンス](LICENSE) のもとで公開されています。
