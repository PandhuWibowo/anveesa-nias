# Security Policy

Anveesa Nias handles database connections, credentials, SQL execution, authentication, and audit data. Please report security issues privately.

## Supported Versions

Until the first stable release, security fixes target the `main` branch. After versioned releases begin, this file should be updated with supported release lines.

## Reporting a Vulnerability

Please do not open a public GitHub issue for suspected vulnerabilities.

Use GitHub Security Advisories if available for this repository. If advisories are not enabled yet, contact the maintainers privately and include:

- Affected version or commit.
- Reproduction steps.
- Impact assessment.
- Any logs, screenshots, or proof-of-concept details that help verify the issue.

## Security Expectations

- Do not commit `.env`, database files, private keys, certificates, backups, or logs.
- Rotate `JWT_SECRET`, `NIAS_ENCRYPTION_KEY`, and default admin passwords before using real data.
- Restrict `CORS_ORIGIN` to trusted domains.
- Review public dashboard and embed settings before enabling them.

## Disclosure

Maintainers will acknowledge valid reports, investigate impact, prepare a fix, and coordinate disclosure timing with the reporter when possible.
