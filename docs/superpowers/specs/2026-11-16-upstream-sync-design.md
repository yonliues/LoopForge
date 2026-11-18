# Design: full upstream sync while preserving multi-executor clarification

Date: 2026-11-16

## Intent and why

Bring the fork (`yonliues/LoopForge`, working copy at `qxzg/LoopForge`) fully up to
date with upstream `Tencent/LoopForge`, while keeping the fork's one local feature:
the multi-executor requirement-clarification work (17 commits, 52 files). This is a
catch-up, not a redesign: upstream contributes fixes, features, and refactors; the
clarification feature must survive intact and keep compiling and passing its tests.

The sync source is pinned to the local upstream clone at
`/home/yong/work/opensoure/LoopForge` at commit `dd56585` (2026-11-15), NOT to a
moving GitHub `main` ref. This makes the change set, conflict set, and migration
ceiling fixed and reproducible.

## Goals

- Integrate every upstream commit from the merge base `b751e09` (2026-11-13 13:00)
  through `dd56585` (205 commits, ~620 changed files) into the fork's `main`.
- Preserve all 17 local clarification commits and their behavior: per-engine
  availability, per-engine command construction, session pinning, personal default
  engine preference, web pickers, docs, and the `029` migration.
- Keep both histories and all commit hashes intact so the local, revision-based
  synchronization audit trail stays valid.
- Resolve the ~20 overlapping files by combining both sides' semantic changes, not
  by discarding one side.
- Keep deployment-specific adapters (`local/`, `.env`, `.data/`) isolated and never
  introduce private deployment files, credentials, or staff data.

## Non-goals

- No package publication or release tag (per `docs/upstream-sync.md`).
- No history rewrite: no rebase, no force-push, no cherry-pick replay.
- No change to the clarification feature's behavior beyond what merging requires.
- No touching of maintainer-only private audit records (they stay outside the repo).

## Anti-goals (failure even if it compiles)

- Silently dropping either upstream fixes or the clarification feature inside a
  conflicted file.
- Copying the upstream working tree over the fork checkout (destroys history and
  violates `docs/upstream-sync.md`).
- Committing secrets, `.env`, `.data/`, or private infrastructure.
- Leaving a broken migration sequence (two `029` scripts).
- Skipping validation and declaring success on an unbuilt tree.

## Constraints

- Repo `AGENTS.md`: preserve platform architecture; keep deployment adapters
  isolated; never copy private deployment files/credentials/staff data into the
  repo; repo-layer and handler-layer ownership rules must still hold after merge.
- `docs/upstream-sync.md`: port shared fixes with their full dependency chain;
  resolve conflicts using adapter and compatibility tests; never copy a deployment
  repository over the public checkout; audit labels must reflect code actually
  synchronized.
- `local/AGENTS.md`: Classic (`.codebuddy/.codex/.cursor/.claude`) and Portable
  (`skills/`) editions must not be mixed; `.cursor`/`.claude` are generated bundles
  (do not edit directly); after a Classic change run
  `python3 scripts/build-classic-hosts.py --write`; end-to-end validation commands
  are listed there.
- Migration convention: scripts are named `NNN_description.sql` with strictly
  increasing numbers; code does not run migrations or seed.

## Approaches considered

1. **Merge upstream into the fork (chosen).** Add a `upstream` remote, fetch, and
   `git merge --no-ff upstream/main`. Three-way merge auto-resolves the bulk of the
   620 files; ~20 overlapping files need manual resolution. All commit hashes and
   the single clarification feature branch are preserved; the only new commit is
   the merge. Cost: a number of conflict files to reconcile by hand and validate.
2. **Rebase the 17 local commits onto upstream.** Rewrites all 17 local commit
   hashes (including a merge commit), breaking the revision-based audit trail, and
   replays conflicts across each commit. Rejected.
3. **Cherry-pick all 205 upstream commits.** Equivalent to rebase in effort, with
   the same conflict exposure and no benefit under a full-sync requirement. Rejected.

Merge was chosen because the user wants a full sync with the local feature kept,
and because the repo's own sync guidance depends on tracing commits by revision.

## The how (decided)

### Branch and merge mechanics

- Work on a dedicated branch `sync/upstream-2026-11-16` cut from current `main`
  (tree is clean), so `main` stays stable and the merge is reviewable.
- Add the local clone as a remote: `git remote add upstream /home/yong/work/opensoure/LoopForge`;
  `git fetch upstream` (fetch by path, not the network).
- **Pre-merge gate:** assert the fetched upstream tip equals `dd56585`
  (`git rev-parse upstream/main`). If it differs, stop and re-scope — the analyzed
  conflict set and `029`→`033` migration ceiling are only valid for `dd56585`.
- Merge the pinned commit: `git merge --no-ff dd56585`. Resolve conflicts in the
  working tree, then commit the merge. Do not use `merge -X ours/theirs`:
  overlapping files need both sides' changes.
- After validation, merge the sync branch into `main` (fast-forward) and leave the
  result ready for the user to push. No force-push.

### Conflict resolution principles

- Files changed only upstream: take upstream as-is.
- Files changed only by the fork (including new fork-only files such as
  `internal/httpapi/clarify_engine.go`, `docs/proposals/clarify-multi-executor.md`,
  `docs/superpowers/plans/2026-11-12-clarify-multi-executor.md`,
  `migrations/029_clarify_model_preference.sql`, the clarify engine/availability/
  command test files, and the web picker tests): keep the fork version.
- Overlapping files: keep both sides' semantic changes. Known hot spots:
  - `internal/httpapi/clarify_mcp.go`: upstream `bda7b97` extracts TAPD/Gongfeng
    read instructions into a helper; the fork adds per-engine command construction.
    Keep the extracted helper and the multi-engine logic wired together.
  - `internal/httpapi/clarify_engine_test.go`, `clarify_session.go`,
    `clarify_jobs_test.go`, `clarify_mcp_test.go`: upstream also has files of these
    names; reconcile coverage so both upstream regressions and clarification
    engine behavior are tested.
  - `internal/repo/models.go`, `users.go`: merge upstream's new fields/columns with
    the fork's `clarify_model` preference column (no duplicate GORM tags).
  - `internal/httpapi/server.go`, `stream.go`: merge upstream wiring/refactors with
    the fork's clarify engine availability and session wiring.
  - `web/app/assistant-models.js`, `issue-drawer.js`, `sync-schedule.js`,
    `web/assistant_models_test.js`: keep the fork's clarification picker + upstream
    changes.
  - Docs (`CHANGELOG.md`, `README.md`, `README.zh-CN.md`, `design.md`,
    `docs/api/openapi.yaml`, `docs/deployment.md`): take upstream as the base and
    re-apply the fork's clarification docs (multi-executor spec, picker layout
    rules, engine selection, per-engine availability).
  - Build config (`Dockerfile`, `compose.yaml`, `scripts/test-runtime-image.sh`,
    `.dockerignore`, `.gitignore`): combine upstream changes with the fork's
    "keep local state out of the docker build context" and harness bundling.
- After resolving, grep the conflicted files for conflict markers and confirm no
  `<<<<<<<`, `=======`, `>>>>>>>` remain.

### Migration numbering

- Fork has `029_clarify_model_preference.sql`; upstream introduces
  `029_assistant_diagnostics.sql` and `030`–`032`. Renumber the fork's migration
  to `033_clarify_model_preference.sql` so the sequence stays strictly increasing
  and no number is duplicated. The SQL is a standalone `ALTER TABLE ... ADD COLUMN`
  that upstream does not otherwise reference, so renumbering is safe. Record the
  renumber in the merge resolution (the file becomes a fork-only addition at a new
  path, not a textual conflict).

### Validation (before merging the sync branch into main)

- Go: `go test ./...` and `go vet ./...`; pay special attention to
  `internal/httpapi/...` and `internal/repo/...`.
- Web/acceptance: run the repo's public-content and browser-acceptance checks
  relevant to the changed frontend files; run the clarification-specific tests.
- Local edition checks per `local/AGENTS.md`:
  `python3 scripts/build-classic-hosts.py --check`, `bash scripts/validate.sh`,
  `bash scripts/smoke-install.sh`, `bash scripts/e2e.sh`,
  `bash scripts/test-observability.sh`, `bash scripts/scan-secrets.sh`.
- Confirm migration numbers are unique and increasing; confirm no secrets or
  private deployment files entered the tree; confirm third-party licenses and
  contributor attribution are preserved.

## What is left to the builder

Produce the ordered, executable plan: remote setup and fetch, branch creation,
the merge command, a file-by-file conflict resolution checklist (with the exact
combine intent for each hotspot), the migration renumber step, the validation
commands, and the final merge into `main`. The plan should be executable by
someone who has not read this conversation.
