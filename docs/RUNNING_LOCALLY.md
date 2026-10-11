# Running & testing tf-automation

Two ways to run it:

- **Docker Compose** (§ 1): the quickest way to stand up the whole portal on a Linux host.
- **From source** (§ 2–6): for development; the backend and frontend run as local processes.

Either way you need a GitHub account with two repos (§ 3) and a token or GitHub App.
The full click-through test is § 7.

## 1. Docker Compose (Linux host)

Prerequisites: Docker Engine 24+ with the Compose plugin, and the two GitHub repos from § 3.

```bash
git clone <your-remote>/tf-automation.git && cd tf-automation
cp .env.example .env
#   Edit .env: GITHUB_TOKEN (or the GITHUB_APP_* trio), MODULES_REPO_*, OUTPUT_REPO_*,
#   POSTGRES_PASSWORD, JWT_SIGNING_SECRET (openssl rand -hex 32), STATE_BACKEND_BUCKET.
docker compose up -d --build
docker compose ps                    # migrate: exited (0); backend/frontend: healthy
docker compose run --rm --entrypoint seed-admin backend -username admin -password 'change-me-please'
```

Open `http://<host>:8080` (`PORTAL_PORT` in `.env`).

What runs:

| Service | Image | Notes |
|---|---|---|
| `postgres` | postgres:16-alpine | Data in the `pgdata` volume |
| `migrate` | backend image | One-shot `migrate up` over `backend/db/migrations`, then exits |
| `backend` | `backend/Dockerfile` | Go API; not published, reached through nginx. Health: `/api/healthz` |
| `frontend` | `frontend/Dockerfile` | nginx serving the React build and proxying `/api` to the backend |

**Session cookie and plain HTTP.** The session cookie is `Secure` by default, and browsers
only keep a `Secure` cookie over HTTPS or on `http://localhost`. So for a test host reached
as `http://192.168.x.x:8080`, either:
- set `COOKIE_SECURE=false` in `.env` and run `docker compose up -d backend` (test only); or
- browse through an SSH tunnel: `ssh -N -L 8080:localhost:8080 user@host`, then open `http://localhost:8080`; or
- put HTTPS in front (Caddy, nginx, a load balancer) and keep `COOKIE_SECURE=true`.

Without one of these, sign-in appears to succeed and the login page tells you the cookie
was dropped.

Useful commands:
```bash
docker compose logs -f backend
docker compose up -d --build backend frontend     # after pulling new code
docker compose run --rm migrate                   # re-apply migrations (no-op if current)
docker compose down                               # keeps the database volume
```

## 2. From source: prerequisites and build

- **Go 1.25+**: `go.mod` requires it (a test dependency needs 1.25). With an older Go, leave
  `GOTOOLCHAIN` at its default so Go downloads 1.25 itself.
- **Node 20+**
- **PostgreSQL 14+**: local, or `docker run --name tf-pg -e POSTGRES_PASSWORD=postgres -p 5432:5432 -d postgres:16`
- **golang-migrate**: `go install -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.1`

```bash
cd backend
go build ./... && go vet ./... && go test ./...
cd ../frontend
npm ci
npm run build && npm run lint && npm test
```

`go.sum` and `package-lock.json` are committed, so builds are reproducible. Don't run
`go mod tidy` unless you're changing dependencies.

## 3. Modules repo and output repo on GitHub

Create two repos (they can be private):

- **Modules repo** (catalog input). Push `C:\repos\tf-modules` as-is. It holds the
  portal-ready modules `modules/compute/instance` and `modules/iam/project-iam`.
  `C:\repos\terraform\modules` is a separate registry and stays untouched.
- **Output repo**, where generated code lands. It can start empty, but needs at least one
  commit (a README is enough) so it has a default branch.

Module conventions (details in `C:\repos\tf-modules\README.md`):

- **Location.** A module is any folder under `modules/` containing `variables.tf` and
  `metadata.yaml`, at any depth. Its name is the folder path, e.g. `compute/instance`.
- **Releasing.** Only tagged modules appear in the catalog. Tag with the module path, with
  or without the `modules/` prefix:
  ```bash
  git tag modules/compute/instance/v1.0.0
  git tag modules/iam/project-iam/v1.0.0
  git push origin main --tags
  ```
- **metadata.yaml `variables:`.** Gives each variable its form section (`group`), an
  `example` (the **e** hover), and optionally `advanced: true` (collapsed by default). The
  **i** hover shows the variable's `description` from `variables.tf`.
- **Name check.** `metadata.yaml` `name` must be the folder's leaf name (`instance`) or the
  full path.

Credentials: a classic PAT with `repo` scope covering both repos is the fastest start.
A GitHub App (the locked decision) is a drop-in swap via `GITHUB_APP_*`.

## 4. Postgres and migrations

```bash
createdb tf_automation      # or: docker exec -it tf-pg createdb -U postgres tf_automation
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/tf_automation?sslmode=disable"
migrate -database "$DATABASE_URL" -path backend/db/migrations up
```

Three migrations apply:

| Migration | Phase | What it adds |
|---|---|---|
| `000001` | 1 | Core schema |
| `000002` | 3 | Roles and audit log |
| `000003` | — | Customer tenancy: customer profile fields, a unique short name, `users.customer_id`, `invitations` |

Run the database-backed tests against it:
```bash
cd backend && TEST_DATABASE_URL="$DATABASE_URL" go test ./internal/data/...
```

## 5. Backend

```bash
cp .env.example .env        # fill in as for Docker; DATABASE_URL points at your Postgres
set -a; source .env; set +a
cd backend
go run ./cmd/seed-admin -username admin -password 'change-me-please'   # first platform admin
go run ./cmd/server
```

`seed-admin` creates a **platform admin**: the `admin` role and no customer. Platform admins
see every customer, invite customers, refresh the catalog, and read the audit log.
Everyone else joins through invitations (§ 7).

## 6. Frontend (dev server)

```bash
cd frontend
npm run dev        # http://localhost:5173, proxies /api to $TF_AUTOMATION_API_PROXY (default http://localhost:8080)
```

On a remote Linux box, tunnel the port (`ssh -N -L 5173:localhost:5173 user@host`) and
browse `http://localhost:5173`, or set `COOKIE_SECURE=false` and run `npx vite --host`.

## 7. Click-through test

1. **Platform admin.** Sign in as `admin`. The left panel shows *Resources, New resource,
   Module catalog, Customers, Audit log*.
2. **Catalog.** *Module catalog → Refresh catalog*. Compute Instance and Project IAM appear
   with cloud and version. If one is missing, the backend log names the module and the
   reason.
3. **Invite a customer.** *Customers → Invite customer*, enter an email, then *Create
   invitation*. Copy the link; it's shown only once.
4. **Accept.** In a private window, open the link, choose a username and password. You land
   on **onboarding**:
   - Enter *Doha Marine Sports Centre*. The short name pre-fills as `DMSC`.
   - Fill in contact and cloud defaults, then *Finish setup*.
   - Trying an existing short name shows "already taken" on the field.
5. **Customer home.** The header shows the organization and `DMSC`. *Resources* lists only
   this customer's requests; the left panel shows *Team* and *Organization*, not admin pages.
6. **New resource.**
   - Enter a project name and an environment.
   - Add *Compute Instance* from the dropdown; it shows name, cloud and version.
   - Each module gets its own colored pane, with instances as tabs. Add a second instance
     (or *Instance actions → Duplicate*).
   - Hover **i** next to a field for its description, type, default and rules, and **e**
     for an example. *Use this example* fills the field.
   - Add *Project IAM* as a second module in the same request.
7. **Preview and save.**
   - *Preview generated code* shows `main.tf`, `variables.tf`, `compute-instance.auto.tfvars`
     and `iam-project-iam.auto.tfvars`, plus `backend.tf` the first time.
   - *Create resource* commits to the output repo under `doha-marine-sports-centre/<project>/<env>/`.
   - Edit the resource: the preview opens on a *Changes* diff.
8. **Validate the Terraform.** Clone the output repo, `cd` into the folder, then
   `terraform init -backend=false && terraform validate`.
9. **Team.** *Team → Invite people* adds a Member. A member can request resources but
   can't invite people or edit the organization.
10. **Isolation.** Invite and onboard a second customer. It sees none of the first
    customer's resources, and opening a first-customer resource URL returns "not found".
11. **Audit log.** As `admin`, check *Audit log* for the onboarding, invitations and
    resource creates.

## 8. SSO (optional)

Register an OIDC client with redirect URI `http://<host>/api/auth/sso/callback` and set the
`OIDC_*` variables. A first-time SSO user has no customer and sees "No organization yet".
For now, customers join through invitations; linking SSO users to customers is future work.

## 9. Still unverified

- Real GitHub commits, conflict handling (`ErrOutputModifiedExternally`), and retry-sync
  against a live repo. Local testing used a dummy token, so commits ended in `sync_status=failed`.
- Concurrent saves racing on the same naming counter.
- The Docker images were written without a Docker daemon available. Each step they run
  (`go build`, `go install migrate`, `npm ci && npm run build`) was verified outside Docker;
  the image builds themselves were not.
