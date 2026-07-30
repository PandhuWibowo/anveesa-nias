# Changelog

This project follows a human-readable changelog format.

## 1.2.0 - 2026-07-30

- Added an SFTP archive preview — viewing a `.zip`/`.tar`/`.tar.gz`/`.tgz` file lists its contents instead of refusing to preview it as binary.
- Added an **Extract** action for SFTP archives ("extract here"), with the same sudo elevation as Compress for restricted destinations.

## 1.1.2 - 2026-07-30

- Fixed the "Browse Data" grid returning inconsistent rows when sorting or paging: queries now always include a deterministic tiebreaker (the table's primary key, or a per-row fallback when there isn't one) after the requested sort column, so identical values — and the no-sort-requested case — no longer shuffle between page fetches.

## 1.1.1 - 2026-07-30

- Added sudo elevation to SFTP file uploads for destinations the SSH user can't write to directly (uploads to a writable scratch path first, then moves the file into place with sudo).
- Fixed SFTP requests hanging indefinitely with no error when the SSH connection succeeded but the SFTP subsystem never responded; now times out with a clear error.

## 1.1.0 - 2026-07-30

- Added pause, resume, cancel, and dismiss controls to the SFTP upload progress dock.

## 1.0.3 - 2026-07-30

- Extended sudo elevation to SFTP New Folder, Delete, and Rename for paths the SSH user can't write to directly.

## 1.0.2 - 2026-07-29

- Added sudo elevation to SFTP Compress for destinations the SSH user can't write to directly (matches the existing Nginx config management convention), with automatic retry-with-sudo in the UI.

## 1.0.1 - 2026-07-29

- Fixed SFTP Compress leaving a broken partial archive behind on failure, which then blocked every retry with a false "already exists" conflict.

## 1.0.0 - 2026-07-28

First stable release.

- Prepared the repository for open-source collaboration.
- Added project governance files, issue templates, security policy, and CI.
- Added dashboard and chart embed support.
- Improved dashboard export support for PDF, PNG, Excel, CSV, SQL, and JSON.
- Added SFTP file search, a read-only file preview, and folder compression (zip/tar.gz).
- Standardized search/filter, sort, and pagination across the frontend onto shared composables (`useListFilter`, `useSort`, `usePagination`) and a shared `<Pagination>` component; fixed a schema explorer sort that updated its arrow but never reordered rows.
