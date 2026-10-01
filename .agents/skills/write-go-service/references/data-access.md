# Data access and transactions

Read this reference when routed here by [write-go-service](../SKILL.md). Its rules apply to the selected work.

## DAO layer (`internal/dao/`)

Persist and retrieve data from PostgreSQL (and other external sources). No business logic, no
validation, no error translation beyond mapping database-level errors to domain sentinels.

```go
// pg.userSelect.go

//go:embed pg.userSelect.sql
var userSelectQuery string

// ErrUserSelectNotFound is returned when no user matches the requested ID.
var ErrUserSelectNotFound = errors.New("user not found")

// PgUserSelect retrieves a single active user by their ID.
type PgUserSelect struct{}

// UserSelectRequest holds the parameters for a [PgUserSelect.Exec] call.
type UserSelectRequest struct {
    ID uuid.UUID
}

func (r *PgUserSelect) Exec(ctx context.Context, request *UserSelectRequest) (*User, error) {
    ctx, span := otel.Tracer().Start(ctx, "dao.PgUserSelect")
    defer span.End()

    db, err := postgres.GetContext(ctx)
    if err != nil {
        return nil, otel.ReportError(span, fmt.Errorf("get postgres context: %w", err))
    }

    var user User
    if err = db.NewRaw(userSelectQuery, request.ID).Scan(ctx, &user); err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            err = errors.Join(err, ErrUserSelectNotFound)
        }
        return nil, otel.ReportError(span, fmt.Errorf("execute query: %w", err))
    }

    return otel.ReportSuccess(span, &user), nil
}

func NewPgUserSelect() *PgUserSelect { return &PgUserSelect{} }
```

- **SQL** lives in a companion `.sql` file embedded with `//go:embed` into a package-level variable
  — never an inline string literal. Use **`write-sql`** for all `.sql` work (parameterization,
  return patterns, formatting, the read-vs-write target rules).
- **Errors:** map a database error onto a domain sentinel by _joining_ it
  (`err = errors.Join(err, ErrXxxNotFound)`), then `otel.ReportError` it like any other failure —
  **including the not-found case**. A missing row is a real outcome the DAO encountered; whether
  it's benign is the caller's call (ultimately the handler's, by discarding it).
- **Telemetry:** `otel.ReportError(span, err)` on every failure path; `otel.ReportSuccess(span,
value)` on the happy path. See the Telemetry section.
- **Entity types** (bun models) go in their own `pg.<entity>.go` file, separate from the operations
  that use them.

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
