# BinaSiswa Backend

Go API for BinaSiswa, built with Gin and `database/sql` over MariaDB. The current API includes health/readiness, authentication, published catalogs, student learning, and assessment attempts/results. It does not create or migrate database tables.

## Requirements

- Go 1.25.5 or newer
- MariaDB is optional for starting the API and required for `/ready` to return ready

## Run locally

From the `backend` directory, the application loads `.env` automatically when `APP_ENV` is `development` (the default). Existing process environment variables always take precedence, including variables explicitly set to an empty value. `.env` is not loaded in non-development environments; configure those values through the runtime environment instead.

To run without MariaDB, leave the `DB_HOST`, `DB_USER`, and `DB_NAME` values unset or empty:

```powershell
$env:APP_ADDR = ":8080"
go run ./cmd/api
```

In another terminal:

```powershell
Invoke-RestMethod http://localhost:8080/health
Invoke-RestMethod http://localhost:8080/ready
```

`/health` reports whether the process is serving requests. `/ready` returns HTTP 503 with `database: not_configured` when database settings are absent.

To enable the MariaDB readiness check, configure `DB_HOST`, `DB_USER`, and `DB_NAME` (and `DB_PASSWORD` if needed). The connection uses `utf8mb4` and UTC. Startup does not require MariaDB to be reachable; `/ready` reports HTTP 503 until a ping succeeds.

`CORS_ALLOWED_ORIGINS` is a comma-separated list of exact origins, for example `http://localhost:5173`. Wildcard origins are not supported.

## Configuration

| Variable | Default | Description |
|---|---:|---|
| `APP_ADDR` | `:8080` | HTTP listen address |
| `APP_ENV` | `development` | Environment label written to logs |
| `APP_REQUEST_TIMEOUT` | `30s` | Maximum request execution time |
| `APP_SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown deadline |
| `CORS_ALLOWED_ORIGINS` | empty | Comma-separated exact browser origins |
| `DB_HOST` | empty | MariaDB host; database is disabled if all required DB settings are empty |
| `DB_PORT` | `3306` | MariaDB TCP port |
| `DB_USER` | empty | MariaDB username |
| `DB_PASSWORD` | empty | MariaDB password; keep it outside source control |
| `DB_NAME` | empty | MariaDB database name |
| `DB_MAX_OPEN_CONNS` | `25` | Maximum open database connections |
| `DB_MAX_IDLE_CONNS` | `5` | Maximum idle database connections |
| `DB_CONN_MAX_LIFETIME` | `5m` | Maximum lifetime of a pooled connection |
| `DB_CONN_MAX_IDLE_TIME` | `1m` | Maximum idle time of a pooled connection |
| `DB_READY_TIMEOUT` | `2s` | Timeout for the MariaDB readiness ping |
| `AUTH_ACCESS_TOKEN_SECRET` | generated in development | HMAC secret; at least 32 bytes and required outside development |
| `AUTH_ACCESS_TOKEN_TTL` | `15m` | Access-token lifetime |
| `AUTH_REFRESH_TOKEN_TTL` | `720h` | Refresh-session lifetime; must exceed access-token lifetime |

Database settings are optional for process startup. If any of `DB_HOST`, `DB_USER`, or `DB_NAME` is provided, all three are required.

## Routes

- `GET /health` — process health; does not contact MariaDB.
- `GET /ready` — readiness; returns HTTP 200 only when MariaDB is configured and reachable.
- `POST /api/v1/auth/register` — create a student account and session.
- `POST /api/v1/auth/login` — authenticate and issue an access token.
- `POST /api/v1/auth/refresh` — rotate the refresh session and issue a new access token.
- `POST /api/v1/auth/logout` — revoke the refresh session.
- `GET /api/v1/me` — current authenticated account and student profile.
- `GET /api/v1/materials` — published material catalog.
- `GET /api/v1/materials/{slug}` — published material detail; premium details require a current grant or admin account.
- `GET /api/v1/assessment-packages` — published package metadata, without question or answer content.
- `GET /api/v1/me/overview` — authenticated student profile and progress summary for started, accessible materials.
- `GET /api/v1/me/learning/materials/{slug}` — authenticated material syllabus with the current student's progress; premium access requires an active grant.
- `PUT /api/v1/me/lessons/{lessonId}/progress` — persist lesson position and completion for the authenticated student.
- `GET /api/v1/me/lessons/{lessonId}/notes` — list the authenticated student's notes for an accessible lesson.
- `POST /api/v1/me/lessons/{lessonId}/notes` — create a timestamped note for an accessible lesson.
- `DELETE /api/v1/me/lessons/{lessonId}/notes/{noteId}` — delete only a note owned by the authenticated student.
- `POST /api/v1/attempts` — start or resume an active attempt for a published package; the server sets its deadline.
- `GET /api/v1/attempts/{attemptId}` — resume an owned attempt and retrieve its questions, options, saved answers, and remaining time without answer keys.
- `PUT /api/v1/attempts/{attemptId}/answers/{questionId}` — save or clear one answer and its flagged state; send `selectedOptionKey` as an option key or an empty string to clear it.
- `POST /api/v1/attempts/{attemptId}/submit` — finalize and score an attempt; repeated submissions return the same final result.
- `GET /api/v1/attempts/{attemptId}/result` — retrieve the finalized score, correct answers, and explanations.
- `GET /api/v1/me/attempts` — list the authenticated student's attempts (`page`, `pageSize`).

JSON responses use a `data` envelope; errors use `{"error":{"code":"...","message":"..."}}`. List endpoints return `items`, `pagination` (`page`, `pageSize`, `total`), and—on materials—database-derived `filters`. Both English query names (`educationLevel`, `accessTier`, `sort`, `pageSize`) and the existing catalog names (`jenjang`, `akses`, `urutan`, `batas`) are accepted. Search is `q`; filters also include `grade`, `subject`, and, for packages, `kind` and `difficulty`. Page size is limited to 100. Material sorting supports `relevan`, `terbaru`, `populer` (distinct learners with recorded progress), and `latihan` (published practice questions).

Refresh tokens are random, stored only as SHA-256 hashes in `auth_sessions`, and returned solely in an HttpOnly cookie. Access tokens are short-lived HS256 bearer tokens. In development, an access-token secret is generated at startup if none is configured; tokens issued before a restart then become invalid. Configure a stable secret through the environment in production. Cookies are Secure outside development, HttpOnly, SameSite=Strict, and scoped to `/api/v1/auth`; deployments should keep the frontend and API on the same site and configure exact `CORS_ALLOWED_ORIGINS` values.

No schema, tables, or migrations are created by this application. The database must already contain the expected schema.

## Database conventions

Database access uses context-aware `database/sql` methods and MariaDB `?` placeholders; user-controlled values are never interpolated into SQL. Registration and refresh-session rotation use transactions. Assessment start, answer writes, expiry finalization, and submission use transactions and lock the owned attempt row. The server/database clock determines deadlines; expired attempts are finalized when accessed or during history retrieval. Assessment scores are calculated server-side from per-question weights, and keys/explanations are only selected for finalization and finalized results. Since no pass threshold is configured, `passed` remains `NULL` rather than inferring a pass/fail decision. Timestamps are handled in UTC. Catalog queries only return published rows and never select question answer keys.

## Code structure

The existing authentication and catalog modules use a Model–Handler–Service–Repository split:

- `internal/auth` and `internal/catalog` each keep entities, request/response DTOs, and domain errors in `model.go`.
- `internal/learning` follows the same layers for student overview, material progress, lesson progress, and notes.
- `internal/assessment` follows the same layers for attempt lifecycle, saved answers, server-side scoring, results, and history.
- `handler.go` handles Gin requests, validates HTTP input, and writes API responses.
- `service.go` coordinates business rules and authentication/catalog/learning/assessment operations.
- `repository.go` owns MariaDB queries through `database/sql`.
- `auth/password.go` and `auth/token.go` contain authentication-specific cryptographic utilities.
- `internal/platform/http` wires repositories, services, and handlers through constructors.
