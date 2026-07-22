---
name: write-sql
description: >
  Write, review, and maintain PostgreSQL SQL for Agora backend services. Use whenever creating
  or editing SQL — DAO query files (internal/dao/*.sql), schema migrations
  (internal/models/migrations/*.sql), or raw SQL embedded in Go: SELECT/INSERT/UPDATE/DELETE
  queries, DDL (tables, views, indexes, constraints), materialized views, pg_cron jobs.
---

# SQL Writing Skill

SQL appears in two contexts — **DAO query files** and **schema migrations** — each with its own
lifecycle and risk profile. Read the section for the task at hand; the PostgreSQL conventions at
the end apply to both.

**Before writing any SQL**, read the existing files in the same directory and follow their
patterns exactly. For migrations, also read the most recent `.up.sql` and `.down.sql` to learn
the current schema state before changing it.

---

## After Every Edit

Run these in order after changing any SQL file:

```
pnpm format     # prettier — this is what formats SQL (via prettier-plugin-sql)
pnpm format:go  # only when the SQL change rippled into Go files
pnpm lint:go    # catches any Go-level issues introduced by the SQL change
```

**`pnpm format` is the one that matters for SQL.** The Go-only `pnpm format:go` does not touch
`.sql`, so running just that after a SQL edit leaves indentation, comment, and whitespace drift
in place. CI's `lint-node` stage runs `prettier --check` and fails on the unformatted file even
when Go lint is clean. Run `pnpm format` before pushing any `.sql` edit.

Then invoke the **`write-go-tests` skill** to verify or update the DAO test for the changed
query. SQL changes often shift query results in ways existing fixtures surface.

---

## DAO Query Files (`internal/dao/*.sql`)

DAO query files hold the raw SQL for a single database operation. They are embedded into
the companion Go file with `//go:embed` and executed via bun's parameterized query API.

### File Naming

DAO SQL files mirror the Go file they serve, with `.sql` replacing `.go`:

| Go file             | SQL file             |
| ------------------- | -------------------- |
| `pg.jwkSearch.go`   | `pg.jwkSearch.sql`   |
| `pg.userSelect.go`  | `pg.userSelect.sql`  |
| `pg.orderInsert.go` | `pg.orderInsert.sql` |

One SQL file per DAO operation. Never combine multiple queries in one file.

### Embedding

The SQL file is embedded at package level in the companion `.go` file with an unexported
variable:

```go
//go:embed pg.jwkSearch.sql
var jwkSearchQuery string
```

The `//go:embed` directive and its variable must be at package level — never inside a function.
Never inline DAO SQL as a raw Go string literal, and never build SQL with `fmt.Sprintf`. Short,
parameterless maintenance SQL in `cmd/` entry points (e.g. `REFRESH MATERIALIZED VIEW`) may stay
an inline raw string, being a one-time operational command rather than a reusable query.

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
convention and is not substituted by bun's `NewRaw`. Never build SQL by concatenating strings.

### Return Patterns

- **SELECT** queries scan into a struct or slice. Use `SELECT *` when returning a full model row —
  bun maps columns to struct fields via `bun:` tags.
- **INSERT**, **UPDATE**, and **DELETE** queries that must return the affected row use
  `RETURNING *`:

  ```sql
  INSERT INTO
    keys (id, private_key, created_at)
  VALUES
    (?0, ?1, ?2)
  RETURNING
    *;
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

The timestamp is the current wall-clock time **down to the second**: run
`date '+%Y%m%d%H%M%S'` immediately before creating the files. Never truncate to minutes, guess,
or reuse an existing timestamp.

The description uses underscores and is as specific as possible:

```
20250113182800_keys_table.up.sql
20250113182800_keys_table.down.sql
20260416152344_add_user_soft_delete.up.sql
20260416152344_add_user_soft_delete.down.sql
```

### Always Create Both Up and Down

**Every migration requires a paired `.up.sql` and `.down.sql`.** The down migration must
fully reverse the up migration so that a rollback restores the exact prior schema state.

When reversal is inherently destructive (the down migration drops a table and loses all its
data), that is expected — document it with a comment and use `IF EXISTS` guards.

If a change is irreversible (dropping a column that held data), still write the closest
approximation of a reversal — re-add the column without its data — and comment on the
limitation.

### Immutability of Committed Up Migrations

**Never modify a `.up.sql` file that has been merged to `master`.** Migrations are applied
once, in order, and are never re-run. Editing an applied migration has no effect where it
already ran, and silently diverges the codebase from the actual schema.

Permitted exceptions — changes with no runtime effect:

- Adding, editing, or removing comments (`--` and `/* */`).
- Reformatting whitespace or alignment.

For everything else — including fixing a bug in an existing migration — create a **new**
migration with a current timestamp.

**Up migrations on the current branch (not yet merged to `master`) and all down migrations
may be edited freely**, since they have not yet been applied to any shared environment.

#### Exception: `service-template` edits its initial migrations in place

**`a-novel/service-template` is exempt from the immutability rule.** Nothing is ever published or
deployed from a template, so no environment has already applied its migrations and there is no
divergence to create. The rule's entire premise is absent.

In that repo, change the schema by **editing the initial migration directly** — do not add a new one.
Every generated service inherits the template's migration set, so a second migration that only
patches the first is propagated forever with no purpose.

```sql
-- service-template: edit 20250306000000_items_table.up.sql in place
created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
```

```sql
-- NOT this — a corrective migration in a repo that never runs migrations against a live database
ALTER TABLE items ALTER COLUMN created_at TYPE timestamp with time zone;
```

This applies **only** to `service-template`. Every real service — `service-authentication`,
`service-json-keys`, `service-narrative-engine`, and anything generated from the template — has
deployed environments and follows the immutability rule above without exception.

### Statement Splitting: `--bun:split`

bun's migration runner executes each file as a single database round-trip by default. When a
migration holds statements that must run sequentially (B depends on A having committed),
separate them with `--bun:split`:

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

Statements with no ordering dependency (`CREATE TABLE` then `CREATE INDEX` on that table) need
no split — PostgreSQL handles multiple DDL statements in one round-trip. Split only when a later
statement requires an earlier one to have committed first.

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

In down migrations, always use `IF EXISTS` so a partial rollback or a re-application does not
fail on missing objects:

```sql
DROP TABLE IF EXISTS keys;

DROP INDEX IF EXISTS keys_usage_idx;

DROP VIEW IF EXISTS active_keys;

DROP MATERIALIZED VIEW IF EXISTS active_keys;
```

In up migrations, use `IF NOT EXISTS` only where the migration is designed to be idempotent
(adding a standalone index that is safe to re-apply). Do not use it for table creation — the
timestamp uniqueness makes re-application impossible under normal operation, and masking an
accidental re-application is worse than surfacing it as an error.

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

Job names are global within the PostgreSQL instance, so a generic name collides with jobs from
other services. Use a descriptive, service-scoped one: `refresh-active-keys`, not `refresh`.

### Materialized Views

When creating a materialized view that will ever be refreshed with `CONCURRENTLY`, a unique
index on the view is required by PostgreSQL:

```sql
CREATE MATERIALIZED VIEW active_keys AS (...);

CREATE UNIQUE INDEX active_keys_id_idx ON active_keys (id);
```

PostgreSQL does not inherit constraints or indexes from the source table into a materialized
view. Without the unique index, `REFRESH MATERIALIZED VIEW CONCURRENTLY` silently fails at
runtime (the scheduler's hourly job runs but does nothing). Add the unique index in the same
migration that creates the view, or in an immediate follow-up migration if the view already
exists.

### Migration Comments

Explain **why** — the SQL already says what it does. Comment on why the change was necessary or
why a particular approach was chosen:

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

Never use `varchar(n)`: PostgreSQL gains no performance from it over `text`, and length
constraints belong in the core layer unless they are a true database invariant.

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

Use `CURRENT_TIMESTAMP` for the current time in both queries and DDL. Never use `NOW()` — the
two are equivalent, but `CURRENT_TIMESTAMP` is the SQL standard form used throughout this
codebase:

```sql
WHERE
  expires_at > CURRENT_TIMESTAMP
```

---

## Common Pitfalls

The rules above, as a pre-push checklist:

- Modifying a committed up migration instead of writing a new one.
- Shipping an `.up.sql` with no paired `.down.sql`.
- Guessing or truncating a migration timestamp.
- Using `$1` where bun expects `?0`.
- Inlining DAO SQL in Go instead of `//go:embed`-ing a `.sql` file.
- Reading from `keys` instead of `active_keys`.
- Omitting `RETURNING *` on a mutating query whose row the caller needs.
- Omitting `--bun:split` between statements that must commit in order.
- Creating a materialized view without the unique index `REFRESH ... CONCURRENTLY` requires.
- Dropping objects out of reverse creation order in a down migration.
- Omitting `IF EXISTS` in a down migration.
- `varchar(n)` over `text`, bare `timestamp` over `timestamp(0) with time zone`, `NOW()` over
  `CURRENT_TIMESTAMP`.
- Generic pg_cron job names that collide across services.
