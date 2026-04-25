# Anveesa Nias

Project documentation lives under [docs/README.md](/Users/pandhuwibowo/Portfolio/anveesa/anveesa-nias/docs/README.md).

Docker Compose files live under [deploy/compose](/Users/pandhuwibowo/Portfolio/anveesa/anveesa-nias/deploy/compose).

Common repo-local commands:

```bash
docker-compose -f deploy/compose/docker-compose.yml up -d
docker-compose -f deploy/compose/docker-compose.postgres.yml up -d
docker-compose -f deploy/compose/docker-compose.prod.yml up -d
```

Optional Redis acceleration:

```bash
docker-compose -f deploy/compose/docker-compose.yml --profile cache up -d
REDIS_URL=redis://redis:6379/0 docker-compose -f deploy/compose/docker-compose.postgres.yml --profile cache up -d
```

If `REDIS_URL` is not set, the app automatically falls back to in-process server memory for rate limiting and cache storage.

Supported Redis env vars:

- `REDIS_URL`
- `REDIS_PASSWORD`
- `REDIS_DB`
- `REDIS_PREFIX`

Docker Hub publishing:

```bash
make docker-build IMAGE_NAME=pandhu612/anveesa-nias IMAGE_TAG=v1.0.0
make docker-push IMAGE_NAME=pandhu612/anveesa-nias IMAGE_TAG=v1.0.0
make docker-push IMAGE_NAME=pandhu612/anveesa-nias IMAGE_TAG=v1.0.0 PUSH_LATEST=1
```

GitHub Actions publishing:

- Push `main` to publish `pandhu612/anveesa-nias:latest`
- Push a tag like `v1.2.3` to publish:
  - `pandhu612/anveesa-nias:v1.2.3`
  - `pandhu612/anveesa-nias:1.2`
  - `pandhu612/anveesa-nias:latest`
- Configure GitHub repository secrets:
  - `DOCKERHUB_USERNAME`
  - `DOCKERHUB_TOKEN`
