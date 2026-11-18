# Design: self-hosted GitLab alongside GitHub

Date: 2026-11-17

## Intent

Add self-hosted GitLab as a hosting provider with the same product surface GitHub already has: bind a repository, clone and push with a personal access token, pull issues, find or create and merge merge requests, track delivery state, and expose the matching assistant tools.

GitHub code stays in place. GitLab is a parallel `gitlab` provider, not a shared hosting interface. Gongfeng stays on its MCP path. One LoopForge user stores one GitLab site and one token. A deployment can still hold projects whose owners use different sites, because the site is part of each repository's identity.

## Goals

- Recognize a GitLab repository only when its host matches the project owner's saved GitLab site.
- Store the token apart from the existing GitHub `GitToken`.
- Pull issues and drive merge-request delivery the way GitHub pull requests are driven.
- Offer `workbench_gitlab_*` assistant tools on projects that have a GitLab repository.
- Show the site, token, sync entry, and source labels on the existing personal-settings, repository, and sync screens.

## Non-goals

- No GitLab OAuth app, device flow, or account login.
- No second GitLab site on the same user. Saving a new site and token replaces the previous pair.
- No change to GitHub behavior, Gongfeng, or SSH hosts that are not the owner's GitLab site.
- No push of locally created work items to GitLab. Sync only pulls.
- No rebase merge, no skipping pipeline or approval checks, no deleting issues or merge requests.
- No "skip TLS verify" and no private CA upload.
- No new table and no startup data migration.

## Repository identity and credentials

`provider` is `gitlab`. The remote identity stays in the existing `GongfengProjectID` field. Its form is `gitlab:<host>[:port]/<group>/<project>`, for example `gitlab:git.example.com/platform/api` or `gitlab:git.example.com:8443/platform/api`. Nested groups are part of the path. The host is included so `group/repo` on two sites does not collide.

The host is lowercase. Default ports are omitted: 443 for https and 80 for http. Any other port is kept. Each path segment matches `[A-Za-z0-9_.-]+`, there are at least two segments, and `.` and `..` are rejected. Userinfo, query strings, and fragments are rejected.

Web and clone URLs are derived from that identity. The clone URL is HTTPS without a token, including when the user pasted an SSH URL on that host. The scheme of the stored web URL follows the project owner's saved site URL.

Each user gains two fields beside `GitToken`: `GitLabBaseURL` (JSON `gitLabBaseUrl`, the site root such as `https://git.example.com`, not `/api/v4`) and `GitLabToken` (JSON `gitLabToken`). Public user payloads include the site and `gitLabTokenConfigured`, never the token. API calls go to `{site}/api/v4` with the token in the `PRIVATE-TOKEN` header. The project path is URL-encoded.

A pasted URL becomes `provider=gitlab` only when its host and port equal the project owner's saved site. HTTPS, HTTP, and SSH on that host all qualify. Any other host keeps today's SSH behavior, so Gitea and Gogs stay transport-only. If the owner has not saved a site yet, saving the repository fails and tells the user to fill in the GitLab site and token under personal settings first. There is no provider dropdown.

Before anyone's token is used on a repository, that repository's host must equal that user's saved site. A mismatch fails before the request, and the token is not sent.

Token choice matches GitHub:

- Issue sync and merge-request delivery use the project owner's token.
- Clone and push use the executing user's token.
- Assistant tools use the requester's token.

## Issue sync and merge requests

Full sync and a schedule job whose source is `gitlab` pull every `provider=gitlab` repository. The call is `GET /api/v4/projects/<path>/issues?state=all`, 100 items per page, at most 100 pages. Past that, that repository's sync fails. GitLab's issue list does not include merge requests, so nothing is filtered out as a pull request.

Stored issues follow the GitHub rules. The local id is `gl-<repository id>-<iid>`, the source is `gitlab`, `opened` becomes `todo`, and `closed` becomes closed with an external close source. A body that already has clarification, or that was edited locally, is kept. Sync does not create GitLab issues for local work items.

Merge-request delivery follows the GitHub pull-request path, not Gongfeng's baseline promotion or "commit is on the default branch" check. Look up by source branch and keep only the most recently updated match. An already merged match returns `!iid` and does not create another. An open match is merged. A missing match, or a closed unmerged match, is not reused: create a new merge request when a target is present. Merge with a merge commit and keep the source branch. The same source and target is a no-op. No target and no open match returns the existing "no open MR" result.

The web URL is the `web_url` GitLab returns. If that is missing and the repository record is available, build `{webUrl}/-/merge_requests/<iid>` from the stored web URL. If only the identity string is available, use `https://<host>/<path>/-/merge_requests/<iid>`.

Delivery state is only `opened`, `merged`, or `closed`. Refresh rows whose project starts with `gitlab:` through the GitLab API. Those rows skip both Gongfeng promotion scanning and the default-branch commit check. When GitLab refuses the merge because of a pipeline, an approval, or a conflict, return that error and stop.

## Assistant tools

Add a parallel set named `workbench_gitlab_*` with kind `gitlab`. Do not change the GitHub tools. Register the GitLab tools only for a normal Main Bot session whose current project has at least one `provider=gitlab` repository.

The tools use the requester's token and only repositories bound to the current project whose host matches that user's saved site. The token stays on the server. Tool text is redacted. There is no delete tool.

Approval matches GitHub. Comments, labels, and issue creation go through the independent reviewer when the session is in 「帮我批准」. Review, merge, and close do not run in 「请求批准」, because those actions have no draft to confirm. 「完全权限」 runs the write without that review.

The set is:

- List and read issues. List merge requests. Read merge-request status and changed files.
- Create an issue. Close an issue. Close a merge request without merging.
- Comment, add labels, submit a review, and merge.

Three GitLab differences are explicit:

- Issue iid and merge-request iid are separate sequences. Comment and label tools require `target`: `issue` or `merge_request`. Close-issue and close-merge-request stay separate tools.
- Labels are checked against labels that already exist on the project. Unknown labels are rejected before the write. GitLab would otherwise create them.
- `APPROVE` calls the approve API. `COMMENT` posts a note only. `REQUEST_CHANGES` posts a note and, when this token has already approved, removes that approval. GitLab has no changes-requested state. Merge accepts `merge` and `squash` only; `rebase` is rejected before the call. The supplied `sha` is sent through, and a changed head fails at GitLab. Merge does not delete the source branch. Issue close sends no `completed` / `not_planned` reason; an optional comment carries the explanation.

Lists report `hasMore`. Bodies and diffs use the same truncation budgets as the GitHub tools. A failed pipeline read is an unknown check with a reason, not a passed check.

## Run credentials and screens

Each repository on a run carries its own token. A `gitlab` repository gets the executing user's GitLab token. A `github` repository still gets `GitToken`. An SSH repository still gets none. Tokens are not swapped across hosts. A host mismatch fails before clone. A missing or rejected GitLab token points at "Personal settings → GitLab", not the GitHub hint. The token exists only on the invocation sent to the runner. API responses and repository records do not keep it.

Personal settings gains a GitLab credential next to GitHub. The user enters the site root and the personal access token together. Saving calls `{site}/api/v4/user` first and writes nothing when that call fails. The hint tells the user to create a token with the `api` scope. After a site is entered, the apply link is `{site}/-/user_settings/personal_access_tokens`. A new pair replaces the old one. Clearing removes both.

The repository dialog stays a paste field. The hint states the recognition rule: a GitHub URL stays GitHub, a URL on the project owner's saved GitLab site becomes GitLab, and any other host stays Git SSH.

The sync screen gains a GitLab source, count, badge, and "Sync GitLab" action. Schedule jobs can use source `gitlab`. The issue list filter and the issue detail name say GitLab. Branch and merge-request links keep using the stored web URL, which already uses GitLab's `/-/` paths for non-GitHub hosts. Conversation repository cards match the path inside `gitlab:<host>/<path>`, not the whole identity, and they label that repository GitLab rather than Gongfeng.

## Failures and token handling

Self-hosted sites are often on a private network, so a private host is allowed. The token is sent only to the saved site. Any redirect stops the call; the token is not forwarded.

The site must be an `http` or `https` origin with no userinfo, path, query, or fragment. A value that includes `/api/v4` is rejected so calls do not land on `/api/v4/api/v4`. TLS uses the system trust store. An untrusted certificate fails with a certificate error.

`401` and `403` tell the user to check the GitLab token under personal settings: invalid, expired, or missing the `api` scope. Sync logs, API responses, and assistant results never contain the token. GitLab error text is redacted before display.

One repository's sync failure is logged on that repository and the loop continues. The overall sync is failed if any repository failed. Passing 100 pages fails that repository. Verify calls time out at 10 seconds. Sync and merge-request calls time out at 30 seconds.

Assistant tools return before calling GitLab when the repository is outside the project, the host does not match, `target` does not match the numbered issue or merge request, the label does not exist, the merge method is `rebase`, or review, merge, or close was asked in 「请求批准」.

## Data and tests

AutoMigrate adds the two user columns when the process connects. There is no startup migration and no new table. `GitToken` is unchanged.

The default suite talks to a local fake GitLab API, not a real site. It covers:

- Parsing HTTPS, HTTP, and SSH on the owner's host, including subgroups, a non-default port, `.git`, and `/-/` subpages, into `gitlab:<host>/<path>`. Other hosts are not GitLab. Userinfo, `..`, and query strings are rejected.
- Credentials: `/user` must succeed before save. The public user has the site and the configured flag, and an empty token. A failed check writes nothing. A site with a path is rejected. Clearing removes both fields. Saving GitLab credentials leaves `GitToken` unchanged.
- Repository save: a matching host stores `provider=gitlab` and an HTTPS clone URL without a token. No saved owner site returns 400. Other hosts stay SSH. `github.com` stays GitHub.
- Sync: `opened` / `closed`, id `gl-<repository>-<iid>`, preserved body and clarification, the 100-page failure, and continuation after one repository fails. The call uses the project owner's token. A host mismatch sends no request.
- Merge requests: lookup by source branch keeps the most recently updated match, merges it when it is open, returns `!iid` when it is already merged, and creates a new one when the match is missing or closed and unmerged. Merge uses a merge commit and keeps the source branch. The same branch is a no-op. No target and no open match returns the existing "no open MR" error. A pipeline or approval refusal is returned as-is. Delivery state is only `opened`, `merged`, or `closed`. A `gitlab:` row refreshes through GitLab and skips Gongfeng promotion and the default-branch commit check.
- Assistant tools: absent when the project has no GitLab repository. Comments and labels require `target`. Unknown labels, `rebase`, and review, merge, or close under 「请求批准」 are rejected before the call. `APPROVE` hits the approve API. `REQUEST_CHANGES` posts a note and removes an existing approval from this token. `sha` is forwarded. Results contain no token. The call uses the requester's token.
- Runs: a mixed run puts the GitLab token only on GitLab repositories, `GitToken` only on GitHub repositories, and no token on SSH. A host mismatch fails before clone. The auth failure names the GitLab personal setting.

One live test stays skipped unless `AGENTFLOW_GITLAB_LIVE_WRITE=1` and `AGENTFLOW_GITLAB_LIVE_REPO` name a disposable repository. It must not merge into the default branch. The default suite does not run it.
