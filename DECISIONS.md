# Decisions

## Technical Decisions

- Use upstream `Wei-Shaw/sub2api` as the source repository.
- Keep the checkout on upstream `main` unless the user asks to pin a release.
- Use upstream Docker image `weishaw/sub2api:latest` for deployment by default.

## Deployment Decisions

- Prefer `deploy/docker-compose.local.yml` because it stores application,
  PostgreSQL, and Redis data in local directories that are easy to back up and
  move.
- Use Codex port slot `03`: local backend `9103`, production backend `9203`.
- Bind production to `127.0.0.1:9203` when a reverse proxy with HTTPS is used.
  Bind to `0.0.0.0:9203` only if the user wants direct IP access and the
  firewall is intentionally opened.
- Do not run the upstream one-line installer unless Docker is unavailable or
  the user prefers systemd binary deployment.

## Security Decisions

- Do not commit `.env` files, passwords, OAuth secrets, API keys, private keys,
  or generated admin credentials.
- Generate `POSTGRES_PASSWORD`, `JWT_SECRET`, and `TOTP_ENCRYPTION_KEY` with
  `openssl rand -hex 32` on the deployment host.
- If using Nginx in front of Codex CLI traffic, ensure `underscores_in_headers
  on;` is configured at the Nginx `http` level.

## Rejected Options

- Building from source on Windows is not the first deployment path because this
  host currently lacks Go and Docker, while upstream already publishes Docker
  images and deployment compose files.
