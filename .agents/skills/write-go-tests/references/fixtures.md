# Test fixtures and static data

Read this reference when routed here by [write-go-tests](../SKILL.md). Its rules apply to the selected work.

## Cross-Package Test Fixtures

Some fixtures are shared across packages — a Postgres preset reused by both `dao_test` and
`handlers_test`, say. Go's `_test.go` rule is per-package (package X's `_test.go` cannot be
imported from package Y's), so a shared fixture has to live in a regular `.go` file, which is
compiled into production binaries.

**Always isolate cross-package fixtures into a dedicated subpackage.** Name the directory and
package after the layer plus the suffix `test`, mirroring Go stdlib conventions like
`net/http/httptest` and `testing/iotest`:

| Layer     | Subpackage path               | Package name |
| --------- | ----------------------------- | ------------ |
| `config/` | `internal/config/configtest/` | `configtest` |
| `lib/`    | `internal/lib/libtest/`       | `libtest`    |
| `core/`   | `internal/core/coretest/`     | `coretest`   |

```go
// internal/config/configtest/postgres.go
package configtest

// PostgresPreset is the PostgreSQL configuration used in integration tests.
var PostgresPreset = postgrespresets.NewDefault(pgdriver.WithDSN(env.PostgresDsn))
```

Test files import it as `configtest`:

```go
import (
    "github.com/a-novel/service-json-keys/v2/internal/config/configtest"
)

postgres.NewContext(ctx, configtest.PostgresPreset)
```

**Never:**

- Define test fixtures in the production package (e.g., `internal/config/postgres.config.go`)
  guarded only by a `Test` prefix on the variable. The variable is exported and compiled in, and a
  future change can wire it into a production code path without a single review flag.
- Use `.test.go` (with a dot) as a substitute for `_test.go` — the Go toolchain does not recognize
  the dot, so the file is compiled into the production binary.
- Reuse the bare name `testutils` for several fixture subpackages in one project. Two imports of
  `testutils` from different paths force aliasing at every call site. Use the layer-prefixed name
  (`configtest`, `libtest`) so each fixture subpackage has a unique, descriptive name.

---

## Static and large test data

Keep only short values inline when they make a test case easier to read. Put structured definitions
and large payloads under the package's `testdata/` directory, then embed them from an `_test.go` file
with `//go:embed`.

Prefer YAML (`.yaml`) for human-authored semantic fixtures. Convert it to the production format only at
the boundary the test exercises. Keep JSON when its exact representation is part of the behavior:
parser or encoder cases, exact wire bytes, malformed JSON, and byte-size boundaries. A production JSON
asset, including a JSON Schema document, keeps its native format when a test embeds it.

Reuse the repository's YAML parser. If none exists, apply `choose-dependency`; this preference does not
waive approval for a new package.

Reuse existing fixture and mock data before adding another definition. Keep one canonical large value
and derive small case-specific variants from it. When multiple packages need the same data, let the
dedicated `*test` fixture subpackage own and expose it instead of copying it into several `testdata/`
directories.
