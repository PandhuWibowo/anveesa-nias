# Contributing to Anveesa Nias

Thank you for considering a contribution. This project is intended to be a practical, maintainable open-source database studio, so focused changes with clear tests and documentation are preferred.

## Ways to Contribute

- Report reproducible bugs.
- Improve documentation and examples.
- Fix UI defects, accessibility issues, and export behavior.
- Add tests around backend handlers, permissions, and dashboard rendering.
- Propose features with a clear product use case.

## Development Setup

```bash
git clone https://github.com/PandhuWibowo/anveesa-nias.git
cd anveesa-nias

cd server
go mod download

cd ../web
npm install
```

Run the backend:

```bash
cd server
go run .
```

Run the frontend:

```bash
cd web
npm run dev
```

## Verification Before Opening a PR

Run the checks that match your change:

```bash
cd server && go test ./...
cd web && npm run build
```

For backend changes, prefer adding or updating Go tests. For frontend changes, verify the workflow in the browser and include screenshots for visible UI changes.

## Pull Request Guidelines

- Keep PRs focused on one problem.
- Explain the user-facing behavior change.
- Include screenshots or short videos for UI changes.
- Do not commit local secrets, databases, logs, or generated build output.
- Update documentation when behavior, configuration, or APIs change.

## Code Style

- Go code should be formatted with `gofmt`.
- Frontend code should follow the existing Vue/TypeScript style in `web/src`.
- Prefer existing components, composables, and utilities before adding new abstractions.
- Keep security-sensitive behavior explicit and documented.

## Commit Messages

Use concise, descriptive Conventional Commit messages. The container pipeline reads commits since the latest `vX.Y.Z` tag and creates the next version automatically:

- Major release: use `!` in the commit type, such as `feat!: remove legacy API`, or include `BREAKING CHANGE:` in the commit body.
- Minor release: use `feat:`.
- Patch release: use `fix:`, `perf:`, `refactor:`, `docs:`, `style:`, `test:`, `build:`, `ci:`, `chore:`, or `revert:`.

- `fix: correct dashboard export header`
- `feat: add chart embed route`
- `docs: update dashboard guide`
- `test: cover dashboard permissions`

## Security Issues

Do not open public issues for vulnerabilities. Follow [SECURITY.md](SECURITY.md).
