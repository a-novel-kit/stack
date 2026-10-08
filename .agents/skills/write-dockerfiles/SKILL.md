---
name: write-dockerfiles
description: >
  Write or review Dockerfiles, compose files, and builds/ assets for services and development
  images. Load for any builds/ change; add language skills for scripts or SQL.
---

# Dockerfile and Compose Writing Skill

This skill governs how to write and maintain container build files for Agora backend services.
All build artifacts live under `builds/`. Read the relevant section for the task at hand; the
conventions at the end apply to every Dockerfile.

**Before writing or editing any Dockerfile**, read the existing files in `builds/`. Every Dockerfile
here follows the same structure — copy it, don't invent a new one.

---

## After Every Edit

Rebuild the affected image after changing any Dockerfile, to catch syntax errors and confirm the
build still succeeds:

```
podman build --format docker -f ./builds/<name>.Dockerfile -t <name>:local .
```

Run `a-novel build --type=podman -y` to rebuild all images at once. Never assume a build is correct
without running it — layer caching can make a previously-failing step appear to succeed on a stale
cache.

---

## Architecture: Service Images vs Job Images

This project separates the main process from maintenance work:

- **Main process images** (`grpc.Dockerfile`, `rest.Dockerfile`): long-running servers, expecting a
  fully migrated database before they start. They contain only the server binary and its healthcheck
  tool.

- **Job images** (`migrations.Dockerfile`, `rotate-keys.Dockerfile`): short-lived, run-to-completion
  containers, run as Kubernetes Jobs or equivalent before the main process starts. They carry only
  the binary needed for their single task.

- **Standalone images** (`standalone.grpc.Dockerfile`, `standalone.rest.Dockerfile`): dev and
  integration-test convenience images, bundling the server, migrations, and rotate-keys into a single
  container and running migrations and rotation before starting the server. Never use standalone
  images in production.

- **Database image** (`database.Dockerfile`): PostgreSQL and pgBackRest on Wolfi, plus whatever
  extensions the service needs. Migrations are not baked in — run the migrations job image against
  it separately.

This separation keeps production images minimal: the server binary doesn't carry migration code
it never runs, and job images don't carry server code.

---

## Go Service and Job Images

All Go images follow the same two-stage build pattern.

### Builder stage

```dockerfile
FROM docker.io/library/golang:1.26.2-alpine AS builder

# Produce a fully static binary with no C library dependency: required on Alpine (musl
# libc), and it keeps the final image free of dynamic linker dependencies.
ENV CGO_ENABLED=0

WORKDIR /app

# ── Layer caching: download dependencies before copying source ──────────────────────────
# go mod download runs before any source COPY, so the module cache layer survives rebuilds
# that only touch source. Only go.mod or go.sum invalidates it.
COPY go.mod go.sum ./
RUN go mod download

# ── Optional: install tools used in the final image ────────────────────────────────────
# A tool the image needs (grpcurl for a healthcheck, say) installs here — after go mod
# download, before the source COPY — so it caches independently of source changes.
RUN GOBIN=/usr/local/bin go install github.com/fullstorydev/grpcurl/cmd/grpcurl@v1.9.3

# ── Copy source files ──────────────────────────────────────────────────────────────────
# Copy only the packages this binary needs, never the entire repo: keeps the build context
# small and avoids invalidating unrelated caches.
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

**Build flags**: all three (`CGO_ENABLED=0`, `-ldflags="-s -w"`, `-trimpath`) are required on every
`go build`. Set `CGO_ENABLED=0` once as `ENV` at the top of the stage rather than inlining it per
build command.

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

The runtime stage contains only the binary (or binaries) and their runtime requirements: no shell
tools, no package managers, no build artifacts. Alpine is the runtime base for all Go images — it
provides a shell (required for standalone images' `sh -c` CMD), BusyBox utilities, and a small
footprint.

---

## Healthchecks

Every image that serves traffic must have a `HEALTHCHECK`.

### gRPC healthcheck

Use `grpcurl` against the standard `grpc.health.v1.Health/Check` endpoint:

```dockerfile
HEALTHCHECK --interval=1s --timeout=5s --retries=10 --start-period=1s \
  CMD grpcurl --plaintext -d '' localhost:8080 grpc.health.v1.Health/Check || exit 1
```

Install `grpcurl` in the builder (`RUN GOBIN=/usr/local/bin go install .../grpcurl@v1.9.3`) and
`COPY --from=builder` it into the runtime stage — both lines appear in the stage examples above. Pin
the version, never `@latest`.

### REST healthcheck

Alpine's BusyBox includes `wget` — no extra `apk add` required:

```dockerfile
HEALTHCHECK --interval=1s --timeout=5s --retries=10 --start-period=1s \
  CMD wget -qO /dev/null http://localhost:8080/ping || exit 1
```

Never add `curl` to the runtime image just for a healthcheck. BusyBox `wget` is already present in
any Alpine-based image and serves the same purpose at zero size cost.

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

Every service with a database ships the same image recipe, which infra's database hosts and backup
repositories run: signed Wolfi packages assembled by apko, plus pgBackRest built from its
checksum-verified upstream release. Copy `builds/database.Dockerfile` and `builds/database.apko.yaml`
from `service-json-keys`, then add the service's own packages and init SQL. Infra relies on what the
recipe preserves: UID/GID 999, `PGDATA=/var/lib/postgresql/18/docker`, the upstream
`docker-entrypoint.sh`, `bash`, `pg_isready`, and `pgbackrest` on the `PATH`.

- **Pin service packages in the apko manifest** (`postgresql-18=18.6-r5`, `pg_cron-18=1.6.8-r0`).
  The shared Renovate preset tracks every `name=version` line; base libraries stay unpinned.
- **Extensions come from Wolfi packages.** Find the exact release in Wolfi's `APKINDEX` before
  pinning. Only pgBackRest is source-built, in a discarded stage, so compilers never reach the runtime.
- **Server settings an extension needs at init** go in `postgresql.conf.sample`
  (`/usr/share/postgresql18/`), which initdb copies: the server that runs the init SQL has them too.
- **pg_cron needs `cron.use_background_workers = on`.** By default a job logs in over TCP without a
  password, and SCRAM authentication rejects it, so every run fails with `connection failed`. Set
  `cron.database_name` from `POSTGRES_DB` in an entrypoint wrapper, and only for the `postgres`
  command: infra also runs the image with other commands.
- **A Wolfi data directory is not portable from the Debian image.** Moving between them needs a fresh
  volume, or a logical dump and restore.

**Executable files**: use `COPY --chmod=755` when copying shell scripts or other executables into
the image. It sets the executable bit in a single instruction and avoids a separate `RUN chmod +x`
layer:

```dockerfile
COPY --chmod=755 ./builds/database.entrypoint.sh /usr/local/bin/database.entrypoint.sh
```

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

- Always set `context: ..` (the repo root) so Dockerfiles can reference files anywhere in the repo
  with paths relative to the root.
- Use named networks with descriptive, scenario-scoped names (e.g.,
  `json-keys-integration-grpc-test`) to prevent cross-compose network leakage when scenarios run
  concurrently.
- All ports and credentials come from environment variables (sourced from `setup-env.sh`). Never
  hardcode port numbers or passwords in compose files.
- Use `depends_on` with `condition: service_healthy` so the dependent service waits for the
  dependency's `HEALTHCHECK` to pass, not just for the container to start. Without it, a standalone
  image that runs migrations at startup may connect to postgres before it accepts connections and
  fail:

  ```yaml
  service-json-keys:
    depends_on:
      postgres-json-keys:
        condition: service_healthy
  ```

  This requires the dependency to define a `HEALTHCHECK` in its Dockerfile (`pg_isready` for the
  database image). The chain: postgres container starts → `pg_isready` passes → service container
  starts → migrations run against a ready database.

### Development compose (`podman-compose.yaml`)

Uses standalone images (`standalone.grpc.Dockerfile`, `standalone.rest.Dockerfile`) so the full
stack comes up with a single `podman compose up`. Persistent postgres data lives in a named volume,
so the database survives container restarts:

```yaml
volumes:
  json-keys-postgres-data:
```

### Test compose files

Use standalone images too — tests need a clean, self-contained stack. Never mount persistent volumes
in test compose files: the test stack must start from scratch every run.

Integration test compose files expose only the service port, not the postgres port: the test process
talks to the service, not directly to the database.

### Postgres environment variables

All postgres services require exactly these five environment variables:

```yaml
environment:
  POSTGRES_PASSWORD: "${POSTGRES_PASSWORD}"
  POSTGRES_USER: "${POSTGRES_USER}"
  POSTGRES_DB: "${POSTGRES_DB}"
  POSTGRES_HOST_AUTH_METHOD: scram-sha-256
  POSTGRES_INITDB_ARGS: --auth=scram-sha-256
```

The last two configure password authentication. Never use `trust` authentication, even locally — it
accepts any connection without a password.

---

## .dockerignore

A `.dockerignore` file at the repo root limits what is sent to the build context. Keep it current:
sending `.git` or `node_modules` to the daemon wastes seconds on every build.

Always exclude:

- `.git` — can be hundreds of MB; no Dockerfile needs it
- `node_modules` — potentially hundreds of MB; not part of any Go build
- `**/*_test.go` — test files are not compiled into production binaries

---

## Common Pitfalls

- **`go mod download` after source COPY.** Invalidates the module cache on every source change.
  Always download modules after only `go.mod`/`go.sum`, before any other COPY.
- **Missing `CGO_ENABLED=0`.** Produces a binary dynamically linked against the host's C library,
  which can fail with "not found" at runtime on Alpine (musl libc). Set it as `ENV CGO_ENABLED=0` at
  the top of the builder stage.
- **Missing `-ldflags="-s -w"`.** Symbol tables and DWARF debug info inflate the binary by ~30%.
  Always include these flags.
- **Using `@latest` for tool installs.** `go install grpcurl@latest` resolves at build time
  and produces different binaries on different dates. Pin to a specific version tag.
- **`apk add curl` in Alpine runtime.** Use the BusyBox `wget -qO /dev/null <url>` already present in
  any Alpine image.
- **Build tools in the database runtime.** A cleanup `RUN` removes files from new layers but not
  from previous ones. Compile in a discarded stage and copy only the binary, as pgBackRest is.
- **Hardcoded paths after a major PostgreSQL version bump.** Moving from PostgreSQL 18 to 19 changes
  the package names in the apko manifest, the `postgresql18` paths and symlinks in the Dockerfile,
  and `PGDATA`.
- **Using standalone images in production.** They run migrations and key rotation at startup, which
  is unsafe where multiple replicas start concurrently. Use the dedicated job images instead.
- **Forgetting to update `.dockerignore` when adding new top-level directories.** Large directories
  (generated artifacts, downloaded tools) at the repo root bloat the build context unless excluded.
- **Bare `depends_on` without a healthcheck condition.** `depends_on: [postgres]` waits only for the
  container to start, so a standalone image running migrations at startup fails with a connection
  error while postgres is still initializing. Always use `condition: service_healthy` and ensure the
  dependency has a `HEALTHCHECK`.
- **Hardcoded credentials or ports in `POSTGRES_DSN`.** The DSN must use environment variable
  substitution: `postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@host:5432/${POSTGRES_DB}`.
  Never hardcode usernames, passwords, or database names. Use port `5432` (the internal container
  port), not `${POSTGRES_PORT}` (the host-mapped port) — services communicate over the compose
  network, not through the host.
- **`RUN chmod +x` after COPY.** Use `COPY --chmod=755 <src> <dst>` instead — a single instruction,
  one fewer layer.
