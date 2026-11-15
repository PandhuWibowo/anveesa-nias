# Anveesa Nias

[English](README.md) · [Bahasa Indonesia](README.id.md) · **简体中文** · [日本語](README.ja.md)

Anveesa Nias 是一个开源的内部开发者平台。它为工程团队提供一个 Web 应用，用来安全地浏览和变更数据库、构建仪表盘、运行基础设施工具，并审计谁做了什么，所有操作都在同一套权限模型和同一个审批流程之下。

它是一个 Go 二进制文件（标准库 `net/http`），同时提供 API 和 Vue 3 + Vite 前端。

📖 **[Wiki](https://github.com/PandhuWibowo/anveesa-nias/wiki/ZH-Home)**：架构、设计决策、已知痛点和路线图，提供 English、Bahasa Indonesia、简体中文和日本語版本。

## 功能概览

**数据库与数据存储**
- 支持 PostgreSQL、MySQL/MariaDB、SQL Server 和 SQLite 连接，可通过 SSH 隧道；凭据以 AES 加密存储。
- SQL Studio：带 schema 感知自动补全的编辑器、历史、代码片段、事务、导入、行编辑、EXPLAIN、ER 图、schema diff、行历史。
- PostgreSQL/MySQL 运维：活动会话、慢日志/错误日志/审计日志、数据库用户与授权（规划 → 预览 → 执行或申请审批）、复制、服务器参数。
- Redis、Memcache、MongoDB、Cassandra、Kafka、Laravel 队列、Elasticsearch/OpenSearch、阿里云 SLS 日志，以及对象存储 bucket。

**分析**
- 仪表盘支持表格、KPI、柱状图、折线图、面积图、散点图、饼图和环形图；可导出为 PDF、PNG、Excel、CSV、SQL 和 JSON；支持公开链接和 iframe 嵌入。
- 带文件夹的已保存查询，以及 AI 辅助的 "Ask your data"（支持任何兼容 OpenAI 的提供方）。

**变更管理与自动化**
- 针对写 SQL、change set、数据脚本和数据库授权的多步骤审批流程。
- 定时查询、数据管道、备份到 bucket，以及数据 watcher。

**基础设施**
- 通过 SSH 管理 Docker、Nginx、原生 crontab 编辑，以及 SFTP 文件浏览器（受限路径支持 sudo 提权）。
- 只读的 Kubernetes 浏览器，支持实时日志和 exec 终端。

**监控与治理**
- 健康状态、查询性能、日志检索、可用性检查、基础设施指标和告警。
- 用户、角色、访问组、按连接的 SQL 权限、强制 MFA，以及覆盖所有工具的审计日志。
- 界面语言：English 和简体中文。

## 快速开始

前置条件：Go 1.25+ 和 Node.js 20+。

```bash
git clone https://github.com/PandhuWibowo/anveesa-nias.git
cd anveesa-nias
cp .env.example .env
make secrets      # 输出 JWT_SECRET、NIAS_ENCRYPTION_KEY 和 DEFAULT_ADMIN_PASSWORD，粘贴到 .env
make install      # npm install + go mod tidy
make dev          # 后端 :8080，前端 :5173
```

打开 http://localhost:5173。默认的 `.env.example` 使用 SQLite（`DB_DRIVER=sqlite`、`DATABASE_URL=data.db`），试用时无需数据库服务器。

### 使用 Docker Compose（PostgreSQL）

```bash
cp .env.example .env   # 设置 JWT_SECRET、NIAS_ENCRYPTION_KEY、DEFAULT_ADMIN_PASSWORD
docker compose -f deploy/compose/docker-compose.yml --env-file .env up -d --build
```

打开 http://localhost:8080。其他变体（production、RDS、MySQL、Kafka、Cassandra）位于 [`deploy/compose/`](deploy/compose/)。

## 首次登录

首次启动时，如果 `users` 表为空，Anveesa Nias 会根据 `.env` 创建一个管理员账号：

1. 运行 `make secrets`，把输出的 `DEFAULT_ADMIN_PASSWORD` 填入 `.env`（如果不想使用 `admin`，也请设置 `DEFAULT_ADMIN_USERNAME`）。
2. 启动应用并使用这些凭据登录。
3. 登录后立即修改密码。

如果未设置 `DEFAULT_ADMIN_PASSWORD`，会回退到占位密码 `admin123`，而在 `NIAS_ENV=production` 时服务会拒绝使用它。此规则只适用于空数据库。

## 配置

从 `.env.example` 开始，切勿提交真实密钥。最重要的变量：

| 变量 | 用途 |
| --- | --- |
| `DB_DRIVER` | 应用内部数据库：`sqlite`、`postgres` 或 `mysql`。 |
| `DATABASE_URL` | SQLite 文件路径，或 PostgreSQL/MySQL 连接字符串。 |
| `JWT_SECRET` | 用于签发登录 token。生产环境至少 32 个字符（`openssl rand -hex 32`）。 |
| `NIAS_ENCRYPTION_KEY` | 恰好 32 个字符，用于加密已存储的凭据（`openssl rand -hex 16`）。修改后已存储的凭据将无法读取。 |
| `DEFAULT_ADMIN_USERNAME` / `DEFAULT_ADMIN_PASSWORD` | 首个管理员账号，仅在没有任何用户时创建。 |
| `NIAS_ENV` | `development` 或 `production`（校验更严格）。 |
| `CORS_ORIGIN` | 允许的浏览器来源。 |
| `REDIS_URL` | 可选的 Redis，用于缓存和限流；未设置时使用进程内存。 |
| `AI_API_KEY`、`AI_BASE_URL`、`AI_MODEL` | "Ask your data" 的可选 AI 提供方。 |

## 开发

```bash
make dev           # 后端 + 前端
make dev-server    # 仅后端（:8080）
make dev-web       # 仅前端（:5173）
make test          # go test + vue-tsc 类型检查
make build         # web/dist + bin/nias
```

后端测试：`cd server && go test ./...`。以 `_local_test.go` 结尾的测试需要本地数据库。

## 仓库结构

```text
.
├── server/          Go 后端（main.go、handlers/、middleware/、db/、config/、cache/）
├── web/             Vue 3 前端（views/、components/、composables/、i18n/）
├── deploy/compose/  Docker Compose 变体
├── docs/            用户与功能指南
├── DESIGN.md        UI 设计方向
└── Makefile
```

## 项目状态

Anveesa Nias 自 `v1.0.0` 起进入稳定版。REST API 路由、权限常量以及连接/凭据存储结构遵循 [semver](https://semver.org/)：不兼容的变更会提升主版本号。发布历史见 [CHANGELOG.md](CHANGELOG.md)，后续计划见 [Wiki 中的路线图](https://github.com/PandhuWibowo/anveesa-nias/wiki/ZH-Roadmap)。

## 文档

- [Wiki](https://github.com/PandhuWibowo/anveesa-nias/wiki/ZH-Home)
- [文档索引](docs/README.md)
- [功能指南](docs/FEATURES.md)
- [开源项目指南](docs/OPEN_SOURCE.md)

## 参与贡献

欢迎贡献。请阅读[贡献指南](CONTRIBUTING.md)、[行为准则](CODE_OF_CONDUCT.md)以及 [Wiki 中的贡献者指南](https://github.com/PandhuWibowo/anveesa-nias/wiki/ZH-Contributor-Guide)。带有 [`good first issue`](https://github.com/PandhuWibowo/anveesa-nias/labels/good%20first%20issue) 标签的 issue 是很好的起点。

**安全：** 请不要在公开 issue 中报告漏洞，请遵循[安全策略](SECURITY.md)。

## 捐助

如果 Anveesa Nias 对你有帮助，且你在印度尼西亚，可以通过银行转账捐助：

| 银行 | 账户名 | 账号 |
| --- | --- | --- |
| BCA | Pandhu Wibowo | `6043081611` |
| BNI | Pandhu Wibowo | `1487723030` |

PayPal：<https://www.paypal.com/paypalme/pandhuwibowo>

## 许可证

Anveesa Nias 基于 [MIT 许可证](LICENSE)发布。
