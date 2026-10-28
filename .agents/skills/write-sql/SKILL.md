---
name: write-sql
description: >
  Write or review PostgreSQL queries, migrations, schema objects, and SQL embedded in Go. Load for
  any SQL work, including DAO files and internal/models/migrations.
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
in place. CI's `lint-js` job runs `prettier --check` and fails on the unformatted file even
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
tx.NewRaw(jwkSearchQuery, request.Usage, request.Limit).Scan(ctx, &entities)
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

### Bounded reads

Every query that can return more than one row ends with an explicit `LIMIT`. An unbounded query
loads the whole set into the service's memory. No read assumes one page holds a whole set.

A list returned to a client pages with `limit` and `offset`, in the set's natural order, such as
`position`. The same query returns the total as `count(*) OVER () AS total` on each row. Window
functions run before `LIMIT` and `OFFSET`, so the total counts every match, and the client pages
from that one query. The core caps the limit. An empty page has no row to carry the total, so it
reports a total of 0.

```sql
SELECT
  *,
  count(*) OVER () AS total
FROM
  scenes
WHERE
  project_id = ?0
ORDER BY
  position
LIMIT
  ?1
OFFSET
  ?2;
```

`WHERE` runs before window functions, though, so a count over rows the query then filters out sits
in a subquery. `PgLayerKind` counts each kind's layers across the project, then keeps the layers a
delete names. Its `LIMIT` is the number of IDs, the most rows it can match:

```sql
SELECT
  *
FROM
  (
    SELECT
      id,
      kind,
      count(*) OVER (
        PARTITION BY
          kind
      )::integer AS count
    FROM
      layers
    WHERE
      project_id = ?0
  ) AS kinds
WHERE
  id = ANY (ARRAY[?1]::uuid[])
LIMIT
  ?2;
```

A schema cap bounds what a parent may hold. A check holds the position column to a range, and a
unique key on the parent and the position allows one row per position. The cap does not make the
set small enough to read at once, so its lists still page.

```sql
-- Migration: the range and the unique key cap a project at 500 scenes.
CREATE TABLE scenes (
  id uuid PRIMARY KEY NOT NULL DEFAULT uuidv7(),
  project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  position integer NOT NULL CONSTRAINT scenes_position_check CHECK (
    position >= 0
    AND position < 500
  ),
  -- A shift moves positions in one statement, so uniqueness is checked at commit.
  CONSTRAINT scenes_project_id_position_key UNIQUE (project_id, position)
  DEFERRABLE INITIALLY DEFERRED
);
```

A write that renumbers positions shifts a range in SQL instead of reading every row. The client
sends the target position, and the statement that shifts clamps it, so no count is read before the
write. `LEAST(?1, count(*))` turns a position past the end into an append. This insert shifts the
scenes from there on by the number of new ones and fills the gap in one statement, which the
deferred unique key allows until commit. `WITH ORDINALITY` numbers the new scenes from 1 in the
order the client sent them:

```sql
WITH
  start AS (
    SELECT
      LEAST(?1, count(*))::integer AS position
    FROM
      scenes
    WHERE
      project_id = ?0
  ),
  shifted AS (
    UPDATE scenes
    SET
      position = scenes.position + cardinality(ARRAY[?2]::uuid[])
    FROM
      start
    WHERE
      scenes.project_id = ?0
      AND scenes.position >= start.position
  )
INSERT INTO
  scenes (id, project_id, position, title)
SELECT
  data.id,
  ?0,
  start.position + (data.position - 1)::integer,
  data.title
FROM
  unnest(ARRAY[?2]::uuid[], ARRAY[?3]::text[]) WITH ORDINALITY AS data (id, title, position),
  start
RETURNING
  *;
```

A move clamps its target to `count(*) - 1`, the last place, and shifts the rows between the old and
the new position by one toward the old one.

A write past the range breaks the check. Its DAO joins that violation to a sentinel, such as
`ErrSceneInsertFull`, so the core can refuse the request.

A sweep repeats until a batch comes back short, and its `ORDER BY` puts the most urgent rows first.

Fetch related rows in the same query, with a join or `= ANY(?0)`, never one query per parent row.

An existence check needs a single row. It ends with `LIMIT 1`, and its caller tests for any row. A
test for exactly one row reads two unexpected matches as none.

### Batch writes

A create, a delete or a content write takes a list (see `write-go-service`'s
[operations](../write-go-service/references/operations.md#core-layer-internalcore)) and applies it
in one statement: one round trip, and all of it or none. Pass each column of the list as
`bun.List(values)` inside a typed array, `ARRAY[?1]::uuid[]`: bun expands the values into the array,
and the cast types it. The statement returns at most one row per item, so the list cap bounds it
and `RETURNING` takes no `LIMIT`.

An insert or an upsert zips its column arrays with `unnest`, which reads them side by side as rows:

```sql
INSERT INTO
  cells (project_id, scene_id, layer_id, content, updated_at)
SELECT
  ?0,
  data.scene_id,
  data.layer_id,
  data.content::jsonb,
  ?4
FROM
  unnest(ARRAY[?1]::uuid[], ARRAY[?2]::uuid[], ARRAY[?3]::text[]) AS data (scene_id, layer_id, content)
ON CONFLICT (scene_id, layer_id) DO UPDATE
SET
  content = EXCLUDED.content,
  updated_at = EXCLUDED.updated_at
RETURNING
  *;
```

A delete matches its IDs with `= ANY` and closes every gap it leaves in the same statement: each
later row moves up by the number of deleted rows before it. The parts of one `WITH` share a
snapshot, so the `UPDATE` still sees the rows the `DELETE` removes and skips them by ID. An ID the
parent does not hold deletes nothing and raises no error, which is why the core counts the rows
returned.

```sql
WITH
  deleted AS (
    DELETE FROM scenes
    WHERE
      project_id = ?0
      AND id = ANY (ARRAY[?1]::uuid[])
    RETURNING
      *
  ),
  shifted AS (
    UPDATE scenes
    SET
      position = scenes.position - (
        SELECT
          count(*)
        FROM
          deleted
        WHERE
          deleted.position < scenes.position
      )::integer
    WHERE
      scenes.project_id = ?0
      AND scenes.id <> ALL (ARRAY[?1]::uuid[])
      AND scenes.position > (
        SELECT
          min(position)
        FROM
          deleted
      )
  )
SELECT
  *
FROM
  deleted;
```

### Read vs Write Targets

This service maintains two objects for the `keys` entity:

- **`keys`** — the base table. All writes (INSERT, UPDATE) target this table directly.
- **`active_keys`** — a plain view that exposes only non-expired, non-deleted rows, evaluated
  per query. All reads (SELECT) target this view.

Never read from `keys` directly in a DAO query — `active_keys` enforces the expiry and
soft-delete rules automatically. Never write to `active_keys`.

Expired and revoked keys stay in `keys` for good, reachable only by manual maintenance queries.
Since the view filters them at read time, the index under it leads with the lookup key, then the
expiry the view filters on. `keys (usage, expires_at)` lets a search range-scan the live keys of its
usage instead of every key the usage ever had.

---

## Schema Migrations (`internal/models/migrations/*.sql`)

Migrations are the authoritative history of the database schema. Every schema change must
go through a migration — no out-of-band DDL.

### File Naming

```
YYYYMMDDHHMMSS_<description>.<up|down>.sql
```

The timestamp is the current local wall-clock time **down to the second**: run
`date '+%Y%m%d%H%M%S'` immediately before creating the files. Never truncate to minutes, guess,
or reuse an existing timestamp. Never use `date -u`: existing migrations carry local time, so a UTC
stamp can sort before one already merged.

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

The template's CI follows suit. Its `main.yaml` skips `check-migration-snapshots` and buf's
breaking check when `github.event.repository.is_template` is true, so an in-place edit needs no
override label. Regenerate the schema snapshot with `GOLIB_UPDATE_SNAPSHOTS=1`. A fork is no
template repository, so both guards run there.

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

Example — down for a migration that created a materialized view with an index:

```sql
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

A job's command calls a function the same migration defines, so a later release changes the job by
replacing the function, the schema census snapshots it, and a test calls it. Schedule it only where
pg_cron is installed: the databases `postgrestest.RunDBTest` clones have no `cron` schema, so a bare
`cron.schedule` fails every data-access test. PL/pgSQL resolves `cron.*` only when the guarded
statement runs, and scheduling an existing name replaces its job. The paired down migration
unschedules it by the same name, the same way:

```sql
-- up
DO $$
BEGIN
  IF EXISTS (SELECT FROM pg_extension WHERE extname = 'pg_cron') THEN
    PERFORM cron.schedule(
      'service-genai-purge-settled',
      '* * * * *',
      format('SELECT %I.purge_settled_generations()', current_schema())
    );
  END IF;
END
$$;

-- down
DO $$
BEGIN
  IF EXISTS (SELECT FROM pg_extension WHERE extname = 'pg_cron') THEN
    PERFORM cron.unschedule(jobid) FROM cron.job WHERE jobname = 'service-genai-purge-settled';
  END IF;
END
$$;
```

Job names are global within the PostgreSQL instance, so a generic name collides with jobs from
other services. Use a descriptive, service-scoped one: `service-genai-purge-settled`, not `purge`.
The database image creates the extension in its init SQL, which runs before the first migration.

### Migrations on live tables

A migration runs while the previous release still serves and long requests are in flight. The
migrations job gives up a lock wait after 3 seconds and retries the file, and a server whose sessions
set a `lock_timeout` fails a write that waits longer instead of delaying it. Keep any lock that blocks
writes on a populated table under a second or two:

- **Build an index concurrently, alone in its file.** A file runs as one implicit transaction, which
  `CREATE INDEX CONCURRENTLY` refuses. A concurrent build that fails leaves an invalid index behind
  and the runner reruns the file, so drop it first, in its own split section:

  ```sql
  DROP INDEX CONCURRENTLY IF EXISTS generations_owner_idx;

  --bun:split
  CREATE INDEX CONCURRENTLY generations_owner_idx ON generations (owner_id);
  ```

  Its down migration is `DROP INDEX CONCURRENTLY IF EXISTS generations_owner_idx;`.

- **Add a constraint `NOT VALID`, then validate it in a later file.** Adding a `CHECK` or a foreign
  key scans the table under a lock that blocks writes; `VALIDATE CONSTRAINT` scans under one that
  lets them through.
- **Add a column without rewriting the table.** A nullable column, or one with a constant default,
  changes only the catalog. A volatile default or a type change rewrites every row under an exclusive
  lock.
- **Backfill in batches, outside the migration.** One `UPDATE` over a table holds every row lock it
  takes until it commits.

A table that stays small, such as one purged within hours, can take a plain statement: judge by the
size it reaches in production, not by its size today.

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

Generate UUIDs with `uuid.NewV7`, and default the column to `uuidv7()`. A UUIDv7 starts with its
creation time, so inserts land at the end of the primary key index. A UUIDv7 reveals when its row
was created; keep `uuid.New` for an ID whose creation time must stay private.

### Integrity Constraints

The schema keeps rows consistent with each other. The request's own rules, such as a length or an
owner, stay with the service's validator and ownership check.

A row that references siblings under one parent stores the parent's ID and reaches each sibling
through a composite foreign key. Each referenced table holds a `UNIQUE (parent_id, id)` key for it:

```sql
CREATE TABLE layers (
  id uuid PRIMARY KEY NOT NULL DEFAULT uuidv7(),
  project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  -- …
  CONSTRAINT layers_project_id_id_key UNIQUE (project_id, id)
);

-- scenes holds the same key, scenes_project_id_id_key.
CREATE TABLE cells (
  project_id uuid NOT NULL,
  scene_id uuid NOT NULL,
  layer_id uuid NOT NULL,
  -- …
  PRIMARY KEY (scene_id, layer_id),
  -- Both keys carry the project, so a cell's scene and layer exist and share its project.
  CONSTRAINT cells_scene_fkey FOREIGN KEY (project_id, scene_id) REFERENCES scenes (project_id, id) ON DELETE CASCADE,
  CONSTRAINT cells_layer_fkey FOREIGN KEY (project_id, layer_id) REFERENCES layers (project_id, id) ON DELETE CASCADE
);
```

A write to `cells` then proves its scene and its layer exist in its project, with no read before
it. Name every constraint whose violation the DAO maps to a sentinel, since the DAO matches the
name. A list of a project's cells then filters on `project_id` alone.

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
- A multi-row query without a `LIMIT`, or one query per parent row.
- A list that reads a whole set at once, even one the schema caps, or a client list whose total
  comes from a separate count query.
- A renumbering that reads every row instead of shifting a range in SQL.
- A count read before a write to clamp a position, which `LEAST` does in the shifting statement.
- A read before a write to prove a referenced row exists in the same parent, which a composite
  foreign key proves in the write itself.
- Omitting `--bun:split` between statements that must commit in order.
- Creating a materialized view without the unique index `REFRESH ... CONCURRENTLY` requires.
- Dropping objects out of reverse creation order in a down migration.
- Omitting `IF EXISTS` in a down migration.
- `varchar(n)` over `text`, bare `timestamp` over `timestamp(0) with time zone`, `NOW()` over
  `CURRENT_TIMESTAMP`.
- Generic pg_cron job names that collide across services.
- A bare `cron.schedule` in a migration, which fails in the test clones that lack pg_cron.
- A plain `CREATE INDEX`, a constraint added without `NOT VALID`, or a table rewrite on a populated
  live table.
