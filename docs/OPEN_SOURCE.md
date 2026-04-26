# Open-Source Project Guide

This repository is set up for public collaboration.

GitHub repository:

https://github.com/PandhuWibowo/anveesa-nias

## Community Files

- `LICENSE`: MIT license.
- `README.md`: public entry point and quick start.
- `CONTRIBUTING.md`: development and PR expectations.
- `CODE_OF_CONDUCT.md`: expected behavior for contributors.
- `SECURITY.md`: private vulnerability reporting process.
- `SUPPORT.md`: support boundaries and channels.
- `CHANGELOG.md`: release notes and notable changes.
- `.github/ISSUE_TEMPLATE/*`: structured issue forms.
- `.github/pull_request_template.md`: PR checklist.
- `.github/workflows/ci.yml`: baseline CI for backend and frontend.

## Maintainer Checklist Before Public Release

- Confirm the license choice is correct for the project.
- Confirm repository URLs point to `https://github.com/PandhuWibowo/anveesa-nias`.
- Enable GitHub Discussions if you want user Q&A outside issues.
- Enable GitHub Security Advisories.
- Add repository topics such as `database`, `sql`, `vue`, `go`, `dashboard`, and `open-source`.
- Protect the `main` branch and require CI for pull requests.
- Rotate any secrets that may have existed before the repository became public.
- Confirm `.env`, database files, backups, and logs are not tracked.

## Contribution Policy

The project should optimize for small, reviewable changes. Large features should start as an issue or discussion with:

- Problem statement.
- User workflow.
- Security and permission impact.
- Backward compatibility notes.
- Test and documentation plan.

## Security Model Notes

Public dashboard links and embed routes intentionally expose rendered dashboard data to anyone with the token. Treat share tokens as secrets, rotate them if exposed, and do not enable public visibility for dashboards containing sensitive data.
