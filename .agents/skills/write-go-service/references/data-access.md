# Data access and transactions

Read this reference when routed here by [write-go-service](../SKILL.md). Its rules apply to the selected work.

Contents:

- [DAO layer (`internal/dao/`)](#dao-layer-internaldao)
- [Constraint violations](#constraint-violations)
- [Ownership](#ownership)
- [Transaction scoping](#transaction-scoping)

## DAO layer (`internal/dao/`)

Persist and retrieve data from PostgreSQL (and other external sources). No business logic, no
error translation beyond mapping database-level errors to domain sentinels.

The DAO and the schema keep the data intact. A constraint holds rows consistent with each other,
such as a cell whose scene and layer belong to its project, and the DAO maps its violation to a
sentinel (see [Constraint violations](#constraint-violations)). Data validation stays out: the
core's validator checks the request, and the core checks ownership (see [Ownership](#ownership)).

```go
// pg.userSelect.go

//go:embed pg.userSelect.sql
var userSelectQuery string

// ErrUserSelectNotFound is returned when no user matches the requested ID.
var ErrUserSelectNotFound = errors.New("user not found")

// UserSelectRequest holds the parameters for a [PgUserSelect.Exec] call.
type UserSelectRequest struct {
    ID uuid.UUID
}

// PgUserSelect retrieves a single active user by their ID.
type PgUserSelect struct{}

func NewPgUserSelect() *PgUserSelect {
    return &PgUserSelect{}
}

func (dao *PgUserSelect) Exec(ctx context.Context, request *UserSelectRequest) (*User, error) {
    ctx, span := otel.Tracer().Start(ctx, "dao.PgUserSelect")
    defer span.End()

    db, err := postgres.GetContext(ctx)
    if err != nil {
        return nil, otel.ReportError(span, fmt.Errorf("get postgres context: %w", err))
    }

    var user User

    err = db.NewRaw(userSelectQuery, request.ID).Scan(ctx, &user)
    if errors.Is(err, sql.ErrNoRows) {
        err = errors.Join(err, ErrUserSelectNotFound)
    }

    if err != nil {
        return nil, otel.ReportError(span, fmt.Errorf("execute query: %w", err))
    }

    return &user, nil
}
```

The file reads in one order: the embedded query, the sentinels, the request, the struct, its
constructor, then `Exec`. `Exec` opens its span, then repeats one shape: a call, then the block that
handles its error, at one indentation level. The handle from `postgres.GetContext` is `db`, since it
is a transaction only when a caller opened one.

- **SQL** lives in a companion `.sql` file embedded with `//go:embed` into a package-level variable
  — never an inline string literal. Use **`write-sql`** for all `.sql` work (parameterization,
  return patterns, formatting, the read-vs-write target rules).
- **Errors:** map a database error onto a domain sentinel by _joining_ it
  (`err = errors.Join(err, ErrXxxNotFound)`) in its own block right after the query, never inside
  the `if err != nil` that follows. Then `otel.ReportError` it like any other failure —
  **including the not-found case**. A missing row is a real outcome the DAO encountered; whether
  it's benign is the caller's call (ultimately the handler's, by discarding it).
- **Telemetry:** `otel.ReportError(span, err)` on every failure path; the happy path returns its
  value directly. See [Telemetry](telemetry.md).
- **Entity types** (bun models) go in their own `pg.<entity>.go` file, separate from the operations
  that use them.

### Constraint violations

A write that breaks a constraint fails with the constraint's SQLSTATE and name. The DAO matches
both and joins a sentinel, as it joins `ErrXxxNotFound` to `sql.ErrNoRows`. A foreign key
violation (`23503`) means the referenced row is missing, so it maps to a not-found sentinel. A check
violation (`23514`) maps to the sentinel of the limit it guards. The layer's `common.go` holds the
matcher:

```go
const (
    // pgForeignKeyViolation is the SQLSTATE PostgreSQL raises when a write breaks a foreign key.
    pgForeignKeyViolation = "23503"
    // pgCheckViolation is the SQLSTATE PostgreSQL raises when a write breaks a check constraint.
    pgCheckViolation = "23514"
)

// isViolation reports whether err comes from a write that broke the named constraint, which code
// classifies.
func isViolation(err error, code, constraint string) bool {
    var pgErr pgdriver.Error

    return errors.As(err, &pgErr) && pgErr.Field('C') == code && pgErr.Field('n') == constraint
}
```

A cell upsert maps each of its foreign keys to its own sentinel:

```go
err = db.NewRaw(cellUpsertQuery, request.ProjectID, request.SceneID, request.LayerID /* … */).Scan(ctx, entity)

switch {
case isViolation(err, pgForeignKeyViolation, "cells_scene_fkey"):
    err = errors.Join(err, ErrCellUpsertSceneNotFound)
case isViolation(err, pgForeignKeyViolation, "cells_layer_fkey"):
    err = errors.Join(err, ErrCellUpsertLayerNotFound)
}
```

Use **`write-sql`** for the constraints and their names.

### Ownership

The DAO takes no owner. Every by-ID query of a child matches its parent too, and its request
carries the parent ID:

```sql
SELECT
  *
FROM
  layers
WHERE
  project_id = ?0
  AND id = ?1;
```

A caller then reaches a child only through the parent it names, so one ownership check on that
parent covers the whole operation. A list by owner, such as a user's projects, still filters by
owner, since that filter is what the query asks for.

The ownership check is a DAO of its own, which answers in one query. Every core that needs it takes
it as a dependency:

```sql
-- pg.projectOwned.sql
SELECT
  1
FROM
  projects
WHERE
  id = ?0
  AND owner_id = ?1
LIMIT
  1;
```

```go
// PgProjectOwned reports whether a user owns a project. A missing project yields false with a nil
// error.
type PgProjectOwned struct{}

func (dao *PgProjectOwned) Exec(ctx context.Context, request *ProjectOwnedRequest) (bool, error) {
    // The span and the db handle open as in PgUserSelect.

    res, err := db.NewRaw(projectOwnedQuery, request.ID, request.OwnerID).Exec(ctx)
    if err != nil {
        return false, otel.ReportError(span, fmt.Errorf("execute query: %w", err))
    }

    n, err := res.RowsAffected()
    if err != nil {
        return false, otel.ReportError(span, fmt.Errorf("get rows affected: %w", err))
    }

    return n > 0, nil
}
```

The core runs it after validation and before any other DAO call. On false it returns not-found, so a
caller learns nothing about rows it cannot reach. Never read a whole parent row only to compare its
owner. A read that needs the row anyway, such as a project get, compares the owner inline.
Collaboration will widen who may act on a row, and that rule then changes in the ownership DAO
alone.

---

## Transaction scoping

`postgres.GetContext(ctx)` returns the current DB handle from the context — a `*bun.DB`
(auto-commit per statement) or a `bun.Tx` (open transaction). **DAOs never start transactions**;
they participate in whatever is already on the context.

**Who starts a transaction:**

- **Core** — when two or more DAO calls must succeed or fail together (one atomic unit of work).
- **`cmd/`** — for batch jobs (rotation, seeding) that should roll back wholesale on failure.
- **Handlers** — never. Transactions are a persistence concern, not a transport concern.

**Scope it right:** wrap exactly the statements that form one logical unit. Too small = splitting
an insert + its dependent update into two implicit transactions; too broad = spanning a tx across
an external HTTP/gRPC call, holding it open during long computation/file I/O, or wrapping two
unrelated operations together.

**Operations that cannot run inside a transaction** (e.g. `DB.Ping()` in a health handler) must
check the concrete type and bail out gracefully:

```go
pgdb, ok := pg.(*bun.DB)
if !ok {
    // Running inside a transaction (e.g. test isolation) — skip the ping.
    return nil
}
err = pgdb.Ping()
```
