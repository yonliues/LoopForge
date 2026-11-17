package engine

import (
	"errors"
	"strings"
	"testing"

	"agentflow/internal/domain"
)

func TestBindRunnerRepositoriesSeparatesGitLabToken(t *testing.T) {
	run := domain.Run{Repositories: []domain.RepositoryContext{
		{RepositoryID: "gh", Provider: "github", Name: "app", GongfengProjectID: "github:octo/app", CloneURL: "https://github.com/octo/app.git"},
		{RepositoryID: "gl", Provider: "gitlab", Name: "app", GongfengProjectID: "gitlab:git.example.com/team/app", CloneURL: "https://git.example.com/team/app.git"},
		{RepositoryID: "ssh", Provider: domain.RepositoryProviderSSH, Name: "mirror", CloneURL: "git@git.example.com:team/mirror.git"},
	}}
	invocation := &domain.RunnerInvocation{}
	if err := bindRunnerRepositories(invocation, run, "ghp", "glpat", "owner", "owner@example.com"); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, repo := range invocation.Repositories {
		got[repo.RepositoryID] = repo.GitToken
		if repo.RepositoryID == "ssh" && !repo.UseSSH {
			t.Fatal("ssh repository lost UseSSH")
		}
	}
	if got["gh"] != "ghp" || got["gl"] != "glpat" || got["ssh"] != "" {
		t.Fatalf("tokens %+v", got)
	}

	denied := &domain.RunnerInvocation{}
	err := bindRunnerRepositories(denied, run, "ghp", "", "owner", "owner@example.com")
	if err == nil || !strings.Contains(err.Error(), "个人配置 → GitLab") {
		t.Fatalf("empty token: %v", err)
	}
	if denied.Repository != nil || len(denied.Repositories) != 0 {
		t.Fatalf("tokens copied on failure: %+v", denied)
	}

	mismatch := domain.Run{Repositories: []domain.RepositoryContext{{
		RepositoryID: "gl", Provider: "gitlab", Name: "app",
		GongfengProjectID: "gitlab:git.example.com/team/app",
		CloneURL:          "https://evil.example/team/app.git",
	}}}
	blocked := &domain.RunnerInvocation{}
	err = bindRunnerRepositories(blocked, mismatch, "ghp", "glpat", "", "")
	if err == nil || !strings.Contains(err.Error(), "主机") {
		t.Fatalf("mismatch: %v", err)
	}
	if blocked.Repository != nil || len(blocked.Repositories) != 0 || blocked.GitUserName != "" {
		t.Fatalf("invocation changed: %+v", blocked)
	}

	raw := errors.New("repository checkout: git clone https://git.example.com/team/app.git: exit status 128 (fatal: Authentication failed)")
	explained := explainGitLabAuthFailure(raw, false)
	if !strings.Contains(explained.Error(), "个人配置 → GitLab") || strings.Contains(explained.Error(), "github.com/settings/tokens") {
		t.Fatalf("gitlab hint: %v", explained)
	}
	if !strings.Contains(explainGitAuthFailure(raw, false).Error(), "github.com/settings/tokens") {
		t.Fatal("github hint changed")
	}
}
