"use strict";
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const root = path.join(__dirname);
const page = fs.readFileSync(path.join(root, "index.html"), "utf8");
assert.match(page, /<option value="gitlab">GitLab<\/option>/);
assert.match(page, /id="importGitLabIssues"/);
assert.match(page, /只同步 GitLab/);
assert.match(page, /粘贴 GitHub 地址，或与项目所有者已保存的 GitLab 站点同主机的地址。其他 SSH 主机仍只做克隆。/);

const workbench = fs.readFileSync(path.join(root, "app/workbench.js"), "utf8");
const credential = vm.createContext({
  document: { addEventListener() {} },
  console,
});
vm.runInContext(workbench, credential);
const kind = credential.credentialKind("gitLab");
assert.equal(kind && kind.key, "gitLab");
const payload = credential.credentialSavePayload(kind, { secret: "glpat-test", extra: "https://git.example.com" });
assert.deepEqual(Object.keys(payload).sort(), ["gitLabBaseUrl", "gitLabToken"]);
assert.equal(payload.gitLabToken, "glpat-test");
assert.equal(payload.gitLabBaseUrl, "https://git.example.com");
assert.equal(payload.gitToken, undefined);
assert.equal(credential.gitLabTokenApplyURL("https://git.example.com"), "https://git.example.com/-/user_settings/personal_access_tokens");
assert.deepEqual(JSON.parse(JSON.stringify(credential.credentialClearPayload(kind))), { gitLabClear: true });

const schedule = fs.readFileSync(path.join(root, "app/sync-schedule.js"), "utf8");
const badgeStart = schedule.indexOf("function sourceBadge");
const badgeEnd = schedule.indexOf("function externalLink");
const badgeCtx = vm.createContext({});
vm.runInContext(schedule.slice(badgeStart, badgeEnd), badgeCtx);
const badge = badgeCtx.sourceBadge({ source: "gitlab" });
assert.match(badge, /GitLab/);
assert.doesNotMatch(badge, /工蜂/);

const drawer = fs.readFileSync(path.join(root, "app/issue-drawer.js"), "utf8");
function grab(source, name) {
  const start = source.search(new RegExp("^(?:async )?function " + name + "\\(", "m"));
  assert.ok(start >= 0, name);
  const rest = source.slice(start);
  const next = /\n(?:async )?function \w+\(/.exec(rest);
  return next ? rest.slice(0, next.index) : rest;
}
const cardCtx = vm.createContext({
  URL,
  escapeHTML: value => String(value ?? ""),
  safeURL: value => String(value || ""),
  deliveryMRStateText: { opened: "待合入" },
  conversationWorkspaceBranches() { return []; },
});
for (const name of ["gongfengRefPath", "conversationRepoIsGitHub", "conversationProjectPath", "conversationRepoPlatform", "gongfengBranchLinks", "conversationBranchList", "conversationRepoCards"]) {
  vm.runInContext(grab(drawer, name), cardCtx);
}
vm.runInContext(grab(fs.readFileSync(path.join(root, "app/issue-model.js"), "utf8"), "deliveryMRList"), cardCtx);
const html = cardCtx.conversationRepoCards({
  deliveryMrs: [{ project: "gitlab:git.example.com/team/app", url: "https://git.example.com/team/app/-/merge_requests/9", state: "opened", iid: 9, repository: "app" }],
}, { requirement: { conversation: { branches: [{ repository: "app", webUrl: "https://git.example.com/team/app", workBranch: "feature", baseBranch: "main" }] } } });
assert.equal((html.match(/class="conv-repo"/g) || []).length, 1);
assert.match(html, /merge_requests\/9/);
assert.match(html, /GitLab/);
assert.doesNotMatch(html, /工蜂/);
assert.equal(cardCtx.conversationRepoPlatform("https://github.com/octo/app"), "GitHub");
assert.equal(cardCtx.conversationRepoPlatform("https://git.example.com/team/app", "gitlab:git.example.com/team/app"), "GitLab");
