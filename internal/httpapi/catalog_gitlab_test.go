package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"agentflow/internal/domain"
	"agentflow/internal/repo"
)

func TestSaveGitLabRepository(t *testing.T) {
	f := cvBNewHTTPFixture(t)
	if _, err := f.s.UpdateUserCredentials(f.user.ID, repo.UserCredentialUpdate{GitLabBaseURL: "https://git.example.com", GitLabToken: "glpat-secret"}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.PutProject(domain.Project{ID: "gl-proj", Key: "GL", Name: "GitLab", OwnerID: f.user.ID}); err != nil {
		t.Fatal(err)
	}
	resp := cvBRequest(t, f, http.MethodPost, "/api/repositories", map[string]string{"projectId": "gl-proj", "repoUrl": "git@git.example.com:platform/api.git"}, true)
	var repository domain.Repository
	if err := json.Unmarshal(resp.Body.Bytes(), &repository); err != nil || resp.Code != http.StatusOK {
		t.Fatalf("save: HTTP %d %s", resp.Code, resp.Body.String())
	}
	if repository.Provider != "gitlab" || repository.GongfengProjectID != "gitlab:git.example.com/platform/api" || repository.CloneURL != "https://git.example.com/platform/api.git" || strings.Contains(repository.CloneURL, "glpat") {
		t.Fatalf("saved %+v", repository)
	}

	if _, err := f.s.UpdateUserCredentials(f.user.ID, repo.UserCredentialUpdate{GitLabClear: true}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.PutProject(domain.Project{ID: "plain-proj", Key: "PL", Name: "Plain", OwnerID: f.user.ID}); err != nil {
		t.Fatal(err)
	}
	missing := cvBRequest(t, f, http.MethodPost, "/api/repositories", map[string]string{"projectId": "plain-proj", "repoUrl": "https://gitlab.internal.example/platform/api"}, true)
	if missing.Code != http.StatusBadRequest || !strings.Contains(missing.Body.String(), "个人设置") {
		t.Fatalf("missing site: HTTP %d %s", missing.Code, missing.Body.String())
	}
	github := cvBRequest(t, f, http.MethodPost, "/api/repositories", map[string]string{"projectId": "plain-proj", "repoUrl": "https://github.com/octo/widgets"}, true)
	var gh domain.Repository
	if err := json.Unmarshal(github.Body.Bytes(), &gh); err != nil || github.Code != http.StatusOK || gh.Provider != "github" {
		t.Fatalf("github: HTTP %d %s", github.Code, github.Body.String())
	}
	ssh := cvBRequest(t, f, http.MethodPost, "/api/repositories", map[string]string{"projectId": "plain-proj", "repoUrl": "git@gitea.example.com:org/app.git"}, true)
	var remote domain.Repository
	if err := json.Unmarshal(ssh.Body.Bytes(), &remote); err != nil || ssh.Code != http.StatusOK || remote.Provider != domain.RepositoryProviderSSH {
		t.Fatalf("ssh: HTTP %d %s", ssh.Code, ssh.Body.String())
	}

	if _, err := f.s.UpdateUserCredentials(f.user.ID, repo.UserCredentialUpdate{GitLabBaseURL: "https://git.example.com", GitLabToken: "glpat-secret"}); err != nil {
		t.Fatal(err)
	}
	other := cvBRequest(t, f, http.MethodPost, "/api/repositories", map[string]string{"projectId": "gl-proj", "repoUrl": "https://gitea.example.com/org/app"}, true)
	if other.Code != http.StatusBadRequest || strings.Contains(other.Body.String(), `"provider":"gitlab"`) {
		t.Fatalf("other host: HTTP %d %s", other.Code, other.Body.String())
	}
}
