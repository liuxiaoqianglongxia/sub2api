# Project State

Last updated: 2026-05-13

## Summary

Sub2API is an AI API gateway platform for subscription quota distribution. This
workspace tracks a local checkout of `Wei-Shaw/sub2api` for deployment to the
user's Linux server.

## Current Phase

- Phase: initial server deployment
- Status: running on server
- Upstream checkout: `main` at `18790386a76f12ae5721e557dc652c346ca699d5`
- Latest remote tag observed: `v0.1.126`

## What Works

- Source cloned into `E:\Codex\projects\sub2api`.
- Upstream Docker Compose deployment files are present under `deploy/`.
- Recommended deployment path identified: `deploy/docker-compose.local.yml`.
- Codex port slot assigned: `03`.
- Server SSH works with `ubuntu@43.163.127.122`.
- Docker and Docker Compose v2 are installed and running on the server.
- Sub2API is deployed under `/opt/sub2api` on the server.
- Runtime containers are healthy: `sub2api`, `sub2api-postgres`, and
  `sub2api-redis`.
- Backend is bound to `127.0.0.1:9203` on the server.
- `GET http://127.0.0.1:9203/health` returns `{"status":"ok"}` on the server.

## What Is Blocked

- Public browser access is not configured yet. The app is intentionally bound to
  loopback until domain DNS and reverse proxy settings are confirmed.
- Windows host still does not have `docker` or `go` in PATH, so local source or
  container startup was not run on Windows.
- Server login smoke test through the web UI is still pending.

## Important Paths

- Project root: `E:\Codex\projects\sub2api`
- Backend entry: `backend/cmd/server`
- Frontend app: `frontend`
- Docker deploy files: `deploy`
- Production env template: `deploy/codex-prod.env.example`
- Server deployment helper: `deploy/codex-server-deploy.sh`
- Runtime data on server: `/opt/sub2api`
- Server compose file: `/opt/sub2api/docker-compose.local.yml`
- Server env file: `/opt/sub2api/.env`

## Last Verification

- Date: 2026-05-13
- Commands:
  - `ssh -i $env:USERPROFILE\.ssh\aoxue_codex_ed25519 ubuntu@43.163.127.122 "uname -a; id"`
  - `sudo apt-get install -y docker.io docker-compose-v2`
  - `sudo docker --version`
  - `sudo docker compose version`
  - `sudo docker compose -f docker-compose.local.yml --env-file .env config --quiet`
  - `sudo docker compose -f docker-compose.local.yml --env-file .env up -d`
  - `sudo docker compose -f docker-compose.local.yml --env-file .env ps`
  - `curl -fsS -i http://127.0.0.1:9203/health`
  - `curl -fsS -I http://127.0.0.1:9203/`
- Results:
  - Server is Ubuntu 24.04.4 LTS on x86_64.
  - Docker `29.1.3` and Docker Compose `2.40.3` are installed.
  - Compose config validates.
  - Containers are up and healthy.
  - Server listens on `127.0.0.1:9203`.
  - Health endpoint returns HTTP 200 with `{"status":"ok"}`.
  - Root path returns HTTP 200 HTML.

## Open Risks

- Domain, TLS, firewall, and reverse proxy are not configured yet.
- Admin password and generated secrets live only in the server `.env`; do not
  store them in this repository or chat.
- URL allowlist is disabled by the upstream default env path; revisit after
  confirming upstream/provider URLs.
