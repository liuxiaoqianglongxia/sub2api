# Maijian Production Notes

Last updated: 2026-05-15

## Current Production

- Domain: `token.maijian.net`
- Server: `ubuntu@43.163.127.122`
- Runtime directory: `/opt/sub2api`
- Compose file: `/opt/sub2api/docker-compose.local.yml`
- Active app image: `sub2api:maijian-qwen-quality-20260515-v1`
- Previous app image: `sub2api:maijian-image-ui-20260515-v2`

## Source Line

This branch tracks the Maijian production source line on top of upstream
`Wei-Shaw/sub2api`.

Important local commits:

- `360d519` adds the Qwen raw-chat default and user image-generation UI.
- `7ba7590` restores Qwen quality by no longer forcing DashScope
  `enable_thinking=false` when clients do not explicitly set it.

## Production Behavior

- Qwen uses the OpenAI-compatible Chat Completions path.
- Qwen requests no longer get an implicit `enable_thinking=false`.
- Clients can still explicitly send `enable_thinking=true` or
  `enable_thinking=false`; the gateway passes explicit values through.
- The user image-generation page is available at `/image-generation`.
- Image generation is configured in the database/admin panel, not hard-coded in
  this file.

## Build Notes

- Docker frontend builds pin `pnpm@10.23.0` to avoid breakage from upstream
  pnpm default-policy changes.
- `.dockerignore` excludes local pnpm stores and backend binaries so Docker
  build contexts stay small.

## Rollback

To roll back only the application image, change the `sub2api` service image in
`/opt/sub2api/docker-compose.local.yml` back to:

```text
sub2api:maijian-image-ui-20260515-v2
```

Then restart only the app service:

```bash
cd /opt/sub2api
sudo docker compose -f docker-compose.local.yml --env-file .env up -d sub2api
```
