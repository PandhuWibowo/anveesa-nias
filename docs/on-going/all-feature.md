# All Features — Anveesa Nias

Dokumen ini merangkum seluruh fitur yang tersedia di Anveesa Nias, database studio open-source untuk tim.

---

## 1. Koneksi Database

- Multi-koneksi ke **PostgreSQL, MySQL, SQLite, dan SQL Server**
- Kredensial koneksi dienkripsi (AES) sebelum disimpan
- Support **SSH tunnel** untuk koneksi ke database di jaringan private
- Folder/grup untuk mengorganisir koneksi
- Test koneksi sebelum menyimpan

---

## 2. SQL Studio

- **SQL Editor** dengan syntax highlighting (CodeMirror + sql-formatter)
- **Schema browser** — jelajahi tabel, kolom, indeks
- **Query history** — riwayat query yang pernah dijalankan
- **Saved queries** — simpan dan kelola query favorit
- **Snippets** — template SQL yang bisa dipakai ulang
- **Multi-exec** — jalankan beberapa statement sekaligus
- **Import** data ke tabel
- **Edit data** langsung dari tabel (row-level)
- **Explain** query plan
- **Row history** — riwayat perubahan per baris

---

## 3. Analytics & Dashboard

- Dashboard dengan berbagai tipe visualisasi: tabel, KPI, bar, horizontal bar, line, area, scatter, pie, donut
- **Export** dashboard ke PDF, PNG, Excel, CSV, SQL, JSON
- **Public sharing** — bagikan dashboard via link publik
- **Embed** — sematkan dashboard atau chart ke website lain via iframe
- **AI Analytics** — analisis dan generate insight data dengan bantuan AI

---

## 4. Schema Management

- **ER Diagram** — visualisasi relasi antar tabel
- **Schema diff** — bandingkan skema antar dua koneksi atau versi
- **Schema editor** — edit struktur tabel lewat UI (tanpa menulis DDL manual)
- **Schema metadata** — dokumentasi tabel dan kolom

---

## 5. Change Management

- **Change sets** — kelompokkan perubahan DDL/DML untuk di-review sebelum dijalankan
- **Approval workflows** — alur persetujuan multi-step untuk query berbahaya
- **Data scripts** — skrip data yang bisa diajukan dan disetujui oleh approver
- **Query approval** — query dari user dengan permission terbatas harus diapprove terlebih dahulu

---

## 6. Monitoring & Observability

- **Audit log** — log semua aktivitas user (login, query, akses fitur)
- **Database audit history** — riwayat perubahan di level database
- **Query performance** — analisis performa query
- **Profiler** — profiling eksekusi query
- **Health check** — status kesehatan aplikasi dan dependensinya
- **Scheduler** — jadwalkan query berjalan otomatis (cron-based)
- **Backup** — backup database internal aplikasi

---

## 7. Integrasi Ekosistem

- **Redis** — browser dan operasi key-value Redis (terpisah dari cache internal)
- **Kafka** — browse topic, produksi dan konsumsi pesan
- **Laravel Queue** — monitor dan kelola job queue Laravel

---

## 8. User Management & Keamanan

- **Users** — manajemen akun pengguna
- **RBAC (Roles)** — role-based access control dengan permission granular
- **Permissions** — kontrol akses per fitur (app permission) dan per koneksi (DB permission: select, insert, update, delete, create, alter, drop)
- **2FA** — two-factor authentication via TOTP (QR code)
- **Session management** — lihat dan revoke sesi aktif
- **Login activity** — riwayat aktivitas login
- **Rate limiting** — proteksi brute force di endpoint login dan registrasi

---

## 9. Notifikasi

- Sistem notifikasi in-app untuk event penting: approval request, perubahan status, dll
- Manajemen notifikasi (baca, hapus, filter per tipe)

---

## 10. AI

- **AI-assisted SQL** — generate, jelaskan, atau optimasi query dengan bantuan AI
- Provider AI dapat dikonfigurasi sendiri via environment variable (`AI_API_KEY`, `AI_BASE_URL`, `AI_MODEL`)
- Per-user override untuk provider AI via UI Settings
