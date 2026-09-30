# DB Migrations

Tool: [golang-migrate](https://github.com/golang-migrate/migrate) (`golang-migrate/migrate/v4`), targeting PostgreSQL.

Naming convention: `NNNNNN_description.up.sql` / `NNNNNN_description.down.sql`, sequential, zero-padded to 6 digits.

WS-A (Data Layer) owns this directory's actual schema migrations, starting with `000001_init.up.sql` /
`000001_init.down.sql` covering `module_schemas`, `records`, `module_selections`, `users`, `customers`,
`naming_templates`, `naming_counters` — see `plan/ARCHITECTURE.md` § Phase 1 → Data model for the target schema.

Run migrations (once `migrate` CLI and a `DATABASE_URL` are available):
```
migrate -database "$DATABASE_URL" -path backend/db/migrations up
```
