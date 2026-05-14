# TODO

## Now

- [x] Clone upstream source.
- [x] Inspect deployment options.
- [x] Add Codex project state files.
- [x] Add Codex server deployment helper.
- [x] Confirm server SSH target and login method.
- [x] Confirm whether deployment should be public by domain/reverse proxy or
  direct by server IP and port.
- [x] Run Docker Compose validation on the server or another Docker-capable
  Linux host.
- [x] Install Docker and Docker Compose v2 on the server.
- [x] Deploy Sub2API to `/opt/sub2api`.
- [x] Verify server health endpoint on `127.0.0.1:9203`.
- [ ] Confirm domain DNS target and reverse proxy choice.
- [ ] Configure HTTPS reverse proxy after DNS is ready.

## Next

- [x] Create `/opt/sub2api` on the server.
- [x] Copy `deploy/docker-compose.local.yml` and a generated `.env` to the
  server.
- [x] Start containers with Docker Compose.
- [x] Verify `GET /health` on the server.
- [x] Capture admin setup without echoing
  secrets in chat.
- [ ] Use an SSH tunnel or reverse proxy to complete a browser login smoke test.

## Later

- [ ] Configure HTTPS reverse proxy if a domain is available.
- [ ] Document backup and upgrade procedure after first successful deployment.

## Verification Checklist

- [x] Source checkout exists.
- [x] Upstream deploy files exist.
- [x] Docker Compose YAML parsed locally.
- [x] Docker Compose config validated on a Docker-capable host.
- [x] Containers started.
- [x] Health endpoint returns success.
- [x] Web UI root returns HTML.
- [x] No secrets exposed.
