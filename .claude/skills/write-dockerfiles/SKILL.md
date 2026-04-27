---
name: write-dockerfiles
description: >
  Write, review, and maintain Dockerfiles and compose files for Agora backend services. Use
  this skill whenever creating or editing any file in builds/ — Dockerfiles, podman-compose
  YAML files, database init scripts, and entrypoint shell scripts. Covers Go service images,
  job images, the database image, standalone (all-in-one) dev images, and the compose files
  that wire them together.
---

# Dockerfile and Compose Writing Skill

This skill governs how to write and maintain container build files for Agora backend services.
All build artifacts live under `builds/`. Read the relevant section for the task at hand; the
conventions at the end apply to every Dockerfile.

**Before writing or editing any Dockerfile**, read the existing files in `builds/` to understand
the current patterns. Every Dockerfile in this project follows the same structure — copy it,
don't invent a new one.

---

## After Every Edit

Rebuild the affected image after changing any Dockerfile to catch syntax errors and verify the
build still succeeds:

```
podman build --format docker -f ./builds/<name>.Dockerfile -t <name>:local .
```

Run `make build` to rebuild all images at once. Avoid assuming a build is correct without
actually running it — Docker layer caching means a previously-failing step may appear to
succeed on a stale cache.

---

## Architecture: Service Images vs Job Images

This project separates the main process from maintenance work:

- **Main process images** (`grpc.Dockerfile`, `rest.Dockerfile`): long-running servers. They
  expect the database to be fully migrated before they start. They contain only the server
  binary and its healthcheck tool.

- **Job images** (`migrations.Dockerfile`, `rotate-keys.Dockerfile`): short-lived, run-to-
  completion containers. They are run as Kubernetes Jobs or equivalent before the main process
  starts. They carry only the binary needed for their single task.

- **Standalone images** (`standalone.grpc.Dockerfile`, `standalone.rest.Dockerfile`): dev and
  integration-test convenience images. They bundle the server, migrations, and rotate-keys into
  a single container, running migrations and rotation before starting the server. Never use
  standalone images in production.

- **Database image** (`database.Dockerfile`): a PostgreSQL image with pg_cron compiled in.
  Migrations are not baked in — run the migrations job image against it separately.

This separation keeps production images minimal: the server binary doesn't carry migration code
it never runs, and job images don't carry server code.

---

## Go Service and Job Images

All Go images follow a two-stage multi-stage build pattern.

### Builder stage

```dockerfile
FROM docker.io/library/golang:1.26.2-alpine AS builder

# Produce a fully static binary with no C library dependency, required for safe execution
# on Alpine (musl libc) and to keep the final image free of dynamic linker dependencies.
ENV CGO_ENABLED=0

WORKDIR /app

# ── Layer caching: download dependencies before copying source ──────────────────────────
# go mod download is placed before any COPY of source files so the module cache layer
# survives rebuilds where only source code changes. Only go.mod or go.sum changes
# invalidate this layer.
COPY go.mod go.sum ./
RUN go mod download

# ── Optional: install tools used in the final image ────────────────────────────────────
# If the image needs a tool like grpcurl for its healthcheck, install it here — after
# go mod download but before copying source files — so tool installation is also cached
# independently of source changes.
RUN GOBIN=/usr/local/bin go install github.com/fullstorydev/grpcurl/cmd/grpcurl@v1.9.3

# ── Copy source files ──────────────────────────────────────────────────────────────────
# Copy only the packages this binary needs; do not copy the entire repo. This keeps the
# build context small and avoids invalidating unrelated caches.
COPY ./cmd/grpc ./cmd/grpc
COPY ./internal/handlers ./internal/handlers
# ... other internal packages the binary imports

# ── Build ──────────────────────────────────────────────────────────────────────────────
# -ldflags="-s -w" strips symbol table and DWARF debug info, reducing binary size ~30%.
# -trimpath removes local filesystem paths from the binary for reproducible builds.
# Use a package path (./cmd/grpc/) not a file path (cmd/grpc/main.go).
RUN go build -ldflags="-s -w" -trimpath -o /grpc ./cmd/grpc/
```

**Layer ordering rule**: the order of instructions is strictly:

1. `COPY go.mod go.sum ./`
2. `RUN go mod download`
3. `RUN go install <tool>` (if any — only tools shipped in the final image)
4. `COPY` source files
5. `RUN go build`

Never move `go mod download` after source COPY — it defeats layer caching.

**Build flags**: all three flags (`CGO_ENABLED=0`, `-ldflags="-s -w"`, `-trimpath`) are
required on every `go build` invocation. Set `CGO_ENABLED=0` once as `ENV` at the top of the
stage rather than inlining it per build command.

**Multiple binaries**: when one Dockerfile produces multiple binaries (standalone images),
chain them in a single `RUN` to reduce layers:

```dockerfile
RUN go build -ldflags="-s -w" -trimpath -o /grpc ./cmd/grpc/ && \
    go build -ldflags="-s -w" -trimpath -o /migrations ./cmd/migrations/ && \
    go build -ldflags="-s -w" -trimpath -o /rotate-keys ./cmd/rotate-keys/
```

### Runtime stage

```dockerfile
FROM docker.io/library/alpine:3.23.4

COPY --from=builder /grpc /grpc
COPY --from=builder /usr/local/bin/grpcurl /usr/local/bin/grpcurl

HEALTHCHECK ...

ENV GRPC_PORT=8080
EXPOSE 8080
EXPOSE 443

CMD ["/grpc"]
```

The runtime stage contains only the binary (or binaries) and their runtime requirements.
No shell tools, no package managers, no build artifacts. Alpine is the runtime base for all
Go images — it provides a shell (required for standalone images' `sh -c` CMD), BusyBox
utilities, and a small footprint.

---

## Healthchecks

Every image that serves traffic must have a `HEALTHCHECK`.

### gRPC healthcheck

Use `grpcurl` against the standard `grpc.health.v1.Health/Check` endpoint:

```dockerfile
HEALTHCHECK --interval=1s --timeout=5s --retries=10 --start-period=1s \
  CMD grpcurl --plaintext -d '' localhost:8080 grpc.health.v1.Health/Check || exit 1
```

Install `grpcurl` in the builder and copy it to the runtime stage. Pin the version — never
use `@latest`:

```dockerfile
# Builder stage:
RUN GOBIN=/usr/local/bin go install github.com/fullstorydev/grpcurl/cmd/grpcurl@v1.9.3

# Runtime stage:
COPY --from=builder /usr/local/bin/grpcurl /usr/local/bin/grpcurl
```

### REST healthcheck

Alpine's BusyBox includes `wget` — no extra `apk add` required:

```dockerfile
HEALTHCHECK --interval=1s --timeout=5s --retries=10 --start-period=1s \
  CMD wget -qO /dev/null http://localhost:8080/ping || exit 1
```

Never add `curl` to the runtime image just for a healthcheck. BusyBox `wget` is already
present in any Alpine-based image and serves the same purpose at zero additional size cost.

### Database healthcheck

Use the `pg_isready` utility, already present in the PostgreSQL base image:

```dockerfile
HEALTHCHECK --interval=1s --timeout=5s --retries=10 --start-period=1s \
  CMD pg_isready || exit 1
```

### Job images

Job images (`migrations`, `rotate-keys`) do not serve traffic and do not need a `HEALTHCHECK`.
They run to completion and exit.

---

## Database Image

The database image is a PostgreSQL image with the `pg_cron` extension compiled in. It uses a
multi-stage build so that build tools (git, make, gcc) never appear in the final image layers.

```dockerfile
FROM docker.io/library/postgres:18.3 AS builder

ARG DEBIAN_FRONTEND=noninteractive

# Install build tools, compile pg_cron from source. The final stage copies only the
# compiled extension files, leaving all build tooling in this discarded stage.
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    build-essential \
    postgresql-server-dev-18 \
  && git clone https://github.com/citusdata/pg_cron.git \
  && cd pg_cron \
  && git fetch --tags \
  && git checkout "$(git describe --tags "$(git rev-list --tags --max-count=1)")" \
  && make \
  && make install

FROM docker.io/library/postgres:18.3

# Copy only the compiled extension artifacts; build tools stay in the builder stage.
# Update these paths when bumping the PostgreSQL major version.
COPY --from=builder /usr/lib/postgresql/18/lib/pg_cron.so /usr/lib/postgresql/18/lib/
COPY --from=builder /usr/share/postgresql/18/extension/pg_cron.control /usr/share/postgresql/18/extension/
COPY --from=builder /usr/share/postgresql/18/extension/pg_cron--*.sql /usr/share/postgresql/18/extension/
```

**Why multi-stage for the database image?** Without it, each `RUN` creates a layer. Even if
you run `apt-get remove` and `rm -rf` in a later `RUN`, the previous layers still contain the
build tools and take space in the image. A multi-stage build discards the builder entirely —
the final image contains only the postgres base plus the few kilobytes of pg_cron files.

**pg_cron version**: pg_cron is built from the latest tagged release (via `git describe
--tags`). This is intentional — unlike application code, pg_cron's release cadence is slow
and the "latest tag" strategy is acceptable. If a specific version is ever required, replace
the `git checkout` with `git checkout v1.6.4` (or whichever version).

**Path versioning**: the pg_cron extension files live at paths that include the PostgreSQL
major version (e.g., `/usr/lib/postgresql/18/lib/`). When bumping the `postgres:18.x` base
to `postgres:19.x`, update these paths accordingly.

**Executable files**: use `COPY --chmod=755` when copying shell scripts or other executables
into the image. This sets the executable bit in a single instruction and avoids a separate
`RUN chmod +x` layer:

```dockerfile
COPY --chmod=755 ./builds/database.entrypoint.sh /usr/local/bin/database.entrypoint.sh
```

**`DEBIAN_FRONTEND=noninteractive`**: only needed in the builder stage (which is Debian-based).
Never set it in Alpine-based runtime stages — it has no effect and only creates confusion.

---

## Compose Files

Compose files live in `builds/` alongside the Dockerfiles they reference. One compose file per
deployment scenario:

| File                                        | Purpose                 |
| ------------------------------------------- | ----------------------- |
| `podman-compose.yaml`                       | Local development stack |
| `podman-compose.test.yaml`                  | Unit test database      |
| `podman-compose.integration-test.grpc.yaml` | gRPC integration tests  |
| `podman-compose.integration-test.rest.yaml` | REST integration tests  |

### General rules

- Always set `context: ..` (the repo root) so Dockerfiles can reference files anywhere in the
  repo with paths relative to the root.
- Use named networks with descriptive, scenario-scoped names (e.g.,
  `json-keys-integration-grpc-test`) to prevent cross-compose network leakage when multiple
  scenarios run concurrently.
- All ports and credentials come from environment variables (sourced from `setup-env.sh`).
  Never hardcode port numbers or passwords in compose files.
- Use `depends_on` with `condition: service_healthy` so the dependent service waits until the
  dependency's `HEALTHCHECK` passes — not just until the container starts. Without this, a
  standalone image that runs migrations at startup may connect to postgres before it is accepting
  connections and fail:

  ```yaml
  service-json-keys:
    depends_on:
      postgres-json-keys:
        condition: service_healthy
  ```

  This requires the dependency to have a `HEALTHCHECK` defined in its Dockerfile (`pg_isready`
  for the database image). The chain is: postgres container starts → `pg_isready` passes →
  service container starts → migrations run against a ready database.

### Development compose (`podman-compose.yaml`)

Uses standalone images (`standalone.grpc.Dockerfile`, `standalone.rest.Dockerfile`) so the
full stack comes up with a single `podman compose up`. Persistent postgres data lives in a
named volume so the database survives container restarts:

```yaml
volumes:
  json-keys-postgres-data:
```

### Test compose files

Use standalone images too — tests need a clean, self-contained stack. Do not mount persistent
volumes in test compose files: the test stack must start from scratch every run.

Integration test compose files do not expose the postgres port to the host — only the service
port is exposed. The test process talks to the service, not directly to the database.

### Postgres environment variables

All postgres services require exactly these four environment variables:

```yaml
environment:
  POSTGRES_PASSWORD: "${POSTGRES_PASSWORD}"
  POSTGRES_USER: "${POSTGRES_USER}"
  POSTGRES_DB: "${POSTGRES_DB}"
  POSTGRES_HOST_AUTH_METHOD: scram-sha-256
  POSTGRES_INITDB_ARGS: --auth=scram-sha-256
```

The last two configure password authentication. Never use `trust` authentication even for
local development — it accepts any connection without a password.

---

## .dockerignore

A `.dockerignore` file at the repo root limits what is sent to the build context. Keep it
current: sending `.git` or `node_modules` to the daemon wastes seconds on every build.

Always exclude:

- `.git` — can be hundreds of MB; no Dockerfile needs it
- `node_modules` — potentially hundreds of MB; not part of any Go build
- `**/*_test.go` — test files are not compiled into production binaries

---

## Common Pitfalls

- **`go mod download` after source COPY.** Invalidates the module cache on every source change.
  Always download modules after only `go.mod`/`go.sum`, before any other COPY.
- **Missing `CGO_ENABLED=0`.** Produces a binary that dynamically links against the host's
  C library. On Alpine (musl libc), this can cause "not found" errors at runtime. Set it as
  `ENV CGO_ENABLED=0` at the top of the builder stage.
- **Missing `-ldflags="-s -w"`.** Go binaries without strip flags carry symbol tables and
  DWARF debug info that inflate binary size by ~30%. Always include these flags.
- **Using `@latest` for tool installs.** `go install grpcurl@latest` resolves at build time
  and produces different binaries on different dates. Pin to a specific version tag.
- **`apk add curl` in Alpine runtime.** BusyBox `wget` is already present in any Alpine image.
  Use `wget -qO /dev/null <url>` instead of installing curl.
- **`ARG DEBIAN_FRONTEND=noninteractive` in Alpine stages.** This Debian/Ubuntu flag is a
  no-op in Alpine and should not appear in Alpine-based runtime stages.
- **Missing `ca-certificates` with `--no-install-recommends`.** When `apt-get install` is used
  with `--no-install-recommends`, `ca-certificates` is not pulled in transitively, causing HTTPS
  connections (e.g., `git clone`) to fail with an SSL error. Always include `ca-certificates`
  explicitly when using `--no-install-recommends` in a Debian/Ubuntu stage that makes HTTPS
  requests.
- **Build tools in the database final stage.** Without a multi-stage build, `apt-get install`
  and `git clone` layers remain in the final image even after cleanup RUN commands. The cleanup
  removes files from new layers but not from previous ones. Always use multi-stage builds when
  compiling extensions from source.
- **Hardcoded paths after a major PostgreSQL version bump.** The pg_cron extension files live
  at `/usr/lib/postgresql/18/lib/` and `/usr/share/postgresql/18/extension/`. After upgrading
  from `postgres:18.x` to `postgres:19.x`, update both the builder's `postgresql-server-dev-18`
  package and these COPY paths.
- **Using standalone images in production.** Standalone images run migrations and key rotation
  at startup, which is unsafe for production deployments where multiple replicas start
  concurrently. Use the dedicated job images instead.
- **Forgetting to update `.dockerignore` when adding new top-level directories.** Large
  directories (generated artifacts, downloaded tools) added at the repo root will bloat the
  build context unless excluded.
- **Bare `depends_on` without a healthcheck condition.** `depends_on: [postgres]` waits only
  for the container to start, not for postgres to be ready. A standalone image that runs
  migrations at startup will fail with a connection error if postgres is still initializing.
  Always use `condition: service_healthy` and ensure the dependency has a `HEALTHCHECK`.
- **Hardcoded credentials or ports in `POSTGRES_DSN`.** The DSN must use environment variable
  substitution: `postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@host:5432/${POSTGRES_DB}`.
  Never hardcode usernames, passwords, or database names. Note: use port `5432` (the internal
  container port), not `${POSTGRES_PORT}` (the host-mapped port) — services communicate over
  the compose network, not through the host.
- **`RUN chmod +x` after COPY.** Use `COPY --chmod=755 <src> <dst>` instead — it is a single
  instruction and avoids an extra layer.
