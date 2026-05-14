# Handoff

## Project

- Name: sub2api
- Path: `E:\Codex\projects\sub2api`
- Branch: `main`
- Upstream commit: `18790386a76f12ae5721e557dc652c346ca699d5`
- Port slot: `03`
- Server: `ubuntu@43.163.127.122`
- Server runtime path: `/opt/sub2api`
- Server bind: `127.0.0.1:9203`

## Quick Start

```powershell
Set-Location E:\Codex\projects\sub2api
git status --short --branch
Get-Content -Raw PROJECT_STATE.md
Get-Content -Raw OPERATIONS.md
```

## Environment Variables

Only variable names are documented here. Do not store real values in this file.

- `BIND_HOST`
- `SERVER_PORT`
- `SERVER_MODE`
- `RUN_MODE`
- `TZ`
- `POSTGRES_USER`
- `POSTGRES_PASSWORD`
- `POSTGRES_DB`
- `REDIS_PASSWORD`
- `ADMIN_EMAIL`
- `ADMIN_PASSWORD`
- `JWT_SECRET`
- `TOTP_ENCRYPTION_KEY`
- `UPDATE_PROXY_URL`

## Current Status

Initial production deployment is running. Docker and Docker Compose v2 are
installed on the server, and `sub2api`, `sub2api-postgres`, and `sub2api-redis`
are healthy under `/opt/sub2api`.

## Safe Next Step

For local browser access before DNS/reverse proxy, open an SSH tunnel:

```powershell
ssh -i $env:USERPROFILE\.ssh\aoxue_codex_ed25519 -L 9203:127.0.0.1:9203 ubuntu@43.163.127.122
```

Then browse:

```text
http://127.0.0.1:9203
```

For server health/status:

```bash
cd /opt/sub2api
sudo docker compose -f docker-compose.local.yml --env-file .env ps
curl -fsS http://127.0.0.1:9203/health
```

## Do Not Touch

- Do not reveal `.env` contents.
- Do not remove deployment data directories.
- Do not modify DNS, firewall, reverse proxy, or production database without
  explicit approval.
