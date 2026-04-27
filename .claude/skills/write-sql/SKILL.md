---
name: write-sql
description: >
  Write, review, and maintain SQL files for Agora backend services. Use this skill whenever
  creating or editing SQL files — DAO query files (internal/dao/*.sql), schema migrations
  (internal/models/migrations/*.sql), or any raw SQL statement embedded in Go. Applies to
  all PostgreSQL-targeting SQL: SELECT/INSERT/UPDATE/DELETE queries, DDL (tables, views,
  indexes, constraints), materialized views, and scheduled pg_cron jobs.
---

# SQL Writing Skill

This skill governs how to write and maintain SQL files in Agora backend services. SQL appears
in two distinct contexts — **DAO query files** and **schema migrations** — each with its own
lifecycle and risk profile. Read the relevant section for the task at hand; the PostgreSQL
conventions at the end apply to both.

**Before writing any SQL**, read the existing files in the same directory. DAO queries and
migrations follow established patterns — follow them exactly. For migrations in particular,
also read the most recent `.up.sql` and `.down.sql` to understand the current schema state
before changing it.

---

## After Every Edit

Run these in order after changing any SQL file:

```
make format   # applies Go module tidy and golangci-lint auto-fixes (also re-embeds SQL)
make lint     # catches any Go-level issues introduced by the SQL change
```

SQL files are plain text embedded into Go — the Go toolchain does not format them
independently. After editing a `.sql` file, rebuild with `make format` to ensure the embedding
is up to date and any auto-fixable lint issues are resolved.

Then invoke the **`write-go-tests` skill** to verify (or update) the DAO test for the changed
query. SQL changes often affect query results in ways that existing test fixtures will surface.

---

## DAO Query Files (`internal/dao/*.sql`)

DAO query files hold the raw SQL for a single database operation. They are embedded into
the companion Go file with `//go:embed` and executed via bun's parameterized query API.

### File Naming

DAO SQL files follow the same naming convention as the Go file they serve, with `.sql`
replacing `.go`:

| Go file             | SQL file             |
| ------------------- | -------------------- |
| `pg.jwkSearch.go`   | `pg.jwkSearch.sql`   |
| `pg.userSelect.go`  | `pg.userSelect.sql`  |
| `pg.orderInsert.go` | `pg.orderInsert.sql` |

One SQL file per DAO operation. Never combine multiple queries in one file.

### Embedding

The SQL file is embedded at package level in the companion `.go` file using an unexported
variable:

```go
//go:embed pg.jwkSearch.sql
var jwkSearchQuery string
```

The `//go:embed` directive and its variable must be at package level — never inside a function.
Never inline SQL as a raw Go string literal.

### Parameterization

Use bun's positional parameter syntax: `?0`, `?1`, `?2`, ... (zero-indexed). These map directly
to the arguments passed to `db.NewRaw(query, arg0, arg1, ...)`:

```sql
SELECT
  *
FROM
  active_keys
WHERE
  usage = ?0
ORDER BY
  created_at DESC
LIMIT
  ?1;
```

```go
tx.NewRaw(jwkSearchQuery, request.Usage, KeysMaxBatchSize).Scan(ctx, &entities)
```

Never use PostgreSQL's native `$1`, `$2`, ... syntax in DAO files — that is the `pgx` driver
convention and is not substituted by bun's `NewRaw`. Never build SQL by concatenating strings
or using `fmt.Sprintf`.

### Return Patterns

- **SELECT** queries scan into a struct or slice. Use `SELECT *` when returning a full model row —
  bun maps columns to struct fields via `bun:` tags.
- **INSERT**, **UPDATE**, and **DELETE** queries that must return the affected row use
  `RETURNING *`:

  ```sql
  INSERT INTO keys (id, private_key, ...) VALUES (?0, ?1, ...) RETURNING *;
  ```

  ```sql
  UPDATE keys
  SET
    deleted_at = ?0,
    deleted_comment = ?1
  WHERE
    id = ?2
    AND deleted_at IS NULL
  RETURNING
    *;
  ```

  `RETURNING *` gives the caller the full row (including server-generated timestamps) in a
  single round-trip. The Go caller passes the result directly to `Scan(ctx, entity)`.

### Read vs Write Targets

This service maintains two objects for the `keys` entity:

- **`keys`** — the base table. All writes (INSERT, UPDATE) target this table directly.
- **`active_keys`** — a materialized view that exposes only non-expired, non-deleted rows.
  All reads (SELECT) target this view.

Never read from `keys` directly in a DAO query — `active_keys` enforces the expiry and
soft-delete rules automatically. Never write to `active_keys`.

---

## Schema Migrations (`internal/models/migrations/*.sql`)

Migrations are the authoritative history of the database schema. Every schema change must
go through a migration — no out-of-band DDL.

### File Naming

```
YYYYMMDDHHMMSS_<description>.<up|down>.sql
```

The timestamp is the current wall-clock time **down to the second**. Always run
`date '+%Y%m%d%H%M%S'` immediately before creating the files to get the exact value. Never
truncate to minutes, never guess, never reuse an existing timestamp.

The description uses underscores and is as specific as possible:

```
20250113182800_keys_table.up.sql
20250113182800_keys_table.down.sql
20250713173700_improve_expiry_management.up.sql
20250713173700_improve_expiry_management.down.sql
20260416152344_add_user_soft_delete.up.sql
20260416152344_add_user_soft_delete.down.sql
```

### Always Create Both Up and Down

**Every migration requires a paired `.up.sql` and `.down.sql`.** The down migration must
fully reverse the up migration so that a rollback restores the exact prior schema state.

When reversal is inherently destructive (e.g., the down migration drops a table and loses
all data), that is expected — document it with a comment and use `IF EXISTS` guards.

If a change is genuinely irreversible (e.g., dropping a column that held data), still write
the best approximation of a reversal (e.g., re-add the column without its data) and add a
comment acknowledging the limitation.

### Immutability of Committed Up Migrations

**Never modify a `.up.sql` file that has been merged to `master`.** Migrations are applied
once, in order, and are never re-run. Editing an applied migration has no effect on any
environment where it already ran, and silently creates a divergence between the codebase
and the actual schema.

Permitted exceptions — changes with no runtime effect:

- Adding, editing, or removing comments (`--` and `/* */`).
- Reformatting whitespace or alignment.

For everything else — including fixing a bug in an existing migration — create a **new**
migration with a current timestamp.

**Up migrations on the current branch (not yet merged to `master`) and all down migrations
may be edited freely**, since they have not yet been applied to any shared environment.

### Statement Splitting: `--bun:split`

bun's migration runner executes each file as a single database round-trip by default. When
a migration contains statements that must execute sequentially (where B depends on A having
committed), separate them with `--bun:split`:

```sql
-- Step 1: drop the old plain view
DROP VIEW IF EXISTS active_keys;

--bun:split
-- Step 2: create the materialized view and its index together
CREATE MATERIALIZED VIEW active_keys AS (...);

CREATE INDEX active_keys_usage_idx ON active_keys (usage);

--bun:split
-- Step 3: populate the view (requires step 2 to have completed)
REFRESH MATERIALIZED VIEW active_keys;

--bun:split
-- Step 4: schedule background refresh (requires the view to exist)
SELECT cron.schedule('refresh-active-keys', '0 * * * *', $$...$$);
```

Statements that form one logical unit and have no ordering dependency (e.g., `CREATE TABLE`
followed by `CREATE INDEX` on that table) do not need splitting — PostgreSQL handles multiple
DDL statements in one round-trip. Use `--bun:split` only when a later statement genuinely
requires an earlier one to have committed first.

### Down Migration Ordering

Down migrations reverse the up migration in **reverse creation order**:

- What was created last is dropped first.
- Scheduled pg_cron jobs are unscheduled before the objects they reference are dropped.
- Indexes are dropped before the table or view they index.
- Dependent objects (views, materialized views) are dropped before the tables they read from.

Example — down for a migration that created a materialized view with an index and a pg_cron job:

```sql
-- Unschedule first, before the view it depends on is dropped.
SELECT cron.unschedule('refresh-active-keys');

--bun:split
DROP INDEX IF EXISTS active_keys_usage_idx;

DROP MATERIALIZED VIEW IF EXISTS active_keys;

--bun:split
-- Restore the prior plain view.
CREATE VIEW active_keys AS (...);
```

### Guard Clauses in Down Migrations

In down migrations, always use `IF EXISTS` so that a partial rollback or a re-application
does not fail on missing objects:

```sql
DROP TABLE IF EXISTS keys;

DROP INDEX IF EXISTS keys_usage_idx;

DROP VIEW IF EXISTS active_keys;

DROP MATERIALIZED VIEW IF EXISTS active_keys;
```

In up migrations, use `IF NOT EXISTS` only when the migration is explicitly designed to be
idempotent (e.g., adding a standalone index that is safe to re-apply). Do not use it for
table creation — the timestamp uniqueness makes re-application impossible under normal
operation, and masking accidental re-application is worse than surfacing it as an error.

### pg_cron Scheduled Jobs

When a migration adds a pg_cron job, the paired down migration must unschedule it by the
same name:

```sql
-- up
SELECT
  cron.schedule (
    'refresh-active-keys',
    '0 * * * *',
    $$REFRESH MATERIALIZED VIEW CONCURRENTLY active_keys;$$
  );

-- down
SELECT
  cron.unschedule ('refresh-active-keys');
```

Job names are global within the PostgreSQL instance. Use a descriptive, service-scoped
name: `refresh-active-keys`, not `refresh`. Avoid generic names that could collide with
jobs from other services.

### Materialized Views

When creating a materialized view that will ever be refreshed with `CONCURRENTLY`, a unique
index on the view is required by PostgreSQL:

```sql
CREATE MATERIALIZED VIEW active_keys AS (...);

CREATE UNIQUE INDEX active_keys_id_idx ON active_keys (id);
```

PostgreSQL does not inherit constraints or indexes from the source table into a materialized
view. Without the unique index, `REFRESH MATERIALIZED VIEW CONCURRENTLY` silently fails at
runtime (the scheduler's hourly job runs but does nothing). Always add the unique index in
the same migration that creates the view, or immediately follow with a separate migration if
the view already exists.

### Migration Comments

Explain the **why**, not the **what**. The SQL already says what it does; comments should
explain why the change was necessary or why a particular approach was chosen:

```sql
-- Converts active_keys from a plain view to a materialized view for read performance,
-- and replaces the COALESCE-based filter with explicit conditions so that deleted_at
-- can no longer be used as a backdoor expiry for keys that have not been revoked.
DROP VIEW IF EXISTS active_keys;
```

For inline column documentation inside `CREATE TABLE`, use block comments `/* */`
immediately after the column definition:

```sql
CREATE TABLE keys (
  id uuid PRIMARY KEY NOT NULL,
  /* Encrypted private key in JSON Web Key format, base64url-encoded. */
  private_key text NOT NULL CHECK (private_key <> ''),
  /* Public key in JSON Web Key format, base64url-encoded. Null for symmetric keys. */
  public_key text,
  /* Hard expiry date. Once passed, the key is excluded from the active view. */
  expires_at timestamp(0) with time zone NOT NULL,
  /* Soft-delete timestamp. Set when a key is revoked early (e.g., due to a compromise). */
  deleted_at timestamp(0) with time zone
);
```

---

## PostgreSQL Conventions

These rules apply to all SQL files — both DAO queries and migrations.

### Formatting

- SQL keywords in **ALL CAPS**: `SELECT`, `FROM`, `WHERE`, `INSERT INTO`, `UPDATE`, `SET`,
  `RETURNING`, `ORDER BY`, `LIMIT`, `AND`, `OR`, `NOT`, `NULL`, `IS`, `IN`, `LIKE`, etc.
- Each major clause on its own line; clause body indented two spaces:

  ```sql
  SELECT
    *
  FROM
    active_keys
  WHERE
    usage = ?0
    AND expires_at > CURRENT_TIMESTAMP
  ORDER BY
    created_at DESC
  LIMIT
    ?1;
  ```

- Multi-column lists (SELECT fields, INSERT column list, VALUES) have each item on its own
  line, indented two spaces.
- End every statement with `;`.
- Use `--` for standalone comments; `/* */` for inline column docs inside `CREATE TABLE`.

### Column Types

| Use case                | Type                              |
| ----------------------- | --------------------------------- |
| Entity identifier       | `uuid`                            |
| Short or long text      | `text` (never `varchar(n)`)       |
| Boolean                 | `boolean`                         |
| Integer                 | `smallint` / `integer` / `bigint` |
| Exact decimal / money   | `numeric(p, s)` (never `float`)   |
| Timestamp with timezone | `timestamp(0) with time zone`     |
| JSON payload            | `jsonb` (not `json`)              |
| Array                   | element type followed by `[]`     |

**Always use `timestamp(0) with time zone`.** The `(0)` precision truncates sub-second noise,
making values round-trippable through Go's `time.Time` without drift. Never use bare `timestamp`
(no timezone) — timezone-naive timestamps cause subtle bugs in multi-region or DST-affected
deployments.

Never use `varchar(n)`. PostgreSQL has no performance advantage over `text`, and length
constraints belong in the service layer unless they represent a true database invariant.

### Required Text Fields

For required text columns that must never be empty, add an explicit `CHECK` constraint:

```sql
private_key text NOT NULL CHECK (private_key <> '')
```

Go's zero-value semantics make it easy to accidentally persist empty strings; the check
constraint is the last line of defense.

### Primary Keys

Use `uuid` for all entity primary keys. Never use `serial` or `bigserial` — auto-increment
integers leak row counts and make client-side ID generation impossible. Generate UUIDs in Go
before the INSERT so the caller always has the ID without a database round-trip.

### Indexes

- Index every column used in a `WHERE` clause of a frequent query.
- Index foreign key columns (PostgreSQL does not auto-index them).
- Use partial indexes when a query always filters by a known condition:

  ```sql
  CREATE INDEX keys_active_usage_idx ON keys (usage)
  WHERE
    deleted_at IS NULL;
  ```

- For `ORDER BY` columns on tables that will grow large, index the sort column to avoid
  sequential scans.
- Unique indexes serve double duty as uniqueness constraints. Prefer them over `UNIQUE` column
  constraints when the index needs to be added after the table is created, or when `IF NOT EXISTS`
  semantics are needed.

### Soft Deletes

Never hard-delete auditable entities. Use the soft-delete pattern established in the `keys`
table:

```sql
deleted_at timestamp(0) with time zone, -- null = not deleted
deleted_comment text -- reason; required when deleted_at is set
```

The `active_*` view or materialized view filters out soft-deleted rows automatically. Direct
database queries can still access them for auditing.

### Time References

Use `CURRENT_TIMESTAMP` for the current time in both queries and DDL. Never use `NOW()` —
it is equivalent but `CURRENT_TIMESTAMP` is the SQL standard form and is used consistently
throughout this codebase:

```sql
WHERE
  expires_at > CURRENT_TIMESTAMP
```

---

## Common Pitfalls

- **Modifying a committed up migration.** Create a new migration instead. The only permitted
  changes are comments and whitespace.
- **Missing down migration.** Every `.up.sql` requires a paired `.down.sql` that fully reverses
  the schema change.
- **Wrong timestamp.** Run `date '+%Y%m%d%H%M%S'` to get the exact current time before naming
  a new migration pair. Never guess or truncate to minutes.
- **Using `$1` in DAO query files.** bun's `NewRaw` uses `?0`, `?1`, ... (zero-indexed). The
  `$1` syntax is for `pgx`-style drivers and is not substituted by bun.
- **Inlining SQL in DAO Go files.** All DAO query SQL lives in `.sql` files embedded with
  `//go:embed` at package level. No raw string literals, no `fmt.Sprintf` in `internal/dao/`.
  Short, parameterless maintenance SQL in `cmd/` entry points (e.g., `REFRESH MATERIALIZED VIEW`)
  is acceptable as an inline raw string, since it is a one-time operational command rather than a
  reusable query.
- **Reading from `keys` in a DAO query.** Always read from `active_keys`. The view enforces
  expiry and soft-delete filtering automatically.
- **Missing `RETURNING *` on mutating queries.** INSERT, UPDATE, and DELETE that return the
  affected row use `RETURNING *`. The caller scans the result into the model struct.
- **Missing `--bun:split` between dependent statements.** When statement B cannot execute
  until statement A has committed (e.g., `REFRESH MATERIALIZED VIEW` after `CREATE`), add
  `--bun:split` between them.
- **Materialized view without a unique index.** `REFRESH MATERIALIZED VIEW CONCURRENTLY`
  silently fails without a unique index on the view. Add `CREATE UNIQUE INDEX` in the same
  migration that creates the view.
- **Unordered down migration statements.** Drop objects in reverse creation order: unschedule
  pg_cron jobs first, then drop dependent indexes, then drop views, then drop tables.
- **Missing `IF EXISTS` in down migrations.** Always guard drops with `IF EXISTS` so that a
  partial rollback does not fail.
- **Using `varchar(n)`.** Use `text` instead; length constraints belong in the service layer.
- **Using bare `timestamp` without timezone.** Always `timestamp(0) with time zone`.
- **Using `NOW()` instead of `CURRENT_TIMESTAMP`.** Use `CURRENT_TIMESTAMP` for consistency.
- **Generic pg_cron job names.** Use descriptive, service-scoped names (`refresh-active-keys`,
  not `refresh`) to avoid collisions with jobs from other services sharing the same database.
