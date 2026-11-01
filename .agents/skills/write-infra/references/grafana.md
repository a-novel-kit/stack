# Grafana coverage

Grafana watches every workload through the Grafana root, `environments/production/grafana`, and
reads a database's own numbers through the cluster root. A new service or platform brings its
coverage in the pull requests that deploy it. A release that changes what Grafana reads updates it
in the same train.

## What covers a workload by itself

Alloy gives each server span and root span an outcome, `a_novel.outcome` (fault, transient,
expected, ok), and derives span metrics from them. The Errors and Performance dashboards group those
metrics by `service_name`, so a workload joins them once it exports spans. Health reads its
namespace: pods, restarts, requests and budget.

The request span carries the useful split. golib's `httpf` and nodelib's SvelteKit hook name it after
the route and record the status code. A server that names its spans otherwise folds every route into
one line.

## Add with the workload

1. **Alert thresholds.** Add the repository to the Grafana root's `alerts` in `terraform.tfvars`.
   Set `failure_rate`, and a `p95_seconds` above the workload's measured p95, read on Performance.
   List in `latency_ignores` the spans whose duration says nothing of slowness, such as a stream.
   The entry creates the rule group: failing share, slow requests, faults and new fault.
2. **Database queries.** Each entry of the cluster root's `databases` needs `queries/<database>.yaml`
   and `queries/<database>.sql`, and the tests refuse a database without them. CloudNativePG runs the
   queries as `cnpg_metrics_exporter`, which reads no table of ours. The `.sql` file grants it the
   columns the queries read, and `database_grants` runs it as the tables' owner after each run of the
   migrations.
   - Count, never export. No email, owner, payload, key or token becomes a value or a label.
   - Run on the primary (`primary: true`, `target_databases: ["app"]`), and read through an index, a
     small table or a uuidv7 key range.
   - Return a row from an empty table: aggregate without `GROUP BY`, or `GROUP BY ROLLUP` with a
     `coalesce(<label>, 'all')`. An empty table then reads 0, and only a failing query reads as no
     data.
   - Grant columns, never tables: `GRANT SELECT (<columns>) ON <table> TO cnpg_metrics_exporter;`.
     A test pins that shape.
   - Prove each query as the exporter, on the service's migrations: migrations and grants as an
     owner role, then the query under `SET ROLE cnpg_metrics_exporter`, a member of `pg_monitor`. A
     superuser check hides a missing grant.
3. **Services tab.** Add a tab to `dashboards/services.json` with the numbers that prove the workload
   works: what it holds, read from its database, and what it does each hour and what it refuses,
   read from its traces. Each panel's description says what the panel shows and when to worry.
4. **Silent promises.** A promise that breaks without an error, such as a key's runway, a retention
   or a queue's oldest wait, gets a rule in `alerts.tf`'s `data_rules`, linked to its panel through
   `dashboard` and `panel`. Add its row and its bullet to infra's `docs/runbooks/alerts.md`.

## Keep it current

A release that renames a span, an attribute, a route, a table or a column updates the panels,
queries and grants that read it, in the same train:

- a stale TraceQL panel reads empty;
- a query on a renamed column fails, and `cnpg_errors_total{errorUserQueries}` names it;
- a grant on a dropped column fails `database_grants`, and with it the deploy.

## Dashboard traps

- **Transformations take the v2 shape,** with the transformer in `group`:
  `{kind: "Transformation", group: "merge", spec: {options: {}}}`. Grafana stores the v1 shape,
  `{id, options}`, as an empty transformation, and `dryRun=All` accepts it all the same. A test pins
  the v2 shape.
- **Tempo names a series after its labels,** as `{span.mail.kind="register", status="unset"}`, or
  after its function when it has none, and gives its frame that name as refId. An override by refId
  never matches. A series with one label reads as its value. Rename the others with `renameByRegex`,
  whose regex Grafana anchors at both ends: `^.*\(wait\)$`, never `\(wait\)$`. A quantile adds
  `p=0.95` to the name.
- **Two unlabeled queries share one name,** and a transformation's frame `filter` applies to every
  frame. Group one of them `by (name)` instead, so its series takes the span's name.
- **TraceQL reads 25 hours at most.** A trace panel pins its own range with `timeFrom: "24h"`.
- **Read back what Grafana stored.** Post the spec to
  `/apis/dashboard.grafana.app/v2/namespaces/<stack namespace>/dashboards?dryRun=All` and compare the
  returned spec with the source. After the deploy, read each panel's frames and display names live.
