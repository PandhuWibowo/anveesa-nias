# Anveesa Nias

Project documentation lives under [docs/README.md](/Users/pandhuwibowo/Portfolio/anveesa/anveesa-nias/docs/README.md).

Docker Compose files live under [deploy/compose](/Users/pandhuwibowo/Portfolio/anveesa/anveesa-nias/deploy/compose).

Common repo-local commands:

```bash
docker-compose -f deploy/compose/docker-compose.yml up -d
docker-compose -f deploy/compose/docker-compose.postgres.yml up -d
docker-compose -f deploy/compose/docker-compose.prod.yml up -d
```

Docker Hub publishing:

```bash
make docker-build IMAGE_NAME=anveesa/nias IMAGE_TAG=v1.0.0
make docker-push IMAGE_NAME=anveesa/nias IMAGE_TAG=v1.0.0
make docker-push IMAGE_NAME=anveesa/nias IMAGE_TAG=v1.0.0 PUSH_LATEST=1
```

GitHub Actions publishing:

- Push `main` to publish `anveesa/nias:latest`
- Push a tag like `v1.2.3` to publish:
  - `anveesa/nias:v1.2.3`
  - `anveesa/nias:1.2`
  - `anveesa/nias:latest`
- Configure GitHub repository secrets:
  - `DOCKERHUB_USERNAME`
  - `DOCKERHUB_TOKEN`
