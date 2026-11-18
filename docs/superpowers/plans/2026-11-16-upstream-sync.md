# Upstream Sync (dd56585) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Merge all upstream `Tencent/LoopForge` commits through pinned commit `dd56585` into the fork's `main`, preserving the fork's multi-executor requirement-clarification feature intact and keeping both histories.

**Architecture:** Add the local upstream clone as a git remote and perform a single `--no-ff` merge of `dd56585` on a dedicated sync branch. The bulk of the ~620 changed files merge cleanly; ~20 overlapping files are resolved file-by-file by combining both sides' semantic changes. A migration-number collision is fixed by renaming the fork's `029` to `033`. Validation gates (Go, web, local editions, public-content) run before the sync branch is fast-forwarded into `main`. No history rewrite, no force-push.

**Tech Stack:** Git, Go (stdlib testing, gorm), JavaScript (Node `--test`), Python validation scripts, Playwright acceptance.

**Spec:** `docs/superpowers/specs/2026-11-16-upstream-sync-design.md`

## Global Constraints

- Sync source is pinned to the local clone `/home/yong/work/opensoure/LoopForge` at `dd56585` (2026-11-15). Do NOT fetch from GitHub and do NOT let a moving `main` ref change scope.
- Merge base is `b751e09` (2026-11-13 13:00). Fork has 17 unique commits since it; upstream has 205.
- Commit hashes of both histories must be preserved. Use `git merge --no-ff`; never rebase, cherry-pick replay, force-push, or `git checkout upstream/main -- .` / `git checkout --theirs` on conflicting files.
- No `merge -X ours/theirs`: overlapping files need both sides' changes.
- Never copy private deployment files, `.env`, `.data/`, credentials, or staff data into the repo. `local/` stays a deployment-specific adapter and is not edited.
- Migration scripts are `NNN_description.sql`, strictly increasing, manually applied; code does not run migrations or seed.
- Do not edit generated Classic host bundles (`.cursor/`, `.claude/`) directly; after a Classic behavior change run `python3 local/scripts/build-classic-hosts.py --write`.

## Review Focus

These are the failure modes the spec implies but a per-file merge resolution can miss. Each is pinned to a task's verification step.

1. **Clarification feature silently dropped in an overlapping file** — e.g. taking upstream's `clarify_mcp.go` and losing the fork's per-engine command construction, or losing the web clarification picker. Expected: both upstream refactors and fork behavior coexist. Pinned in Task 4 (clarify tests) and Task 5 (web picker tests).
2. **Upstream fix/regression coverage lost by taking the fork's version wholesale** — e.g. dropping upstream `bda7b97`'s extracted read-instruction helper in `clarify_mcp.go`. Expected: upstream's new helpers/fixes are present. Pinned in Task 4 (`go test ./...` incl. upstream `clarify_mcp_read_instruction_test.go`).
3. **Fork-only new files deleted by a shortcut** — `clarify_engine.go`, the four clarify engine `*test.go`/picker files, `docs/proposals/clarify-multi-executor.md`, `docs/superpowers/plans/2026-11-12-clarify-multi-executor.md`. Expected: all still tracked after merge. Pinned in Task 7's presence check.
4. **Migration number collision** — two `029_*` scripts. Expected: exactly one `029`, fork's preference migration at `033`. Pinned in Task 6.
5. **Docker build-context regression** — losing "keep local state out of the docker build context" in `.dockerignore`/`Dockerfile`. Expected: `.data/`, `.env`, `local/` still excluded. Pinned in Task 5's `.dockerignore` check.

---

### Task 1: Prepare remote, branch, and pre-merge gate

**Files:** git config only (no working-tree files). Creates branch `sync/upstream-2026-11-16`.

**Interfaces:**
- Produces: a clean sync branch based on `main` at `99f97fe`, with remote `upstream` pointing at the local clone and its objects fetched.

- [ ] **Step 1: Confirm clean tree and base**

```bash
cd /home/yong/work/qxzg/LoopForge
git status --short
git rev-parse HEAD
```
Expected: only untracked `docs/superpowers/specs/` (the spec doc) shows; HEAD is `99f97fe9b8842b327e77375ce8c1a1a58566528f`.

- [ ] **Step 2: Create the sync branch**

```bash
git checkout -b sync/upstream-2026-11-16
```
Expected: `Switched to a new branch 'sync/upstream-2026-11-16'`.

- [ ] **Step 3: Add the local upstream remote and fetch**

```bash
git remote add upstream /home/yong/work/opensoure/LoopForge
git fetch upstream
```
Expected: fetch completes without error. If `upstream` remote already exists, use `git remote set-url upstream /home/yong/work/opensoure/LoopForge` instead of `add`.

- [ ] **Step 4: Pre-merge gate — assert the upstream tip is pinned**

```bash
git rev-parse upstream/main
```
Expected output exactly: `dd56585276d8ecb6e0d6865f55e255194d75f85a`. If it differs, STOP and re-scope the spec — the conflict set and migration ceiling in this plan are only valid for `dd56585`.

- [ ] **Step 5: Commit**

No code commit for this task; the branch and remote are local state. Proceed to Task 2.

---

### Task 2: Execute the merge and inventory conflicts

**Files:** working tree only (conflicted state).

**Interfaces:**
- Consumes: fetched `dd56585` from Task 1.
- Produces: a merge in progress with `Unmerged paths` captured into a conflict checklist; no merge commit yet.

- [ ] **Step 1: Start the no-ff merge**

```bash
git merge --no-ff dd56585 -m "merge: sync upstream through dd56585"
```
Expected: git reports `CONFLICT (content)` for a set of files and stops. A `Automatic merge failed` message is expected and is not an error at this step.

- [ ] **Step 2: Capture the conflict checklist**

```bash
git diff --name-only --diff-filter=U | sort
```
Expected: a list including (subset of) `CHANGELOG.md`, `Dockerfile`, `README.md`, `README.zh-CN.md`, `compose.yaml`, `design.md`, `docs/api/openapi.yaml`, `docs/deployment.md`, `internal/domain/user.go`, `internal/httpapi/clarify_engine_test.go`, `internal/httpapi/clarify_mcp.go`, `internal/httpapi/server.go`, `internal/httpapi/stream.go`, `internal/repo/models.go`, `internal/repo/users.go`, `scripts/test-runtime-image.sh`, `web/app/assistant-models.js`, `web/app/issue-drawer.js`, `web/app/sync-schedule.js`, `web/assistant_models_test.js`. Work through every listed file in Tasks 3–5; do not skip any.

- [ ] **Step 3: Note add/add and one-sided cases**

`internal/httpapi/clarify_engine_test.go` is an **add/add** conflict (absent at base `b751e09`, added by both sides with different content). Fork-only new files (`clarify_engine.go`, `clarify_engine_*_test.go`, `web/clarify_engine_picker_test.*`, the two docs) and upstream-only new files (e.g. `internal/httpapi/clarify_mcp_read_instruction_test.go`) will not be conflicted; they must simply survive (Task 7).

---

### Task 3: Resolve backend (non-clarify) conflicts

**Files (resolve):** `internal/domain/user.go`, `internal/httpapi/server.go`, `internal/httpapi/stream.go`, `internal/repo/models.go`, `internal/repo/users.go`, `CHANGELOG.md`, `README.md`, `README.zh-CN.md`, `design.md`, `docs/api/openapi.yaml`, `docs/deployment.md`, `scripts/test-runtime-image.sh`, `Dockerfile`, `compose.yaml`, `.dockerignore`, `.gitignore`.

**Interfaces:**
- Consumes: conflicted working tree from Task 2.
- Produces: these files conflict-free; no work done on `clarify_mcp.go`, `clarify_engine_test.go`, or web files (Tasks 4–5).

- [ ] **Step 1: Resolve Go backend files by combining both sides**

For each of `internal/domain/user.go`, `internal/httpapi/server.go`, `internal/httpapi/stream.go`, `internal/repo/models.go`, `internal/repo/users.go`: inspect `git diff` for each side (`git show :2:FILE` = fork/ours, `git show :3:FILE` = upstream/theirs) and produce a merged file that keeps upstream's new fields, refactors, and wiring **and** the fork's clarify additions (notably the `clarify_model` preference field/column in `models.go`/`users.go`, and clarify engine availability/session wiring in `server.go`/`stream.go`). Do not drop either side. Verify no duplicate gorm tags or duplicate struct fields.

- [ ] **Step 2: Resolve docs by rebasing fork's clarify docs onto upstream**

For `CHANGELOG.md`, `README.md`, `README.zh-CN.md`, `design.md`, `docs/api/openapi.yaml`, `docs/deployment.md`: take upstream's version as the base and re-apply the fork's clarification documentation (multi-executor spec, engine-selection and per-engine-availability descriptions, picker layout rules). For `openapi.yaml`, keep upstream's newly documented endpoints plus the fork's clarification fields.

- [ ] **Step 3: Resolve build config, preserving the docker-context fix**

For `Dockerfile`, `compose.yaml`, `scripts/test-runtime-image.sh`, `.dockerignore`, `.gitignore`: combine upstream changes with the fork's `build: keep local state out of the docker build context` behavior and harness bundling. Concretely, `.dockerignore` must still exclude `.data/`, `.env`, `local/`, and `.git/`.

- [ ] **Step 4: Stage the resolved files**

```bash
git add internal/domain/user.go internal/httpapi/server.go internal/httpapi/stream.go \
  internal/repo/models.go internal/repo/users.go \
  CHANGELOG.md README.md README.zh-CN.md design.md \
  docs/api/openapi.yaml docs/deployment.md scripts/test-runtime-image.sh \
  Dockerfile compose.yaml .dockerignore .gitignore
```

- [ ] **Step 5: Verify no conflict markers in staged files**

```bash
git grep -nE '^(<<<<<<<|=======|>>>>>>>)' -- '*.go' '*.md' '*.yaml' '*.sh' '.dockerignore' '.gitignore'
```
Expected: no output for these files (clarify/web files from Tasks 4–5 may still be conflicted).

---

### Task 4: Resolve clarify conflicts and prove both features survive

**Files (resolve):** `internal/httpapi/clarify_mcp.go`, `internal/httpapi/clarify_engine_test.go`.

**Interfaces:**
- Consumes: staged backend files from Task 3; upstream helper `bda7b97` (extracted TAPD/Gongfeng read instructions) present in upstream version of `clarify_mcp.go`.
- Produces: clarify files conflict-free with upstream refactors + fork multi-engine behavior; `go test ./...` green.

- [ ] **Step 1: Resolve `clarify_mcp.go` — keep upstream helper AND fork multi-engine logic**

Inspect both sides (`git show :2:internal/httpapi/clarify_mcp.go` and `:3:`). Produce a merged file that retains upstream `bda7b97`'s extracted read-instruction helper(s) (so upstream tests still pass) **and** the fork's per-engine command construction / availability gating / engine-prefixed model resolution. Do not simply pick one side.

- [ ] **Step 2: Resolve the add/add `clarify_engine_test.go`**

Both sides added this file. Produce a merged test file that includes upstream's clarify regressions and the fork's engine-selection cases (`codex:gpt-5.5`, unprefixed CodeBuddy, empty = no choice). Keep all test functions from both sides; remove duplicate helper names only if they collide.

- [ ] **Step 3: Stage and check markers**

```bash
git add internal/httpapi/clarify_mcp.go internal/httpapi/clarify_engine_test.go
git grep -nE '^(<<<<<<<|=======|>>>>>>>)' -- 'internal/httpapi/*.go'
```
Expected: no output.

- [ ] **Step 4: Verify Go compiles and all Go tests pass**

```bash
go test -p=2 ./... -count=1 -timeout=180s
go vet ./...
```
Expected: both pass. This exercises upstream's `clarify_mcp_read_instruction_test.go` and the fork's clarify engine tests together, pinning Review Focus items 1 and 2. If a test fails, the merge resolution is wrong — fix the resolution, not the test.

---

### Task 5: Resolve web conflicts and prove the picker survives

**Files (resolve):** `web/app/assistant-models.js`, `web/app/issue-drawer.js`, `web/app/sync-schedule.js`, `web/assistant_models_test.js`.

**Interfaces:**
- Consumes: staged Go files from Tasks 3–4.
- Produces: web files conflict-free with fork's clarification picker + upstream changes; web test suites green; `.dockerignore` check done.

- [ ] **Step 1: Resolve web JS files by combining both sides**

For each of the four files, inspect both sides and merge so the fork's clarification engine picker (grouped by engine, availability reasons, fixed-position settings picker) coexists with upstream's changes. Do not drop the picker UI or its wiring.

- [ ] **Step 2: Stage and check markers**

```bash
git add web/app/assistant-models.js web/app/issue-drawer.js web/app/sync-schedule.js web/assistant_models_test.js
git grep -nE '^(<<<<<<<|=======|>>>>>>>)' -- 'web/**'
```
Expected: no output.

- [ ] **Step 3: Run web unit tests (pins Review Focus item 1)**

```bash
cd webtest && npm test && cd ..
go test ./web/... -count=1
```
Expected: `webtest` `node --test` suite passes and Go web-package tests (`assistant_models_test.go`, `clarification_composer_test.go`, `clarify_engine_picker_test.go`, `syntax_test.go`) pass.

- [ ] **Step 4: Confirm docker build-context exclusions (pins Review Focus item 5)**

```bash
grep -nE '^(\.data/|\.env|local/|\.git/)$' .dockerignore
```
Expected: all four patterns present. If any is missing, re-apply the fork's `build: keep local state out of the docker build context` change before proceeding.

---

### Task 6: Fix the migration number collision

**Files:** rename `migrations/029_clarify_model_preference.sql` → `migrations/033_clarify_model_preference.sql`.

**Interfaces:**
- Consumes: the merged tree; upstream introduces `029_assistant_diagnostics.sql`, `030_generic_docker_shared.sql`, `031_generic_backend_openssh.sql`, `032_assistant_generation_workspace_lease.sql`.
- Produces: a single strictly-increasing migration sequence with the fork's preference migration at `033`.

- [ ] **Step 1: Rename the fork's migration**

```bash
git mv migrations/029_clarify_model_preference.sql migrations/033_clarify_model_preference.sql
```
Expected: rename staged. The file content (a standalone `ALTER TABLE user_configs ADD COLUMN clarify_model ...`) is unchanged and upstream does not reference it.

- [ ] **Step 2: Verify unique, increasing migration numbers (pins Review Focus item 4)**

```bash
ls migrations/*.sql | sed -E 's#.*/([0-9]+)_.*#\1#' | sort | uniq -d
```
Expected: no output (no duplicate numbers). Also confirm `033_clarify_model_preference.sql` exists and `029_assistant_diagnostics.sql` is the only `029_*`.

---

### Task 7: Commit the merge, then run full validation

**Files:** the whole merged tree.

**Interfaces:**
- Consumes: all conflicts resolved and staged from Tasks 3–6.
- Produces: a merge commit on `sync/upstream-2026-11-16`, fully validated.

- [ ] **Step 1: Assert zero unmerged paths and zero conflict markers**

```bash
git diff --name-only --diff-filter=U
git grep -nE '^(<<<<<<<|=======|>>>>>>>)' -- . ':!*.svg' ':!*.map'
```
Expected: no output from either command (the grep excludes vendored/generated assets). If any remain, resolve before committing.

- [ ] **Step 2: Verify fork-only files survived (pins Review Focus item 3)**

```bash
for f in internal/httpapi/clarify_engine.go \
  internal/httpapi/clarify_engine_availability_test.go \
  internal/httpapi/clarify_engine_command_test.go \
  internal/httpapi/clarify_engine_wiring_test.go \
  internal/httpapi/clarify_session.go \
  internal/httpapi/clarify_session_test.go \
  web/clarify_engine_picker_test.go web/clarify_engine_picker_test.js \
  docs/proposals/clarify-multi-executor.md \
  docs/superpowers/plans/2026-11-12-clarify-multi-executor.md \
  migrations/033_clarify_model_preference.sql; do
  test -e "$f" || echo "MISSING: $f"
done
```
Expected: no `MISSING` lines. If a file is missing, restore it from `main` before committing.

- [ ] **Step 3: Commit the merge**

```bash
git commit --no-edit
```
Expected: merge commit created (the message from Task 2 is reused). Do not create a single squashed "sync" replacement.

- [ ] **Step 4: Run full Go validation**

```bash
go test -p=2 ./... -count=1 -timeout=180s
go vet ./...
```
Expected: pass. This is the final whole-repository Go gate; upstream's full suite and the fork's clarify suite both run.

- [ ] **Step 5: Run local edition validation scripts**

From the repo root, run each and record pass/fail/blocked/not-run:

```bash
python3 local/scripts/build-classic-hosts.py --check
bash local/scripts/validate.sh
bash local/scripts/smoke-install.sh
bash local/scripts/e2e.sh
bash local/scripts/test-observability.sh
bash local/scripts/scan-secrets.sh
```
Expected: all pass. These are separate from platform code per `docs/upstream-sync.md`; report each check's status accurately. If a script is blocked by missing local environment, report it as blocked rather than silently skipping.

- [ ] **Step 6: Run public-content / secret scan**

```bash
python3 scripts/check-public.py
```
Expected: pass, no private credentials, corporate identifiers, or infrastructure leaked by the merge.

- [ ] **Step 7: Commit any validation-driven fixes**

If Tasks 7.4–7.6 required a fix, commit it as a separate `fix:` commit on the sync branch. If everything passed, no further commit.

---

### Task 8: Fast-forward main and hand off

**Files:** git branch state only.

**Interfaces:**
- Consumes: validated merge commit on `sync/upstream-2026-11-16`.
- Produces: `main` at the validated sync commit; working tree clean.

- [ ] **Step 1: Fast-forward main**

```bash
git checkout main
git merge --ff-only sync/upstream-2026-11-16
```
Expected: `main` advances to the sync commit. If fast-forward fails, stop — do not force; investigate divergence.

- [ ] **Step 2: Final state check**

```bash
git status --short
git log --oneline -3
git rev-list --count b751e09..HEAD
```
Expected: clean tree (spec/plan docs may show as untracked/committed per your choice), and HEAD includes the merge commit both histories. Report the final commit hash to the user. Do **not** push — leave that decision to the user.

- [ ] **Step 3: Report**

Summarize: merge commit hash, number of conflicts resolved, migration rename, validation results (passed/failed/blocked/not-run), and that no push was performed.

---

## Self-Review

- **Spec coverage:** Setup/gate (T1), merge+inventory (T2), backend/docs/build resolution (T3), clarify resolution + dual-feature test (T4), web resolution + picker test (T5), migration collision (T6), commit + full validation (T7), final merge/handoff (T8). All spec sections and decided items are covered.
- **Step scan:** Each step is a single checkable action (one command with explicit expected output, or one file's merge intent). No "handle edge cases" filler.
- **Type consistency:** No shared function signatures introduced; the plan operates on git state and existing files. The one pinned constant (`dd56585`) is used verbatim in T1 and T2.
- **Review Focus:** All five items pinned — clarify-drop (T4.4, T5.3), upstream-fix-loss (T4.4), fork-file-loss (T7.2), migration collision (T6.2), docker-context (T5.4).
- **Proportion:** Plan is longer than the short spec because git merge conflicts require per-file resolution intent; code blocks are commands and test assertions, not transcripts of application logic.
