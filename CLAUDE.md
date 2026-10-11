# tf-automation

Self-service web portal: users pick Terraform modules from a catalog, fill out generated forms (including multiple instances of the same module), preview generated code, and save it. Output lands as `.tf`/`.tfvars` under `customer-name/project-name/env-name/` in a GitHub output repo, ready for a human or automation to pull and run Terraform against later.

Full plan, phase-by-phase detail, data model, diagrams, and workstream breakdown: **[plan/ARCHITECTURE.md](plan/ARCHITECTURE.md)** — read that before starting work on any workstream. Phase completion status: **[plan/PROGRESS.md](plan/PROGRESS.md)**. How to build, run, and test it locally: **[docs/RUNNING_LOCALLY.md](docs/RUNNING_LOCALLY.md)**.

## Locked Decisions

| Area | Decision |
|---|---|
| Backend language | Go, using `github.com/hashicorp/hcl/v2` (`hclparse` + `hclwrite`) for all HCL parsing/generation — guarantees syntactic validity and closes string-escaping injection risk |
| Frontend | React + TypeScript, schema-driven forms |
| Database | PostgreSQL |
| VCS | GitHub, for both the modules (input) repo and the generated-code (output) repo |
| GitHub credentials | GitHub App (free to register; higher rate limits and finer-grained permissions than a PAT) |
| Record model | One record = one `customer/project/env` combination = one future Terraform state, containing all its selected module instances |
| Multi-instance codegen | Single module block per module type, `for_each` over a map/list variable — not repeated blocks |
| tfvars structure | **Per-service**: one `<module_name>.auto.tfvars` file per selected module within a record — Terraform auto-loads these, no `-var-file` chaining needed |
| Module discovery | Hybrid: variable schema auto-extracted from `variables.tf`; UI metadata from a required `metadata.yaml` per module |
| Module versioning | **By git tag**, `<module-name>/vX.Y.Z` (monorepo, path-scoped). `<module-name>` is the folder path under `modules/`, nesting allowed (`compute/instance/v1.0.0`); the `modules/` prefix is optional in the tag (`modules/compute/instance/v1.0.0` is the same module). Untagged modules are excluded from the catalog. Each instance pins the exact tag used at submission time |
| Catalog refresh | Webhook (push + tag-create) + periodic reconciliation fallback + **admin-only manual refresh** (gated by `users.is_admin` in Phase 1, superseded by Phase 3 roles) |
| Naming convention | Org-wide, opt-in per module via `metadata.yaml`'s `naming_resource_type`. Admin-managed DB-backed templates (tokens: `business_short_code`, `env`, `project`, `user_label`, `num_id`). Computed name is an **editable default**, not locked. `business_short_code` lives in a `customers` registry (once per customer). `num_id` auto-increments per `(business_short_code, env, resource_type)`, atomically allocated at save time, rejects past its configured width |
| Progress tracking | `plan/PROGRESS.md` updated at the end of every phase (and after each Phase 1 workstream) |
| Execution order | Building **Phase 0 → 1 → 3 now; Phase 2 (execution/apply) on hold** until there's a concrete need to run plan/apply |
| Preview UX | Generated code (and diff, on edits) shown before final save; preview and save share one code path, guaranteeing byte-parity |
| Sensitive variables | Included in generated tfvars for Phase 1, with a clear UI warning at preview time. Exclusion/secrets-manager handling deferred to Phase 2 |
| State backend | Self-hosted GCS/S3 (not Terraform Cloud), selected via a `STATE_BACKEND` config toggle |
| Record deletion | Soft-delete in DB only — output repo files are **never** auto-removed |
| Auth (Phase 1) | Simple built-in username/password + lightweight `is_admin` flag; full SSO/RBAC in Phase 3 |
| Execution scope (Phase 1) | Generate + store only. No `terraform plan`/`apply` runs in this phase — that's Phase 2 |
| Planning docs location | This file stays at project root (Claude Code auto-loads it from here only); detailed plans live in `plan/` |

## Phase Overview (build order)

| Phase | Goal | Status |
|---|---|---|
| 0 — Bootstrap & Shared Contracts | Repo scaffold + frozen `VarSchema`/OpenAPI contracts so Phase 1 workstreams can run in parallel | Building now |
| 1 — Code Generation & Storage (MVP) | Users generate correct, storable Terraform code via a self-service UI | Building now |
| 3 — Access Control & Governance | SSO, roles, audit log — depends only on Phase 1, not Phase 2 | Building now |
| 2 — Execution & Approval | Trigger `terraform plan`/`apply` with a human approval gate | On hold |
| 4 — Operational Maturity | Drift detection, cost estimation, policy-as-code | Not started |

Phase 1 is built as 9 parallel workstreams (WS-A through WS-I, including the Naming Convention Engine) converging in a Consolidation step — see `plan/ARCHITECTURE.md` for the full breakdown, data model, diagrams, and edge-case tables.
