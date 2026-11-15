# Anveesa Nias

[English](README.md) · **Bahasa Indonesia** · [简体中文](README.zh-CN.md) · [日本語](README.ja.md)

Anveesa Nias adalah *internal developer platform* open-source. Satu aplikasi web untuk tim engineering yang perlu menjelajahi dan mengubah database dengan aman, membangun dashboard, menjalankan alat infrastruktur, dan mengaudit siapa melakukan apa, semuanya di bawah satu model permission dan satu alur approval.

Nias adalah satu binary Go (`net/http` stdlib) yang menyajikan API sekaligus frontend Vue 3 + Vite.

📖 **[Wiki](https://github.com/PandhuWibowo/anveesa-nias/wiki/ID-Beranda)**: arsitektur, keputusan desain, pain point, dan roadmap, dalam English, Bahasa Indonesia, 简体中文, dan 日本語.

## Isinya

**Database dan data store**
- Koneksi PostgreSQL, MySQL/MariaDB, SQL Server, dan SQLite, bisa lewat SSH tunnel; kredensial disimpan terenkripsi AES.
- SQL Studio: editor dengan autocomplete berbasis skema, history, snippet, transaksi, import, edit baris, EXPLAIN, ER diagram, schema diff, row history.
- Operasional PostgreSQL/MySQL: sesi aktif, slow/error/audit log, DB users & grants (rencana → preview → apply atau ajukan approval), replikasi, parameter server.
- Redis, Memcache, MongoDB, Cassandra, Kafka, Laravel queue, Elasticsearch/OpenSearch, log Alibaba Cloud SLS, dan bucket object storage.

**Analitik**
- Dashboard dengan tabel, KPI, bar, line, area, scatter, pie, dan donut; ekspor ke PDF, PNG, Excel, CSV, SQL, dan JSON; link publik dan embed iframe.
- Saved query dengan folder, dan "Ask your data" berbantuan AI (provider apa pun yang kompatibel dengan OpenAI).

**Manajemen perubahan dan otomasi**
- Approval workflow bertahap untuk SQL tulis, change set, data script, dan grant DB.
- Query terjadwal, data pipeline, backup ke bucket, dan watcher data.

**Infrastruktur**
- Docker, Nginx, edit crontab langsung di server, dan file browser SFTP lewat SSH (dengan sudo elevation untuk path terbatas).
- Penjelajah Kubernetes read-only dengan log live dan terminal exec.

**Monitoring dan tata kelola**
- Health, performa query, pencarian log, uptime check, metrik infra, dan alert.
- User, role, access group, permission SQL per koneksi, MFA wajib, dan audit log yang mencakup semua alat.
- UI dalam English dan 简体中文.

## Mulai cepat

Prasyarat: Go 1.25+ dan Node.js 20+.

```bash
git clone https://github.com/PandhuWibowo/anveesa-nias.git
cd anveesa-nias
cp .env.example .env
make secrets      # mencetak JWT_SECRET, NIAS_ENCRYPTION_KEY, dan DEFAULT_ADMIN_PASSWORD; tempel ke .env
make install      # npm install + go mod tidy
make dev          # backend di :8080, frontend di :5173
```

Buka http://localhost:5173. `.env.example` bawaan memakai SQLite (`DB_DRIVER=sqlite`, `DATABASE_URL=data.db`), jadi tidak perlu server database untuk mencoba.

### Dengan Docker Compose (PostgreSQL)

```bash
cp .env.example .env   # isi JWT_SECRET, NIAS_ENCRYPTION_KEY, DEFAULT_ADMIN_PASSWORD
docker compose -f deploy/compose/docker-compose.yml --env-file .env up -d --build
```

Buka http://localhost:8080. Varian lain (production, RDS, MySQL, Kafka, Cassandra) ada di [`deploy/compose/`](deploy/compose/).

## Login pertama

Saat pertama kali jalan dan tabel `users` masih kosong, Anveesa Nias membuat satu akun admin dari `.env`:

1. Jalankan `make secrets` dan masukkan `DEFAULT_ADMIN_PASSWORD` yang tercetak ke `.env` (isi juga `DEFAULT_ADMIN_USERNAME` jika tidak ingin `admin`).
2. Jalankan aplikasi dan login dengan kredensial tersebut.
3. Segera ganti password setelah login.

Jika `DEFAULT_ADMIN_PASSWORD` tidak diisi, nilainya jatuh ke placeholder `admin123`, yang ditolak server saat `NIAS_ENV=production`. Ini hanya berlaku untuk database yang masih kosong.

## Konfigurasi

Mulai dari `.env.example` dan jangan pernah commit secret asli. Variabel terpenting:

| Variabel | Fungsi |
| --- | --- |
| `DB_DRIVER` | Database internal aplikasi: `sqlite`, `postgres`, atau `mysql`. |
| `DATABASE_URL` | Path file SQLite, atau connection string PostgreSQL/MySQL. |
| `JWT_SECRET` | Menandatangani token login. Minimal 32 karakter di production (`openssl rand -hex 32`). |
| `NIAS_ENCRYPTION_KEY` | Tepat 32 karakter; mengenkripsi kredensial tersimpan (`openssl rand -hex 16`). Mengubahnya membuat kredensial tersimpan tidak bisa dibaca. |
| `DEFAULT_ADMIN_USERNAME` / `DEFAULT_ADMIN_PASSWORD` | Akun admin pertama, dibuat hanya jika belum ada user. |
| `NIAS_ENV` | `development` atau `production` (validasi lebih ketat). |
| `CORS_ORIGIN` | Origin browser yang diizinkan. |
| `REDIS_URL` | Redis opsional untuk cache dan rate limiting; tanpa ini memakai memori proses. |
| `AI_API_KEY`, `AI_BASE_URL`, `AI_MODEL` | Provider AI opsional untuk "Ask your data". |

## Pengembangan

```bash
make dev           # backend + frontend
make dev-server    # backend saja (:8080)
make dev-web       # frontend saja (:5173)
make test          # go test + type-check vue-tsc
make build         # web/dist + bin/nias
```

Tes backend: `cd server && go test ./...`. Tes yang berakhiran `_local_test.go` butuh database lokal.

## Struktur repositori

```text
.
├── server/          Backend Go (main.go, handlers/, middleware/, db/, config/, cache/)
├── web/             Frontend Vue 3 (views/, components/, composables/, i18n/)
├── deploy/compose/  Varian Docker Compose
├── docs/            Panduan pengguna dan fitur
├── DESIGN.md        Arah desain UI
└── Makefile
```

## Status proyek

Anveesa Nias stabil sejak `v1.0.0`. Route REST API, konstanta permission, dan skema penyimpanan koneksi/kredensial mengikuti [semver](https://semver.org/): perubahan yang tidak kompatibel menaikkan versi mayor. Lihat [CHANGELOG.md](CHANGELOG.md) untuk riwayat rilis dan [roadmap di wiki](https://github.com/PandhuWibowo/anveesa-nias/wiki/ID-Roadmap) untuk rencana berikutnya.

## Dokumentasi

- [Wiki](https://github.com/PandhuWibowo/anveesa-nias/wiki/ID-Beranda)
- [Indeks dokumentasi](docs/README.md)
- [Panduan fitur](docs/FEATURES.md)
- [Panduan proyek open-source](docs/OPEN_SOURCE.md)

## Berkontribusi

Kontribusi sangat diterima. Silakan baca [panduan kontribusi](CONTRIBUTING.md), [kode etik](CODE_OF_CONDUCT.md), dan [panduan kontributor di wiki](https://github.com/PandhuWibowo/anveesa-nias/wiki/ID-Panduan-Kontributor). Issue berlabel [`good first issue`](https://github.com/PandhuWibowo/anveesa-nias/labels/good%20first%20issue) adalah titik awal yang baik.

**Keamanan:** jangan laporkan celah keamanan lewat issue publik. Ikuti [kebijakan keamanan](SECURITY.md).

## Donasi

Jika Anveesa Nias bermanfaat dan kamu berada di Indonesia, donasi bisa dikirim lewat transfer bank:

| Bank | Nama Rekening | Nomor Rekening |
| --- | --- | --- |
| BCA | Pandhu Wibowo | `6043081611` |
| BNI | Pandhu Wibowo | `1487723030` |

PayPal: <https://www.paypal.com/paypalme/pandhuwibowo>

## Lisensi

Anveesa Nias dirilis di bawah [Lisensi MIT](LICENSE).
