# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

A toy multi-tenant backend built on three of the author's own libraries:
[rr](https://github.com/sirkostya009/rr) (codegen HTTP router), [ggen](https://github.com/sirkostya009/ggen)
(codegen JSON with decode-time validation) and barm (generic query builder), over postgres 18.

## Commands

```sh
go build ./...
go test -tags testhash ./...               # needs postgres, see below
go test ./internal/auth                    # the real argon2id, which testhash swaps out
go test -tags testhash -run 'TestUsers_' ./internal/service
go vet ./... && go fix ./...
$(go env GOROOT)/bin/gofmt -l .            # NOT plain gofmt, see Toolchain
go generate ./...                          # after touching //ggen: or //rr: directives

go run ./cmd/migrate shared|tenants|tenant <slug>|all|status
go run ./cmd/server

cd client/ts && pnpm install && pnpm check && pnpm test   # the typescript client
```

`go generate` runs ggen then rr, both as `go tool` dependencies, then
`cmd/clientgen` for both clients (from `internal/httpapi/api.go`). Generated files
(`*_gen.go`, `*_ggen.go`, `client/ts/client.ts`, `client/ts/types.ts`) are
committed and never edited by hand.

## Toolchain gotchas

- barm uses Go 1.27 generic methods. The `gofmt` and `gopls` on `$PATH` are older
  and reject them: use `$(go env GOROOT)/bin/gofmt`, and treat `gopls` type errors
  in files touching barm as false positives (`go vet` is authoritative).
- barm and rr are pinned to pushed commits: after changes are pushed, bump with
  `GOPROXY=direct go get github.com/sirkostya009/<barm|rr>@<commit>` (and `go generate`
  for rr).

## Architecture

Request path: `httpapi.Server` → generated router → controller → service → repository → barm → postgres.

**Public vs internal.** Only `domain/` and `client/go/` are importable by other
modules: the wire and row types, and the api client. Everything else lives under
`internal/`. Handlers take whatever rr binds by type, in any order:
`context.Context`, `http.ResponseWriter` (a handler that sets a header or cookie
takes it), `*http.Request`, path params, the body. Service
interfaces are deliberately not public: they describe the layer below the wire and
would drift from it.

**Wiring.** Each layer has its own `New`, none of which do I/O:
`httpapi.New(db, service.New(repository.New(db)), adminToken)`. Never construct a repository or
service outside its layer's `New`; `TestNew_LeavesNothingUnwired` in both layers
fails on a field left nil.

**Tenancy** is one postgres database with a `shared` schema plus a schema per tenant.
The tenant id is inside the caller's token (see Auth), `httpapi.Server.ServeHTTP` reads
it once and puts a `domain.TenantContext` (id + schema name) in the request context, and
repositories put a query in it with `Apply(tenantSchema(ctx).Q)`: a generic method value,
since a closure cannot be generic and `Apply` infers a method's type. The schema name
is a pure function of the id (`domain.TenantSchema`, base32hex of the uuid), so there
is no lookup, no cache and nothing to keep in sync. A tenant that does not exist shows
up as a missing relation and maps to 404. Never add a lookup on the request path.

**Auth** (`internal/auth`, `service.AuthService`, `service.APIKeyService`): users log in
with email + password (argon2id) and get a session in an `__Host-session` cookie, or
make api keys (bearer tokens) that act as them. Tokens are `gss_`/`gsk_` + base64url of
tenant id and 32 random bytes; the db keeps only their sha-256. A session only rides in
the cookie and a key only in `Authorization`, never the other way round. Only a session
may make or revoke keys. `ServeHTTP` refuses cross-origin writes
(`http.CrossOriginProtection`), then resolves the credential into `domain.WithTenant` +
`domain.WithPrincipal`. A credential that opens nothing leaves the request anonymous,
and the `requireTenant`/`requireAdmin` guards answer 401/403. `ADMIN_TOKEN` is the one
credential with no tenant: it alone reaches `/api/tenants`, and names a tenant with
`X-Tenant-ID`, which nobody else sends. Services enforce who may do what themselves, the
guards only turn away the anonymous.

**Migrations** live in `internal/migrations` with one directory per category (shared,
tenant) and are applied *only* by `cmd/migrate`. The backend checks the version and
refuses to work with a schema that is behind: the server will not start on an outdated
shared schema, and a registered-but-unmigrated tenant answers 404 until `migrate
tenants` has run. Never add migrate-on-open or auto-provisioning to server code.

**Transactions ride the context.** `repository.Txer.Begin(&ctx, nil)` opens one and
*replaces* the caller's ctx, so repository calls made with it run inside; a nested
Begin is a savepoint. Repositories pick their handle with `r.handle(ctx)`, which
returns the transaction from the context or the pool. Services drive it as plain
objects (`defer tx.Rollback()`, `return tx.Commit()`); barm returns `sql.ErrTxDone`
from a rollback after commit, which deferred rollbacks ignore.

**Point reads are cached** in process, by `repository.NewCached` only: `New` builds plain
repositories, which every test except the cache's own uses. The `cached*` repositories
(`repository/*_cached.go`, one beside each plain one) wrap the plain ones, so the interfaces carry no cache concerns;
a plain one exposes an unexported method when the cache needs more than the interface
returns (`remove` hands back the revoked token's hash). `cached(...)` serves a lookup by
key, and every write calls `wrote(ctx, table, invalidate)`. `cachedTxer` puts a `txState`
in ctx (shared by savepoints) recording which tables the transaction wrote: a clean
transaction reads the cache, a dirty table is read from the database and never filled
from. Invalidation runs as a barm `AfterCommit` hook (right away outside a transaction),
so a rolled back savepoint drops it. Fills carry a ticket taken
before the read (at `Begin` in a transaction) and are dropped when an invalidation came
after it. Keys carry the tenant. A write that cascades (deleting a user) marks and
invalidates every table it reaches. Lists are not cached. The cache is per process: other
instances only converge by TTL.

**Writes insert a sub-model.** `db.Insert[domain.CreateX]().Schema(s).Table("x").Values(&in).OneAs[domain.X](ctx)`
returns the full row, so services never map a `CreateX` to an `X` by hand. The table
comes from the *source* model, which is why the repository names it: a `CreateX`
carries column tags but no `BaseModel`.

**Lists are filtered with `QUERY`**, not query parameters: the filter is a JSON body
(`domain.UserFilter` and friends), `GET` stays as the unfiltered first page, and every
list is paged (`domain.DefaultLimit` 50, `MaxLimit` 200). Each filter has a `Q` method
(`domain/filter_query.go`) that a repository hands to barm's `Apply`:
`db.Select[domain.Post]().Apply(tenantSchema(ctx).Q, f.Q).Slice(ctx)`. In `Q` values always bind as
arguments and sort columns are looked up in a whitelist, falling back to `id` —
nothing from a filter may reach the query text. `likeContains` escapes `%`, `_` and `\`.
A slice binds as one Postgres array, so a list matches with `Where("id = ANY(?)", ids)`:
the SQL is the same for any number of values. Pass the typed slice (`[]domain.ID`),
never `[]any`. `barm.In` is for dialects without arrays, not this one.

**Imports stream into a batch.** A body declared `iter.Seq2[T, error]` is a
JSON array rr reads lazily (ggen's `Stream.Array`), so a handler taking one
gets no 400 from rr: the service reads it, and maps a decode error to
`domain.ErrMalformed`. Element types are `multierr`, so an element that only
broke rules arrives with all of its `ggen.Errors` and the array goes on.
`PostRepository.InsertEach` queues an insert per element on a barm batch and
runs it once the array ends: one round trip, one implicit transaction. A
query that fails ends a batch, so a per-element outcome must not be an error:
the insert is `INSERT ... SELECT` from the author row (`FOR KEY SHARE`), and a
missing author is no row rather than a foreign key violation.

**Ids** are `domain.ID`, a named type over stdlib `uuid.UUID`: 22-char unpadded strict
base64url on the wire, native `uuid` with `default uuidv7()` in postgres. The RFC form
is rejected on input. A new id field is `domain.ID`, never a bare uuid or string;
route params bind with `{id=@domain.ParseID}`.

**Errors** are the sentinels in `domain`, mapped to statuses in one place
(`internal/httpapi/errors.go`) and written as plain text via `http.Error`. Responses
are JSON through ggen.

**Clients** are written by `cmd/clientgen` from rr's `gen` model, one method per
route, grouped by controller. clientgen refuses what it cannot express
(query/header params, any-method routes, pointer results) rather than emit a wrong
method, and `TestRenderGo_MatchesCommitted` / `TestWriteTS_MatchesCommitted` fail on
a stale client. Each client's runtime is hand-written.

- `client/go` (`package client`): `client_gen.go` calls into `client.go`:
  credentials, encoding through ggen, and `client.Error`, whose `Is` maps a status
  back to the domain sentinel, so a mapping added to `errors.go` needs its mirror
  there. `TestClient_E2E` drives it against the real router and services.
- `client/ts`: `client.ts` (`createClient`) calls into `runtime.ts`, a fetch wrapper
  throwing `HttpError`. `types.ts` comes from `ggen/gen`: each type in the direction
  it travels, a body as the decoder takes it and a result as the encoder writes it
  (an `Input` twin only for a type that travels both ways). Typed only: nothing
  validates at runtime. `client.test.ts` runs on node with a fake fetch, and its
  `@ts-expect-error` lines pin the typing.

**Logging** goes through `internal/logger`: `LOG_FORMAT=dev` (default) writes one
short line per record to stdout, `json` uses the stdlib handler. Requests go through
`logger.Request(r, status, dur)`, which names the matched route rather than the path.

**Telemetry** (`internal/telemetry`) is OpenTelemetry for traces, metrics and logs,
and Sentry for issues only: never send Sentry spans or logs. Both are configured by
their standard `OTEL_*`/`SENTRY_*` env vars and stay off without them. Spans and
metrics are named after the matched route, never the raw path. Queries are traced by
a barm `QueryHook`, registered only when tracing is on, and named `{op} {table}` (or
their `Prepare` name), never with the schema: tenant schemas are unbounded. With
tracing on barm also records each call's caller (`code.*` attributes) and driver stats
(pool wait, describe round trip, backend pid). An issue is any
`slog.ErrorContext(ctx, ..., slog.Any("err", err))`, at any depth, with the trace of
the span in ctx. So log an error *or* return it, never both, or it is two issues.
`FailSpan` is for the request's outcome only (`handleError`).

**Stack traces** come from `internal/errs`: `errs.New`/`Errorf`/`Wrap` record where an
error was made, and Sentry reads it. `mapErr` wraps every unexpected driver error.
Domain sentinels are expected outcomes and never carry a stack.

## Domain types

One struct per row carries `barm:`, `json:` and ggen `pipe:` tags, so the db shape and
the wire shape cannot drift. Validation runs at decode time (`pipe:"required trim
minrunes=1"`); use `minrunes`/`maxrunes` for character limits and `minlen`/`maxlen`
only for real octet limits (an email address). Custom validators are plain funcs
referenced as `pipe:"@validSlug"`. After editing tags, run `go generate ./domain`.

## Tests

- **Name tests `TestSubject_Case_Detail`** — underscores separate logical pieces.
- `internal/httpapi` tests are wire tests: stub services, exact response bytes,
  headers and status codes, and what each handler bound and passed down.
- `internal/service` tests are integration tests. `pgtest.Run(m)` in `TestMain` creates
  one database per test binary and drops it after; `pgtest.DB(t)` hands each test a
  `*barm.DB` on one pgx transaction (a `barm.Pool` whose Begin is a savepoint), rolled
  back at the end. Tests skip when no postgres answers at
  `TEST_DATABASE_URL` (default `postgres://postgres@localhost:5432/postgres`).
- Two consequences of that single transaction: a statement postgres *rejects* aborts
  the rest of it, so a test expecting a rejection must end there (split the remainder
  into its own test); and `now()` is frozen at transaction start, so anchor time-range
  assertions to a stored timestamp, never `time.Now()`.
- The `testhash` build tag swaps argon2id for an unsalted sha-256
  (`internal/auth/password_testhash.go`), so tests that make users cost nothing to
  hash. Plain `go test` still passes, only slower. The real hash's tests are tagged
  `!testhash`, so run `go test ./internal/auth` without the tag. Never ship a
  binary built with it.
- `internal/repository` and `internal/migrations` take a database each
  (`pgtest.NewDatabase(t)`), because they migrate it.
