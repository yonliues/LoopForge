# Self-hosted GitLab Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `gitlab` provider that binds a self-hosted GitLab repository, clones and pushes with the executing user's personal access token, pulls issues, finds or creates and merges merge requests, tracks delivery, and exposes `workbench_gitlab_*` assistant tools.

**Architecture:** GitLab sits beside the existing `github:` branches. Repository identity is `gitlab:<host>[:port]/<path>` in `GongfengProjectID`. Each user stores one `GitLabBaseURL` and one `GitLabToken`, separate from `GitToken`. Tracker, delivery, runner binding, and assistant tools each gain a GitLab branch. GitHub and Gongfeng code paths stay in place.

**Tech Stack:** Go module `agentflow` (gin, gorm, net/http), SQLite in tests and MySQL in production, vanilla JS in `web/app/*.js` with strings mirrored in `web/i18n-en.js`.

**Spec:** `docs/superpowers/specs/2026-11-17-gitlab-provider-design.md`

## Global Constraints

- `provider` is `gitlab`. Identity stays in `GongfengProjectID` as `gitlab:<host>[:port]/<group>/<project>`. Host is lowercase. Ports 443 (https), 80 (http), and 22 (ssh) are omitted. Any other port is kept. Path segments match `[A-Za-z0-9_.-]+`, at least two segments. `.` and `..` are rejected. Userinfo, query, and fragment are rejected.
- A pasted URL becomes `gitlab` only when its host and port equal the project owner's saved `GitLabBaseURL`. HTTPS, HTTP, and SSH on that host qualify. The stored clone URL is HTTPS without a token. The stored web URL uses the owner's site scheme.
- `github.com` stays GitHub. The Gongfeng host stays Gongfeng. An SSH host that does not match the owner's site stays `provider=ssh`. An HTTP(S) host that is not GitHub, not Gongfeng, and not the owner's site is rejected. If the owner has no site and the paste is HTTP(S) on any other host, return 400 `请项目所有者先在个人设置填写 GitLab 地址和令牌`.
- User fields are `GitLabBaseURL` (`gitLabBaseUrl`) and `GitLabToken` (`gitLabToken`). Public payloads include the site and `gitLabTokenConfigured`, never the token. API calls go to `{site}/api/v4` with header `PRIVATE-TOKEN`. The project path is URL-encoded (`url.PathEscape` of the whole path, slashes become `%2F`).
- Before a token is sent, the repository host must equal that user's saved site. A mismatch fails first. Redirects are not followed. TLS uses the system trust store. Private hosts are allowed. The site must be an `http` or `https` origin with no userinfo, path, query, or fragment.
- Issue sync and merge-request delivery use the project owner's token. Clone and push use the executing user's token. Assistant tools use the requester's token.
- Issue sync is pull-only: `GET /issues?state=all`, 100 per page, at most 100 pages. Local id `gl-<repository id>-<iid>`, source `gitlab`, `opened` → `todo`, `closed` → closed with `CloseSource=external`. Keep a body that has clarification or was edited locally.
- Merge requests: keep the most recently updated match for the source branch. Merged → return `!iid` and do not create another. Open → merge it. Missing or closed-unmerged → create when a target is present. Same source and target is a no-op. No target and no open match returns `ErrNoOpenMR`. Merge with a merge commit, `should_remove_source_branch=false`. A pipeline, approval, or conflict error is returned as-is. Delivery state is only `opened`, `merged`, or `closed`. `gitlab:` rows skip Gongfeng promotion and the default-branch commit check.
- Prefer GitLab `web_url`. Else build from the repository web URL. If only the identity is available, `GongfengMRURL` uses `https://<host>/<path>/-/merge_requests/<iid>`.
- Assistant tools are `workbench_gitlab_*`, kind `gitlab`, only when the Main Bot session's project has a `gitlab` repository. Comments and labels require `target` `issue` or `merge_request`. Unknown labels are rejected before the write. `APPROVE` calls the approve API. `COMMENT` posts a note. `REQUEST_CHANGES` posts a note and unapproves when this token already approved. Merge accepts `merge` and `squash` only. `sha` is forwarded. Issue close sends no close reason. 「请求批准」 cannot review, merge, or close. 「帮我批准」 reviews comments, labels, and issue creation. 「完全权限」 writes without that review. No delete tool. Lists report `hasMore`. Truncation budgets match the GitHub tools. A failed pipeline read is an unknown check.
- One repository sync failure is logged and the loop continues. The overall sync fails if any repository failed. Verify timeout is 10 seconds. Sync and merge-request timeout is 30 seconds. Logs, API responses, and tool text never contain the token.
- AutoMigrate adds `user_configs.gitlab_base_url` and `user_configs.gitlab_token`. Also add `migrations/034_gitlab_credentials.sql` for operators. Startup does not run that script. No new table. `GitToken` is unchanged.
- Default tests use a local fake GitLab. The live test runs only when `AGENTFLOW_GITLAB_LIVE_WRITE=1` and `AGENTFLOW_GITLAB_LIVE_REPO` are set, and it must not merge into the default branch.
- AGENTS.md: new repo methods belong on `UsersRepo`. HTTP handlers for credentials belong on `usersHandlers`. Tracker methods belong on `tracker.Service`. Assistant tool methods belong on `agentToolGateway`. One file does not add methods for another domain.
- User-visible copy is Chinese. Every new `web/` string gets an English key in `web/i18n-en.js`.
- Do not add a dependency. Do not change GitHub request shapes, Gongfeng MCP calls, or the existing `explainGitAuthFailure` text.

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/domain/gitlab_repository.go` | Parse a GitLab URL against a saved site. Host key, API path, identity split. |
| `internal/domain/gitlab_repository_test.go` | Parser tests. |
| `internal/domain/delivery_mr.go` | `GongfengMRURL` gains a `gitlab:` branch. |
| `internal/domain/user.go` | `GitLabBaseURL`, `GitLabToken`, `GitLabTokenConfigured`. `PublicUser` strips the token. |
| `internal/repo/models.go` | `userConfigRow` columns for AutoMigrate. |
| `internal/repo/users.go` | Select, scan, write, and `UserCredentialUpdate` for the two fields. `PutUser` preserves them. |
| `migrations/034_gitlab_credentials.sql` | Manual `ALTER TABLE` for the two columns. |
| `internal/httpapi/gitlab_credential.go` | Site validation, `GET /user` check, no redirects. |
| `internal/httpapi/users.go` | Verify before save. Clear removes both. |
| `internal/httpapi/server.go` | No new route. Save stays `PUT /api/users/:id/credentials`. |
| `internal/httpapi/catalog.go` | Recognize GitLab before the SSH early return. |
| `internal/tracker/gitlab.go` | HTTP client, issue pull, merge-request find/create/merge, delivery refresh. |
| `internal/tracker/service.go` | `PullAll` and `PullScheduleJob` dispatch `provider=gitlab` / source `gitlab`. |
| `internal/tracker/merge_mr.go` | `gitlab:` prefix beside `github:`. |
| `internal/tracker/mr_delivery_sync.go` | Refresh `gitlab:` rows. Skip promotion and default-branch commit checks. |
| `internal/engine/runner_dispatch.go` | Per-repository token. Host mismatch fails before clone. |
| `internal/engine/git_auth.go` | `explainGitLabAuthFailure`. Leave `explainGitAuthFailure` unchanged. |
| `internal/runtime/runtime.go` | `RunCredentials.GitLabToken`. |
| `internal/httpapi/agent_tool_gitlab.go` | Read tools and the dispatcher. |
| `internal/httpapi/agent_tool_gitlab_write.go` | Comment, labels, review, merge, close, create. |
| `internal/httpapi/agent_tool_catalog.go` | Register GitLab tools only when the project has a GitLab repository. |
| `internal/httpapi/agent_tool_mcp.go` | `dispatch` sends kind `gitlab` to `callMainBotGitLab`. |
| `web/app.js` | Wire `#importGitLabIssues` to `syncNow`. |
| `web/app/workbench.js` | GitLab credential card. |
| `web/index.html` | Source filter and schedule source option. Repository hint. |
| `web/app/sync-schedule.js` | Badge, count, sync button, schedule source. |
| `web/app/issue-drawer.js` | Source name and conversation card matching. |
| `web/i18n-en.js` | English keys for the new strings. |

---

### Task 1: Parse a GitLab URL into a repository identity

**Files:**
- Create: `internal/domain/gitlab_repository.go`
- Create: `internal/domain/gitlab_repository_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:

```go
func ParseGitLabRepositoryURL(raw, site string) (GongfengRepoInfo, bool)
func GitLabIdentityHost(project string) (host, path string, ok bool)
func GitLabAPIProject(project string) (escaped string, ok bool)
```

`ParseGitLabRepositoryURL` returns false when `site` is empty or not an `http`/`https` origin. It accepts `https://host/group/sub/repo.git`, `http://host:8443/group/repo/-/tree/main`, `git@host:group/repo.git`, and `ssh://git@host:22/group/repo.git`. `Project` is `gitlab:` plus the host key plus `/` plus the path. `WebURL` uses the site scheme and has no `.git`. `CloneURL` is `WebURL + ".git"`. `Name` is the last path segment.

Host key comparison ignores case. Ports 80, 443, and 22 are omitted. `ssh://git@host:2222/group/repo.git` matches only a site whose port is 2222.

Reject userinfo, query, fragment, a missing group, a segment outside `[A-Za-z0-9_.-]+`, and `.` or `..`. Strip a trailing `.git` and the first `/-/` subpage (`/-/tree/`, `/-/blob/`, `/-/merge_requests/`, `/-/issues/`, `/-/commit/`, `/-/commits/`).

`GitLabIdentityHost("gitlab:git.example.com:8443/platform/api")` returns host `git.example.com:8443` and path `platform/api`. `GitLabAPIProject` of that identity returns `platform%2Fapi`.

- [ ] **Step 1: Write the failing test**

```go
func TestParseGitLabRepositoryURL(t *testing.T) {
	const site = "https://Git.Example.com"
	for _, raw := range []string{
		"https://git.example.com/platform/api.git",
		"https://git.example.com/platform/api/-/tree/main",
		"git@git.example.com:platform/api.git",
		"ssh://git@git.example.com:22/platform/api.git",
	} {
		got, ok := ParseGitLabRepositoryURL(raw, site)
		if !ok || got.Project != "gitlab:git.example.com/platform/api" || got.WebURL != "https://git.example.com/platform/api" || got.CloneURL != got.WebURL+".git" || got.Name != "api" {
			t.Fatalf("%s: %+v ok=%v", raw, got, ok)
		}
	}
	got, ok := ParseGitLabRepositoryURL("https://git.example.com:8443/group/sub/repo", "https://git.example.com:8443")
	if !ok || got.Project != "gitlab:git.example.com:8443/group/sub/repo" {
		t.Fatalf("port: %+v %v", got, ok)
	}
	for _, raw := range []string{
		"https://user:secret@git.example.com/group/repo",
		"https://git.example.com/group/repo?x=1",
		"https://git.example.com/group/../repo",
		"https://other.example/group/repo",
		"https://git.example.com/repo",
	} {
		if _, ok := ParseGitLabRepositoryURL(raw, site); ok {
			t.Fatalf("accepted %s", raw)
		}
	}
	if _, ok := ParseGitLabRepositoryURL("https://git.example.com/group/repo", ""); ok {
		t.Fatal("empty site accepted")
	}
	host, path, ok := GitLabIdentityHost("gitlab:git.example.com:8443/platform/api")
	if !ok || host != "git.example.com:8443" || path != "platform/api" {
		t.Fatalf("split %s %s %v", host, path, ok)
	}
	escaped, ok := GitLabAPIProject("gitlab:git.example.com/platform/api")
	if !ok || escaped != "platform%2Fapi" {
		t.Fatalf("escape %s %v", escaped, ok)
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/domain -run '^TestParseGitLabRepositoryURL$' -count=1`

Expected: FAIL to compile, `undefined: ParseGitLabRepositoryURL`.

- [ ] **Step 3: Implement the parser**

Put the three functions in `internal/domain/gitlab_repository.go`. Do not call them from `ParseRepositoryURL`. A GitLab host must not be parsed as Gongfeng, and a Gitea URL must not become GitLab unless the caller passes a matching site.

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test ./internal/domain -run '^TestParseGitLabRepositoryURL$' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/gitlab_repository.go internal/domain/gitlab_repository_test.go
git commit -m "feat(gitlab): parse a self-hosted repository URL into a host identity"
```

---

### Task 2: Reconstruct a GitLab merge-request URL from the identity

**Files:**
- Modify: `internal/domain/delivery_mr.go` (`GongfengMRURL`)
- Test: `internal/domain/delivery_mr_test.go` if it exists, otherwise `internal/domain/gitlab_repository_test.go`

**Interfaces:**
- Consumes: Task 1 identity form. `GongfengMRURL(project string, iid int) string` already exists.
- Produces: the same function. For `gitlab:` it returns `https://<host-and-path>/-/merge_requests/<iid>`. `github:` and the Gongfeng fallback stay as they are. `iid <= 0` or an empty project still returns `""`.

- [ ] **Step 1: Write the failing test**

```go
func TestGongfengMRURLGitLabIdentity(t *testing.T) {
	if got := GongfengMRURL("gitlab:git.example.com/platform/api", 12); got != "https://git.example.com/platform/api/-/merge_requests/12" {
		t.Fatalf("url %s", got)
	}
	if got := GongfengMRURL("gitlab:git.example.com:8443/platform/api", 3); got != "https://git.example.com:8443/platform/api/-/merge_requests/3" {
		t.Fatalf("port url %s", got)
	}
	if GongfengMRURL("github:octo/hello", 4) != "https://github.com/octo/hello/pull/4" {
		t.Fatal("github url changed")
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/domain -run '^TestGongfengMRURLGitLabIdentity$' -count=1`

Expected: FAIL. The current fallback is `https://git.example.com/<project>/merge_requests/<iid>`, so the assertion on `/-/merge_requests/` fails.

- [ ] **Step 3: Add the `gitlab:` branch**

In `GongfengMRURL`, after the `github:` branch, cut the `gitlab:` prefix and format `https://%s/-/merge_requests/%d`. Do not change the GitHub or Gongfeng returns.

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test ./internal/domain -run '^TestGongfengMRURLGitLabIdentity$|TestPublicRepositoryAndPullRequestIdentity$' -count=1`

Expected: PASS. The existing GitHub URL test still passes.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/delivery_mr.go internal/domain/gitlab_repository_test.go internal/domain/delivery_mr_test.go
git commit -m "feat(gitlab): build merge request URLs from the repository identity"
```

---

### Task 3: Store one GitLab site and token on the user

**Files:**
- Modify: `internal/domain/user.go`
- Modify: `internal/repo/models.go` (`userConfigRow`)
- Modify: `internal/repo/users.go` (`userWithConfig`, `userSelectColumns`, `toDomain`, `userHasConfig`, `PutUser`, `UserCredentialUpdate`, `UpdateUserCredentials`, `writeUserConfig`)
- Create: `migrations/034_gitlab_credentials.sql`
- Test: `internal/repo/users_gitlab_test.go`

**Interfaces:**
- Consumes: `PublicUser`, `UpdateUserCredentials`, `nullableString`.
- Produces:

```go
// On domain.User
GitLabBaseURL          string `json:"gitLabBaseUrl,omitempty"`
GitLabToken            string `json:"gitLabToken,omitempty"`
GitLabTokenConfigured  bool   `json:"gitLabTokenConfigured,omitempty"`

// On repo.UserCredentialUpdate
GitLabBaseURL string `json:"gitLabBaseUrl"`
GitLabToken   string `json:"gitLabToken"`
GitLabClear   bool   `json:"gitLabClear"`
```

`PublicUser` sets `GitLabTokenConfigured` from a non-empty token, then clears `GitLabToken`. It still clears `GitToken`. `GitLabClear` sets both new fields to empty and does not touch `GitToken`. A non-empty `GitLabToken` or `GitLabBaseURL` in the update writes that value. Absent or empty fields keep the stored value, except `GitLabClear`. `PutUser` on an existing user copies the stored GitLab fields the same way it copies `GitToken`, so a login refresh cannot wipe them. `userHasConfig` is true when either GitLab field is set. Columns are `gitlab_base_url` (`text`) and `gitlab_token` (`text`) on `user_configs`.

- [ ] **Step 1: Write the failing test**

```go
func TestGitLabCredentialsStayOffTheGitHubToken(t *testing.T) {
	st := OpenTest(t)
	if err := st.PutUser(domain.User{LoginName: "gl", GitToken: "github-token", GitLabBaseURL: "https://git.example.com", GitLabToken: "glpat-secret"}); err != nil {
		t.Fatal(err)
	}
	saved, ok := st.UserByLoginName("gl")
	if !ok || saved.GitToken != "github-token" || saved.GitLabBaseURL != "https://git.example.com" || saved.GitLabToken != "glpat-secret" {
		t.Fatalf("stored %+v", saved)
	}
	pub := domain.PublicUser(saved)
	if pub.GitLabToken != "" || !pub.GitLabTokenConfigured || pub.GitLabBaseURL != "https://git.example.com" || pub.GitToken != "" {
		t.Fatalf("public %+v", pub)
	}
	again, err := st.PutUser(domain.User{ID: saved.ID, LoginName: "gl"})
	if err != nil {
		t.Fatal(err)
	}
	_ = again
	kept, _ := st.User(saved.ID)
	if kept.GitLabToken != "glpat-secret" || kept.GitToken != "github-token" {
		t.Fatalf("login refresh wiped credentials: %+v", kept)
	}
	cleared, err := st.UpdateUserCredentials(saved.ID, UserCredentialUpdate{GitLabClear: true})
	if err != nil || cleared.GitLabToken != "" || cleared.GitLabBaseURL != "" || cleared.GitToken != "github-token" {
		t.Fatalf("clear %+v %v", cleared, err)
	}
}
```

`OpenTest` is `repo.OpenTest`. Use the same constructor other `internal/repo` tests use. If `PutUser` does not return the user, ignore that return and reload with `UserByLoginName`.

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/repo -run '^TestGitLabCredentialsStayOffTheGitHubToken$' -count=1`

Expected: FAIL to compile, unknown fields `GitLabBaseURL` and `GitLabToken`.

- [ ] **Step 3: Add the columns and the update path**

Wire every read and write listed in the files section. `userSelectColumns` adds `c.gitlab_base_url, c.gitlab_token`. `writeUserConfig` writes both with `nullableString`.

`migrations/034_gitlab_credentials.sql`:

```sql
-- Personal GitLab site and access token. Apply manually; startup does not
-- run this script. gorm AutoMigrate also adds the columns on process start.
ALTER TABLE user_configs ADD COLUMN gitlab_base_url TEXT NULL;
ALTER TABLE user_configs ADD COLUMN gitlab_token TEXT NULL;
```

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test ./internal/repo -run '^TestGitLabCredentialsStayOffTheGitHubToken$' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/user.go internal/repo/models.go internal/repo/users.go internal/repo/users_gitlab_test.go migrations/034_gitlab_credentials.sql
git commit -m "feat(gitlab): store a personal site and access token apart from GitHub"
```

---

### Task 4: Reject a bad site and verify the token before saving

**Files:**
- Create: `internal/httpapi/gitlab_credential.go`
- Modify: `internal/httpapi/users.go` (`user`)
- Test: `internal/httpapi/gitlab_credential_test.go`

**Interfaces:**
- Consumes: Task 3 update fields. `validCLIBaseURL` is not strict enough because it allows a path.
- Produces:

```go
func validGitLabSite(raw string) bool
func verifyGitLabUser(ctx context.Context, site, token string) error
```

`validGitLabSite` accepts only `http` or `https`, a non-empty host, no userinfo, no path other than empty or `/`, no query, no fragment, length at most 2048. `https://git.example.com/api/v4` is invalid.

`verifyGitLabUser` sends `GET {site}/api/v4/user` with `PRIVATE-TOKEN` set to the token. The client timeout is 10 seconds and `CheckRedirect` returns `http.ErrUseLastResponse`. Status 200 with a JSON `id` or `username` returns nil. Any other status, a redirect, a transport error, or a body without those fields returns an error whose text does not contain the token. The error tells the user to check 个人配置中的 GitLab 地址和令牌.

`usersHandlers.user` calls this only when the update sets a site or a token and `GitLabClear` is false. Both values are required on that save: if one is empty, fill it from the stored user; if either is still empty, return 400 `请同时填写 GitLab 地址和令牌` and do not write. Verification failure returns 400 and does not call `UpdateUserCredentials`. `GitLabClear` skips verification. A GitHub `gitToken` update on the same request still follows today's path when no GitLab field is being set.

- [ ] **Step 1: Write the failing test**

Use `httptest.NewServer`. Assert `validGitLabSite("https://git.example.com")` and refuse `"https://git.example.com/api/v4"`, `"https://user:pw@git.example.com"`, and `"ftp://git.example.com"`. Point `verifyGitLabUser` at the test server by passing the server URL as the site. A handler that checks `PRIVATE-TOKEN == "glpat-secret"` and returns `{"id":1,"username":"ada"}` makes verify return nil. A 401 makes verify return an error that does not contain `glpat-secret`. A handler that answers 302 makes verify return an error and records that the token header arrived only on the first request.

Drive `PUT /api/users/{id}/credentials` through the existing users handler test setup. Body `{"gitLabBaseUrl":"https://git.example.com/api/v4","gitLabToken":"glpat-secret"}` returns 400 and leaves the stored token empty. Body with a valid site and token against the fake server returns 200, `gitLabToken` is absent, `gitLabTokenConfigured` is true, and `gitToken` is unchanged.

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/httpapi -run '^TestGitLabCredential' -count=1`

Expected: FAIL to compile, `undefined: validGitLabSite`.

- [ ] **Step 3: Implement validation and the save gate**

Keep the HTTP call in `gitlab_credential.go`. The handler method stays on `usersHandlers` in `users.go`. Do not log the token.

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test ./internal/httpapi -run '^TestGitLabCredential' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/httpapi/gitlab_credential.go internal/httpapi/gitlab_credential_test.go internal/httpapi/users.go
git commit -m "feat(gitlab): verify the personal access token before saving it"
```

---

### Task 5: Save a matching URL as a GitLab repository

**Files:**
- Modify: `internal/httpapi/catalog.go` (the repository save function that branches on SSH and `ParseRepositoryURL`)
- Test: `internal/httpapi/catalog_gitlab_test.go`

**Interfaces:**
- Consumes: `domain.ParseGitLabRepositoryURL`, `h.repo.Project`, `h.repo.User`, `h.saveRepository`.
- Produces: no new exported function. A saved GitLab row has `Provider=="gitlab"`, `GongfengProjectID` from the parser, `CloneURL` ending in `.git` and containing no token, `WebURL` using the owner's scheme.

Recognition order inside the save handler:

1. Resolve the project. Load `Project.OwnerID` and that user.
2. If `ParseGitLabRepositoryURL(primary, owner.GitLabBaseURL)` succeeds, set provider, project id, web URL, clone URL, and name when empty. Default branch `main` when empty. Skip the SSH early return. Save.
3. If the owner has no `GitLabBaseURL` and `primary` is HTTP(S) and not a GitHub URL and not a Gongfeng URL, return 400 `请项目所有者先在个人设置填写 GitLab 地址和令牌`.
4. Otherwise keep today's SSH, GitHub, and Gongfeng branches unchanged.

- [ ] **Step 1: Write the failing test**

Create a project whose owner has `GitLabBaseURL=https://git.example.com` and a token. `POST /api/repositories` with `repoUrl=git@git.example.com:platform/api.git` returns 200, `provider=gitlab`, `gongfengProjectId=gitlab:git.example.com/platform/api`, `cloneUrl=https://git.example.com/platform/api.git`.

A second project whose owner has no GitLab site: `repoUrl=https://git.example.com/platform/api` returns 400 and the body contains `个人设置`. `repoUrl=https://github.com/octo/widgets` still returns `provider=github`. `repoUrl=git@gitea.example.com:org/app.git` still returns `provider=ssh`.

An owner whose site is `https://git.example.com`: `repoUrl=https://gitea.example.com/org/app` stays a 400 and is not saved as `gitlab`.

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/httpapi -run '^TestSaveGitLabRepository$' -count=1`

Expected: FAIL. The SSH paste is stored as `ssh`, or the HTTPS paste is `仓库 URL 无法识别`.

- [ ] **Step 3: Insert the GitLab branch**

Do this before the SSH early return so a matching SSH paste does not become `provider=ssh`. Do not rewrite `ParseRepositoryURL`.

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test ./internal/httpapi -run '^TestSaveGitLabRepository$|TestCatalogRepositoryAgentAndAssetBranches$' -count=1`

Expected: PASS. The existing catalog case that posts `provider=gitlab` without a matching owner site still returns 400.

- [ ] **Step 5: Commit**

```bash
git add internal/httpapi/catalog.go internal/httpapi/catalog_gitlab_test.go
git commit -m "feat(gitlab): bind a repository whose host matches the owner's site"
```

---

### Task 6: Call the GitLab API without following redirects

**Files:**
- Create: `internal/tracker/gitlab.go`
- Test: `internal/tracker/gitlab_client_test.go`

**Interfaces:**
- Consumes: `GitLabIdentityHost`, `GitLabAPIProject`. `tracker.Service` already has `projectAuth` with `Repositories`, `Project`, and `User`, used by `githubClient`.
- Produces:

```go
type gitlabClient struct {
	base, path, token string
	http              *http.Client
}

func (s *Service) gitlabClient(project string) (*gitlabClient, error)
func (g *gitlabClient) request(ctx context.Context, method, suffix string, input, output any) error
```

`gitlabClient` finds the repository whose `GongfengProjectID` equals `project` and whose `Provider` is `gitlab`, then loads that project's owner. The owner's `GitLabBaseURL` host must equal `GitLabIdentityHost`. On mismatch return an error and do not build a client. Empty token returns an error that names 个人配置. `base` is `strings.TrimRight(site, "/") + "/api/v4"`. `path` is `GitLabAPIProject`. The HTTP client timeout is 30 seconds and redirects return `http.ErrUseLastResponse`.

`request` joins `base + "/projects/" + path + suffix`. It sets `PRIVATE-TOKEN` and `Accept: application/json`. A 3xx returns an error that includes the status and not the token. A non-2xx returns the response body with the token string removed, capped at 4 KiB. Decode JSON into `output` when it is non-nil. Encode `input` when it is non-nil.

- [ ] **Step 1: Write the failing test**

Stand up `httptest.NewServer`. Store a project, owner (`GitLabBaseURL` = server URL, token `glpat-secret`), and repository `gitlab:<server-host>/team/app`. Call `gitlabClient` and `request` `GET /issues?per_page=1`. The fake server asserts the path is `/api/v4/projects/team%2Fapp/issues`, the header is `glpat-secret`, and there is no `Authorization` header. A 302 from the server surfaces as an error and the test records a single incoming request.

A second repository whose identity host is `evil.example` returns an error from `gitlabClient` and the fake server receives zero requests.

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/tracker -run '^TestGitLabClient' -count=1`

Expected: FAIL to compile, `undefined: (*Service).gitlabClient`.

- [ ] **Step 3: Implement the client**

Keep GitHub's client in `github.go`. Do not share a struct with it.

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test ./internal/tracker -run '^TestGitLabClient$' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tracker/gitlab.go internal/tracker/gitlab_client_test.go
git commit -m "feat(gitlab): call the API with the owner's token and no redirects"
```

---

### Task 7: Pull GitLab issues

**Files:**
- Modify: `internal/tracker/gitlab.go`
- Modify: `internal/tracker/service.go` (`PullAll`, `PullScheduleJob`)
- Test: `internal/tracker/gitlab_sync_test.go`

**Interfaces:**
- Consumes: Task 6 client. `applyExistingIssue`, `githubBodyHash` (reuse the hash function; do not copy a second hasher). `gongfengStateToStatus` maps `opened` to `todo` and `closed` to `closed`.
- Produces:

```go
func (s *Service) pullGitLabIssues(store IssueStoreAdapter, repo domain.Repository) (int, error)
```

Page with `?state=all&per_page=100&page=N` until a short page or page 100. Page 101 is not requested; return `fmt.Errorf("GitLab import exceeded 100 pages")` after storing what fit. Map each issue to `domain.Issue` the way `pullGitHubIssues` does, with id `gl-<repo.ID>-<iid>`, `Source: "gitlab"`, `ExternalRef.Source: "gitlab"`, `ExternalID` the global id, `ExternalIID` the iid, `ExternalURL` `web_url`. `opened` forces status `todo`. `closed` sets `CloseSource` to `external`. When an old issue exists, call `applyExistingIssue`. If `old.Clarification.Status != ""` or the stored body hash differs from `old.ExternalRef.SourceContentHash`, keep `old.Description`.

`PullAll` handles `repo.Provider == "gitlab"` in the same loop as GitHub, writes a `SyncLog` with `Source: "gitlab"`, and continues to the next repository after a failure. `PullScheduleJob` gains `case "gitlab":` with the same project filter GitHub uses.

- [ ] **Step 1: Write the failing test**

Fake server returns two issues on page 1 (`state` `opened` and `closed`) and an empty page 2. After `pullGitLabIssues`, the store has `gl-<id>-1` in `todo` and `gl-<id>-2` closed with `CloseSource=external`. Run again after setting the open issue's clarification status and changing its description. The description stays local.

A server that always returns 100 items fails the pull with `exceeded 100 pages` once 100 pages were stored. `PullAll` with one failing GitLab repository and one successful GitHub repository still imports the GitHub issue and returns an error.

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/tracker -run '^TestPullGitLabIssues$' -count=1`

Expected: FAIL to compile, `undefined: (*Service).pullGitLabIssues`.

- [ ] **Step 3: Implement the pull and the two dispatch sites**

Do not push local issues to GitLab. Do not call the Gongfeng client for these repositories.

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test ./internal/tracker -run '^TestPullGitLabIssues$|TestGitHub' -count=1`

Expected: PASS. Existing GitHub pull tests still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/tracker/gitlab.go internal/tracker/gitlab_sync_test.go internal/tracker/service.go
git commit -m "feat(gitlab): pull issues into local work items"
```

---

### Task 8: Find, create, and merge a merge request

**Files:**
- Modify: `internal/tracker/gitlab.go`
- Modify: `internal/tracker/merge_mr.go` (`EnsureAndMergeBranchMR`, `FindBranchMR`)
- Modify: `internal/tracker/mr_delivery_sync.go` (the refresh loop, `discoverPromoteMRs`, `syncWorkCommitsOnDefaultBranch`)
- Test: `internal/tracker/gitlab_mr_test.go`

**Interfaces:**
- Consumes: Task 6 client, `ErrNoOpenMR`, `domain.DeliveryMR`, `domain.GongfengMRURL`.
- Produces:

```go
func (s *Service) gitlabFindBranch(project, branch string) (domain.DeliveryMR, bool, error)
func (s *Service) gitlabMergeBranch(project, branch, target, title, description string) (string, error)
func (s *Service) refreshGitLabDelivery(store MRDeliveryStore, project string, iid int) error
```

`gitlabFindBranch` lists `GET /merge_requests?source_branch=<branch>&state=all&order_by=updated_at&sort=desc&per_page=20` and keeps the first item whose `source_branch` equals `branch`. Delivery state is `merged` when `state=="merged"`, `closed` when `state=="closed"`, otherwise `opened`. `URL` is `web_url` when present, otherwise `GongfengMRURL`. `IID` is the iid.

`gitlabMergeBranch` uses that lookup. Merged returns `!` + iid. Open is merged in place. Missing or closed-unmerged: return `ErrNoOpenMR` when `target` is empty; otherwise `POST /merge_requests` with `source_branch`, `target_branch`, title defaulting to `LoopForge: <branch> → <target>`, and the description. Then `PUT /merge_requests/<iid>/merge` with `should_remove_source_branch: false` and no squash flag. A non-2xx from merge is returned unchanged. Same branch and target returns `""`, nil.

`EnsureAndMergeBranchMR` and `FindBranchMR` dispatch `strings.HasPrefix(project, "gitlab:")` the same way they dispatch `github:`.

`refreshGitLabDelivery` loads the merge request and calls `store.UpdateMRDeliveryState` with the classified state, target branch, `merged_at` when state is merged, and `time.Now().UTC()` as the refresh time. The delivery loop handles `gitlab:` like `github:` and does not fall through to Gongfeng. `discoverPromoteMRs` and `syncWorkCommitsOnDefaultBranch` `continue` when the project starts with `gitlab:`.

- [ ] **Step 1: Write the failing test**

Fake server. No existing merge request, target `main`: assert one POST and one PUT, PUT body has `should_remove_source_branch: false` and no `squash: true`, return value `!9`. Second call sees `state: merged` and does not POST. Source equal to target performs no HTTP. Empty target and an empty list returns `ErrNoOpenMR`. A closed match with a target POSTs a new one. PUT 405 body `pipeline must succeed` is returned and contains that text. `refreshGitLabDelivery` on `state: opened` stores `opened`. A repository passed to `discoverPromoteMRs` with a `gitlab:` project id causes zero Gongfeng calls (the test's Gongfeng client is nil and the function returns without logging a panic).

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/tracker -run '^TestGitLabMergeBranch$' -count=1`

Expected: FAIL to compile, `undefined: (*Service).gitlabMergeBranch`.

- [ ] **Step 3: Implement the three methods and the prefix branches**

Do not send `merge_when_pipeline_succeeds` or a skip-CI flag.

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test ./internal/tracker -run '^TestGitLabMergeBranch$|TestEnsureAndMerge' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tracker/gitlab.go internal/tracker/gitlab_mr_test.go internal/tracker/merge_mr.go internal/tracker/mr_delivery_sync.go
git commit -m "feat(gitlab): open and merge a merge request without dropping the source branch"
```

---

### Task 9: Give each repository its own clone token

**Files:**
- Modify: `internal/runtime/runtime.go` (`RunCredentials` add `GitLabToken string`)
- Modify: `internal/engine/run_environment.go` (`resolveRunCredentials` sets it from `user.GitLabToken`)
- Modify: `internal/engine/runner_dispatch.go` (`bindRunnerRepositories`)
- Modify: `internal/engine/git_auth.go`
- Modify: every `bindRunnerRepositories` caller listed below
- Test: `internal/engine/gitlab_token_bind_test.go`

**Interfaces:**
- Consumes: `GitLabIdentityHost`, `domain.RepositoryContext.Provider`, `domain.RepositoryProviderSSH`.
- Produces:

```go
func bindRunnerRepositories(invocation *domain.RunnerInvocation, run domain.Run, gitToken, gitLabToken, gitUserName, gitUserEmail string) error
func explainGitLabAuthFailure(err error, tokenConfigured bool) error
```

Token choice per repository context: `gitlab` gets `gitLabToken`, `github` gets `gitToken`, `ssh` gets `""` and `UseSSH` stays true. A `gitlab` context whose `GongfengProjectID` host differs from the host embedded in the clone URL's host, or whose token is being attached while `gitLabToken` is empty, returns an error before any token is copied onto the invocation. The empty-token error is `运行责任人未配置 GitLab 令牌，请到「个人配置 → GitLab」填写站点地址和个人访问令牌`. The host-mismatch error is `仓库主机与执行人保存的 GitLab 站点不一致，已停止克隆` and the invocation's token fields stay empty.

`explainGitLabAuthFailure` uses the same detection as `explainGitAuthFailure` (git clone/fetch/pull/push plus an auth failure) but the hint is `请责任人到「个人配置 → GitLab」配置个人访问令牌，勾选 api 权限`. It does not mention `github.com/settings/tokens`. `explainGitAuthFailure` is not edited. Call the GitLab explainer only when the failing repository provider is `gitlab`.

Update these call sites to pass the GitLab token (empty when the caller truly has only a GitHub token) and to return the new error:

- `internal/engine/runner_dispatch.go` (the call near the credential snapshot, the call inside `dispatchRunnerScript`, and the call that uses `resolveGitToken`)
- `internal/engine/workspace_preparation.go`
- `internal/engine/conversation_push.go`
- `internal/engine/runner_integrate.go`
- tests in `runner_cases_test.go`, `runner_dispatch_test.go`, `runner_sandbox_test.go`, `workspace_owner_test.go` that call `bindRunnerRepositories` directly: pass `""` as the GitLab token and ignore a nil error when the run has no GitLab repository

`resolveRunCredentials` copies `user.GitLabToken` into `RunCredentials.GitLabToken`. It does not copy that value into `GitToken`.

- [ ] **Step 1: Write the failing test**

Build a run with three repository contexts: GitHub `github:octo/app`, GitLab `gitlab:git.example.com/team/app` with clone URL `https://git.example.com/team/app.git`, and SSH. Call `bindRunnerRepositories` with git token `ghp` and GitLab token `glpat`. The GitHub entry's `GitToken` is `ghp`, the GitLab entry's is `glpat`, the SSH entry's is empty. Call again with GitLab token `""` and expect the 个人配置 error. Call with a GitLab context whose clone host is `evil.example` and expect the 主机 error and empty tokens. `explainGitLabAuthFailure` on a clone auth error contains `个人配置 → GitLab` and does not contain `github.com/settings/tokens`. `explainGitAuthFailure` on the same raw error still contains `github.com/settings/tokens`.

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/engine -run '^TestBindRunnerRepositoriesSeparatesGitLabToken$' -count=1`

Expected: FAIL to compile, wrong number of arguments to `bindRunnerRepositories`.

- [ ] **Step 3: Change the signature and the call sites**

Keep GitHub repositories on `GitToken`. Do not put the GitLab token into `RunCredentials.GitToken`.

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test ./internal/engine -run '^TestBindRunnerRepositoriesSeparatesGitLabToken$|TestExplainGitAuthFailure$|TestInvocationGitToken' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/engine/run_environment.go internal/engine/runner_dispatch.go internal/engine/git_auth.go internal/engine/workspace_preparation.go internal/engine/conversation_push.go internal/engine/runner_integrate.go internal/engine/gitlab_token_bind_test.go internal/engine/runner_cases_test.go internal/engine/runner_dispatch_test.go internal/engine/runner_sandbox_test.go internal/engine/workspace_owner_test.go
git commit -m "feat(gitlab): clone with the executor's GitLab token"
```

---

### Task 10: Read GitLab issues and merge requests from the assistant

**Files:**
- Create: `internal/httpapi/agent_tool_gitlab.go`
- Modify: `internal/httpapi/agent_tool_catalog.go` (append after `mainBotGitHubTools`)
- Modify: `internal/httpapi/agent_tool_mcp.go` (`dispatch`, the `tool.Kind == "github"` branch)
- Test: `internal/httpapi/agent_tool_gitlab_test.go`

**Interfaces:**
- Consumes: `AgentToolSession`, `redactAgentToolResult`, `errAgentToolForbidden`, `errAgentToolUnknown`. The requester is `h.repo.User(session.User.ID)`. The token is that user's `GitLabToken`. The site is `GitLabBaseURL`.
- Produces these tool names, kind `gitlab`:

```text
workbench_gitlab_issues_list
workbench_gitlab_issue_get
workbench_gitlab_mrs_list
workbench_gitlab_mr_status
workbench_gitlab_mr_files
```

`mainBotGitLabTools(session *AgentToolSession) []agentToolDescriptor` returns nil unless `session.Mode == AgentToolAssistant` and `ProjectID` is non-empty. `agent_tool_catalog.go` appends them only when some repository has `ProjectID == session.ProjectID` and `Provider == "gitlab"`. A project with no GitLab repository gets zero GitLab tools and still gets the GitHub tools.

Resolution: `repository` is the bound repository name or id inside the session project. The row's provider must be `gitlab` and `GitLabIdentityHost` must equal the host of the requester's site. Otherwise return an error and do not call GitLab. Another project's repository is rejected.

List tools accept `state` `opened|closed|all` (default `opened`) and `limit` 1..50 (default 20). They set `hasMore` when more matches exist than `limit`. Issue get returns the full description and at most 30 non-system notes (`system: false`), each body truncated to 2000 runes. MR status includes title, state, source and target branches, head sha, `merge_status`, author, and pipeline summary. A pipeline request that fails sets `checksState` to `unknown` and puts the reason in `checksWarnings`. MR files calls `GET /merge_requests/:iid/diffs`, default limit 50, max 100, each patch truncated to 3000 runes, total patch budget 40000 runes. These numbers match `githubPRFilePatchRunes`, `githubPRFilesPatchBudget`, `githubIssueCommentRunes`, and `githubIssueMaxComments`.

The dispatcher is `callMainBotGitLab`. In `agent_tool_mcp.go` `dispatch`, add `if tool.Kind == "gitlab"` immediately after the `github` branch and call `h.callMainBotGitLab`. Do not send kind `gitlab` into the GitHub function. Results pass through `redactAgentToolResult`.

- [ ] **Step 1: Write the failing test**

Session mode assistant, project with one `gitlab` repository and one `github` repository: five tools, every kind `gitlab`. A project with only GitHub: zero GitLab tools. `issues_list` against the fake server hits `/api/v4/projects/<escaped>/issues` with the requester's token, not the project owner's token. Naming the GitHub repository returns an error and zero HTTP calls. A second page of results sets `hasMore` true when `limit` is 1. Pipeline 500 on `mr_status` yields `checksState` `unknown` and a warning, and the MR state is still returned.

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/httpapi -run '^TestGitLabAssistantRead$' -count=1`

Expected: FAIL to compile, `undefined: mainBotGitLabTools`.

- [ ] **Step 3: Implement the read tools and the catalog gate**

Do not register the write tools in this task. The dispatcher returns `errAgentToolUnknown` for any other `workbench_gitlab_*` name until Task 11.

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test ./internal/httpapi -run '^TestGitLabAssistantRead$|TestMainBotGitHubToolsOnlyForAssistantWithProject$' -count=1`

Expected: PASS. GitHub tools are unchanged.

- [ ] **Step 5: Commit**

```bash
git add internal/httpapi/agent_tool_gitlab.go internal/httpapi/agent_tool_gitlab_test.go internal/httpapi/agent_tool_catalog.go internal/httpapi/agent_tool_mcp.go
git commit -m "feat(gitlab): list and read issues and merge requests from the assistant"
```

---

### Task 11: Write comments, labels, reviews, merges, and closes

**Files:**
- Create: `internal/httpapi/agent_tool_gitlab_write.go`
- Modify: `internal/httpapi/agent_tool_gitlab.go` (register the rest of the tools and dispatch them)
- Test: `internal/httpapi/agent_tool_gitlab_write_test.go`

**Interfaces:**
- Consumes: Task 10 resolver and dispatcher. `githubWriteAllowed` stays GitHub-only. Add `gitlabWriteAllowed` with the same rule: `ExecMode.AutoWrite` allows the call; otherwise the error names the tool and tells the user to switch to 「完全权限」 or 「帮我批准」.
- Produces these additional tools, kind `gitlab`:

```text
workbench_gitlab_comment_add       target, number, body
workbench_gitlab_labels_add        target, number, labels
workbench_gitlab_mr_review         number, event APPROVE|REQUEST_CHANGES|COMMENT, body
workbench_gitlab_mr_merge          number, method merge|squash, sha, commitMessage
workbench_gitlab_issue_close       number, comment
workbench_gitlab_issue_create      title, body, labels
workbench_gitlab_mr_close          number, comment
```

`target` is required and is `issue` or `merge_request`. Comment posts `POST /projects/:id/<issues|merge_requests>/:iid/notes` with `body`. Labels: `GET /projects/:id/labels` first; any name not in that list returns an error and does not PUT. Then PUT the issue or merge request with `add_labels` joined by commas.

`APPROVE` posts `/merge_requests/:iid/approve`. `COMMENT` posts a note and does not approve. `REQUEST_CHANGES` requires `body`, posts a note, and when `approved_by` contains the username from `GET /user`, posts `/merge_requests/:iid/unapprove`. Merge PUTs `/merge` with `sha` when provided, `squash: true` only for method `squash`, and `should_remove_source_branch: false`. Method `rebase` returns an error before HTTP. Issue create POSTs `/issues`. Labels on create use the same pre-check. Issue close PUTs `state_event=close` and does not send a reason field. An optional comment is a note posted first. An already closed issue returns `alreadyClosed: true` and does not PUT. MR close refuses `state=="merged"` with an error that contains `已经合并`. An already closed MR returns `alreadyClosed: true`.

Review, merge, issue close, and MR close call `gitlabWriteAllowed` first. Comment, labels, and issue create call the same reviewer hook GitHub uses (`reviewGitHubWrite` or a sibling that passes the GitLab action text) when `ExecMode.ReviewWrites` is set. 「完全权限」 (`AutoWrite` true, `ReviewWrites` false) does not call the reviewer.

- [ ] **Step 1: Write the failing test**

Against one fake server: comment with `target=issue` posts to `/issues/3/notes` and comment without `target` errors before HTTP. Labels `["missing"]` errors before PUT. `APPROVE` hits `/approve`. `REQUEST_CHANGES` posts a note and `/unapprove` when `/user` username is in `approved_by`. `method=rebase` errors before HTTP. Merge sends `sha` and `should_remove_source_branch: false`. Issue close body has `state_event` and no `state_reason`. Session with `AutoWrite` false rejects merge and contains `请求批准`. The tool result for a 401 body that echoes the token does not contain the token.

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/httpapi -run '^TestGitLabAssistantWrite$' -count=1`

Expected: FAIL. The write tool names are absent, or dispatch returns `errAgentToolUnknown`.

- [ ] **Step 3: Implement the write tools**

Add them to `mainBotGitLabTools`. Keep the catalog gate from Task 10 so a project without a GitLab repository still hides them.

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test ./internal/httpapi -run '^TestGitLabAssistant' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/httpapi/agent_tool_gitlab.go internal/httpapi/agent_tool_gitlab_write.go internal/httpapi/agent_tool_gitlab_write_test.go
git commit -m "feat(gitlab): let the assistant comment, review, and merge"
```

---

### Task 12: Show the site, the sync action, and the GitLab source

**Files:**
- Modify: `web/app/workbench.js` (`CREDENTIAL_KINDS` and the credential save payload)
- Modify: `web/index.html` (source filter near the issue list, schedule `<select name="source">`, repository dialog hint)
- Modify: `web/app/sync-schedule.js` (`sourceBadge`, the summary counts, the schedule source check, the sync button enablement)
- Modify: `web/app/issue-drawer.js` (the source label near line 258, `conversationRepoPlatform`, the conversation card matcher)
- Modify: `web/app.js` (the `#importGitHubIssues` click wiring)
- Modify: `web/app/workbench.js` activity feed source label near the sync log renderer
- Modify: `web/i18n-en.js`
- Test: `web/gitlab_source_test.js` plus the Go wrapper pattern in `web/issue_clarify_draft_test.go` if that is how JS tests run; otherwise extend the nearest existing web Go test that executes `web/app` scripts.

**Interfaces:**
- Consumes: `PUT /api/users/:id/credentials` with `gitLabBaseUrl` and `gitLabToken`. `gitLabClear: true` clears both. The public user has `gitLabBaseUrl` and `gitLabTokenConfigured`.
- Produces: a credential row named `GitLab` beside GitHub. Its extra field is the site (`name=gitLabBaseUrl`, placeholder `https://git.example.com`). The secret is the token. The guide says to create a token with the `api` scope. The apply link, once a site is present, is `{site}/-/user_settings/personal_access_tokens`. Saving sends both fields. The row does not write `gitToken`.

Repository dialog hint becomes: `粘贴 GitHub 地址，或与项目所有者已保存的 GitLab 站点同主机的地址。其他 SSH 主机仍只做克隆。`

Issue source filter gains `<option value="gitlab">GitLab</option>`. Schedule source gains `<option value="gitlab">只同步 GitLab</option>`. `fillScheduleJobForm` accepts `source==='gitlab'`. `sourceBadge` returns `<span class="sync-badge local">GitLab</span>` for `gitlab`. The sync summary counts GitLab issues. Add `<button id="importGitLabIssues">` beside `#importGitHubIssues`, labeled `同步 GitLab`. In `web/app.js`, set its `onclick` to the same `syncNow` the GitHub button uses. `PullAll` already pulls every configured source, so this button does not get its own endpoint. Include `gitlab` in the summary, the failure toast, and the workbench activity feed wherever `github` / `gongfeng` / `tapd` are named. The issue detail source at `issue-drawer.js` maps `issue.source==='gitlab'` to `GitLab` instead of falling through to `LoopForge`. `closedTrackerHint` maps `gitlab` to `GitLab`.

`conversationRepoPlatform` returns `GitLab` when the repository provider is `gitlab` or the stored project id starts with `gitlab:`. It returns `GitHub` for `github.com` and `工蜂` only for the remaining Gongfeng URLs. Card matching compares the path after the host in `gitlab:<host>/<path>` with the web URL path, not the whole identity string.

Every new Chinese string in `web/` gets an English entry in `web/i18n-en.js`.

- [ ] **Step 1: Write the failing test**

Load the credential kind list and assert a kind whose key is `gitLab` and whose save payload keys are `gitLabBaseUrl` and `gitLabToken`. Assert the source filter HTML contains `value="gitlab"`. Assert `sourceBadge({source:'gitlab'})` contains `GitLab` and does not contain `工蜂`. Assert a conversation card for project `gitlab:git.example.com/team/app` and web URL `https://git.example.com/team/app` links the merge request instead of dropping it, and the platform label is `GitLab`.

- [ ] **Step 2: Run the test and confirm it fails**

Run the Go wrapper the nearest web test uses, for example `go test ./web -run '^TestGitLabSource$' -count=1`.

Expected: FAIL. The credential kind is missing, or the badge falls through to empty.

- [ ] **Step 3: Add the row, the filter, the badge, and the card match**

Reuse the existing credential card markup. Do not add a new stylesheet. Do not add a GitLab OAuth button.

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test ./web -run '^TestGitLabSource$' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/app/workbench.js web/app.js web/index.html web/app/sync-schedule.js web/app/issue-drawer.js web/i18n-en.js web/gitlab_source_test.js web/gitlab_source_test.go
git commit -m "feat(gitlab): show the site, sync action, and source in the existing screens"
```

---

### Task 13: Opt-in live check and the default suite

**Files:**
- Create: `internal/tracker/gitlab_live_test.go`

**Interfaces:**
- Consumes: `pullGitLabIssues`, `gitlabMergeBranch`, `ParseGitLabRepositoryURL`.
- Produces: `TestGitLabLiveImportAndDelivery`. It skips unless `AGENTFLOW_GITLAB_LIVE_WRITE=1`. It reads `AGENTFLOW_GITLAB_LIVE_REPO` as `group/project` and `AGENTFLOW_GITLAB_LIVE_SITE` as the site origin, plus a token file path in `AGENTFLOW_GITLAB_LIVE_TOKEN_FILE`. It refuses to run when the repo path is empty. It creates two throwaway branches, opens one merge request, merges that merge request, and asserts the default branch name is unchanged. It does not merge into the default branch.

- [ ] **Step 1: Write the skipped test**

Copy the guard style of `TestGitHubLiveImportAndDelivery`. The first lines skip when the write env is not `1`.

- [ ] **Step 2: Run it and confirm it skips**

Run: `go test ./internal/tracker -run '^TestGitLabLiveImportAndDelivery$' -count=1`

Expected: PASS with `--- SKIP`.

- [ ] **Step 3: Run the default GitLab tests together**

Run: `go test ./internal/domain ./internal/repo ./internal/httpapi ./internal/tracker ./internal/engine ./web -count=1 -timeout 4m`

Expected: PASS. The live test skips. No test dials a real GitLab host.

- [ ] **Step 4: Commit**

```bash
git add internal/tracker/gitlab_live_test.go
git commit -m "test(gitlab): add an opt-in live import that stays off the default branch"
```

---

## Self-review

Spec coverage:

- Identity, host key, and clone URL: Task 1 and Task 5.
- Separate token and public flag: Task 3.
- Verify before save, no path on the site, no redirect: Task 4.
- Owner-host recognition, GitHub and SSH unchanged: Task 5.
- Owner token for sync and merge requests, 100-page cap, local id, clarification keep, schedule source: Task 6 and Task 7.
- Find, create, merge, `!iid`, delivery states, skip Gongfeng promotion: Task 8.
- Executor token, mixed run, host mismatch, GitLab auth hint: Task 9.
- Assistant read and write tools, gates, label pre-check, rebase refusal, redaction: Task 10 and Task 11.
- Screens, issue-detail source name, sync summary, and English strings: Task 12.
- Manual SQL plus AutoMigrate, live env names: Task 3 and Task 13.

No task says "implement later" or "same as GitHub" without naming the function and the HTTP call. `explainGitAuthFailure` is left unchanged so the existing GitHub hint test stays valid.
