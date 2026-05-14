# Operations

## Local Inspection

```powershell
Set-Location E:\Codex\projects\sub2api
git status --short --branch
git log -1 --oneline
Get-ChildItem deploy
```

## Local Development

Windows currently has Node, npm, and pnpm, but no Go or Docker in PATH.
`Ubuntu-codex` currently has git, node, and npm, but no Go or Docker in PATH.
Development from source needs:

- Go matching `backend/go.mod`
- Node and pnpm for `frontend`
- PostgreSQL and Redis

Upstream development commands:

```powershell
pnpm --dir frontend install
pnpm --dir frontend run build
```

```bash
cd backend
go run ./cmd/server
```

## Docker Deployment Plan

Current server deployment:

- Host: `ubuntu@43.163.127.122`
- Directory: `/opt/sub2api`
- Compose: `/opt/sub2api/docker-compose.local.yml`
- Env: `/opt/sub2api/.env`
- Bind: `127.0.0.1:9203`
- Containers: `sub2api`, `sub2api-postgres`, `sub2api-redis`

Status check:

```bash
cd /opt/sub2api
sudo docker compose -f docker-compose.local.yml --env-file .env ps
curl -fsS http://127.0.0.1:9203/health
```

Local browser tunnel before domain/HTTPS:

```powershell
ssh -i $env:USERPROFILE\.ssh\aoxue_codex_ed25519 -L 9203:127.0.0.1:9203 ubuntu@43.163.127.122
```

Then open `http://127.0.0.1:9203`.

Recommended server directory:

```bash
sudo mkdir -p /opt/sub2api
sudo chown "$USER":"$USER" /opt/sub2api
cd /opt/sub2api
```

Copy or download the upstream compose file:

```bash
curl -fsSL https://raw.githubusercontent.com/Wei-Shaw/sub2api/main/deploy/docker-compose.local.yml -o docker-compose.local.yml
```

Codex helper option:

```bash
bash deploy/codex-server-deploy.sh
```

Create `.env` from `deploy/codex-prod.env.example`, then generate secrets on
the server:

```bash
openssl rand -hex 32
```

Start services:

```bash
docker compose -f docker-compose.local.yml up -d
docker compose -f docker-compose.local.yml ps
docker compose -f docker-compose.local.yml logs --tail=100 sub2api
```

Health check:

```bash
curl -fsS http://127.0.0.1:9203/health
```

## Production Access

Preferred:

- Bind Sub2API to `127.0.0.1:9203`.
- Put Nginx or Caddy in front with HTTPS.
- For Nginx and Codex CLI traffic, add `underscores_in_headers on;` to the
  Nginx `http` block.

Direct IP option:

- Set `BIND_HOST=0.0.0.0`.
- Open the chosen firewall port intentionally.
- Use HTTPS at a reverse proxy as soon as practical.

## Backup

When using `docker-compose.local.yml`, runtime data lives under:

- `/opt/sub2api/data`
- `/opt/sub2api/postgres_data`
- `/opt/sub2api/redis_data`

Stop before a simple full-directory backup:

```bash
docker compose -f docker-compose.local.yml down
tar czf sub2api-backup-$(date +%Y%m%d-%H%M%S).tar.gz /opt/sub2api
docker compose -f docker-compose.local.yml up -d
```

## Upgrade

```bash
cd /opt/sub2api
docker compose -f docker-compose.local.yml pull
docker compose -f docker-compose.local.yml up -d
docker compose -f docker-compose.local.yml logs --tail=100 sub2api
```
