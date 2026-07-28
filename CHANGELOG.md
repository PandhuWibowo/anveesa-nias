# Changelog

This project follows a human-readable changelog format.

## 1.0.0 - 2026-07-28

First stable release.

- Prepared the repository for open-source collaboration.
- Added project governance files, issue templates, security policy, and CI.
- Added dashboard and chart embed support.
- Improved dashboard export support for PDF, PNG, Excel, CSV, SQL, and JSON.
- Added SFTP file search, a read-only file preview, and folder compression (zip/tar.gz).
- Standardized search/filter, sort, and pagination across the frontend onto shared composables (`useListFilter`, `useSort`, `usePagination`) and a shared `<Pagination>` component; fixed a schema explorer sort that updated its arrow but never reordered rows.
