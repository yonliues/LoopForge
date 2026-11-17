package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"agentflow/internal/domain"
	"agentflow/internal/repo"
)

func TestGitLabAssistantRead(t *testing.T) {
	h, owner := workbenchFixture(t)
	if err := h.repo.PutUser(domain.User{LoginName: "gitlab-requester"}); err != nil {
		t.Fatal(err)
	}
	requester, _ := h.repo.UserByLoginName("gitlab-requester")
	var hits atomic.Int32
	var token string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		token = r.Header.Get("PRIVATE-TOKEN")
		if r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected authorization %q", r.Header.Get("Authorization"))
		}
		if !strings.Contains(r.URL.EscapedPath(), "/projects/team%2Fapp/") {
			t.Errorf("path %s", r.URL.EscapedPath())
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/pipelines"):
			http.Error(w, "pipeline down", http.StatusInternalServerError)
		case strings.HasSuffix(r.URL.Path, "/issues"):
			_, _ = w.Write([]byte(`[{"iid":1,"title":"One","state":"opened"},{"iid":2,"title":"Two","state":"opened"}]`))
		case strings.Contains(r.URL.Path, "/merge_requests/7"):
			_, _ = w.Write([]byte(`{"iid":7,"title":"Change","state":"opened","source_branch":"feature","target_branch":"main","sha":"abc","merge_status":"can_be_merged","author":{"username":"ada"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	identity := gitlabToolIdentity(t, srv.URL, "team/app")
	for _, userID := range []int64{owner.ID, requester.ID} {
		tok := "glpat-owner"
		if userID == requester.ID {
			tok = "glpat-requester"
		}
		if _, err := h.repo.UpdateUserCredentials(userID, repo.UserCredentialUpdate{GitLabBaseURL: srv.URL, GitLabToken: tok}); err != nil {
			t.Fatal(err)
		}
	}
	requester, _ = h.repo.User(requester.ID)
	if err := h.repo.PutRepository(domain.Repository{ID: "gl-app", ProjectID: "workbench", Provider: "gitlab", Name: "app", GongfengProjectID: identity, CloneURL: srv.URL + "/team/app.git", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.PutRepository(domain.Repository{ID: "gh-app", ProjectID: "workbench", Provider: "github", Name: "hub", GongfengProjectID: "github:octo/app", CloneURL: "https://github.com/octo/app.git", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.PutProject(domain.Project{ID: "gh-only", Name: "GitHub only", OwnerID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.PutRepository(domain.Repository{ID: "gh-only-repo", ProjectID: "gh-only", Provider: "github", Name: "only", GongfengProjectID: "github:octo/only", CloneURL: "https://github.com/octo/only.git"}); err != nil {
		t.Fatal(err)
	}
	gw := &agentToolGateway{deps: h.deps}
	session := &AgentToolSession{Mode: AgentToolAssistant, ProjectID: "workbench", UserID: requester.ID, User: requester}
	tools := gw.buildCatalog(t.Context(), session)
	var gitlabTools, githubTools int
	for _, tool := range tools {
		if strings.HasPrefix(tool.Name, "workbench_gitlab_") {
			gitlabTools++
			if tool.Kind != "gitlab" {
				t.Fatalf("kind %s", tool.Kind)
			}
		}
		if tool.Kind == "github" {
			githubTools++
		}
	}
	if gitlabTools != 5 || githubTools == 0 {
		t.Fatalf("gitlab tools %d github tools %d", gitlabTools, githubTools)
	}
	only := gw.buildCatalog(t.Context(), &AgentToolSession{Mode: AgentToolAssistant, ProjectID: "gh-only", UserID: requester.ID, User: requester})
	for _, tool := range only {
		if tool.Kind == "gitlab" {
			t.Fatalf("github project exposed %s", tool.Name)
		}
	}

	before := hits.Load()
	if _, err := gw.dispatch(t.Context(), session, agentToolDescriptor{Name: mainBotGitLabIssuesList, Kind: "gitlab"}, map[string]any{"repository": "hub", "limit": 1}); err == nil {
		t.Fatal("github repository was accepted")
	}
	if hits.Load() != before {
		t.Fatalf("http calls %d", hits.Load()-before)
	}
	text, err := gw.dispatch(t.Context(), session, agentToolDescriptor{Name: mainBotGitLabIssuesList, Kind: "gitlab"}, map[string]any{"repository": "app", "limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	if token != "glpat-requester" {
		t.Fatalf("token %q", token)
	}
	var listed struct {
		HasMore bool `json:"hasMore"`
	}
	if err := json.Unmarshal([]byte(text), &listed); err != nil || !listed.HasMore {
		t.Fatalf("list %s %v", text, err)
	}
	text, err = gw.dispatch(t.Context(), session, agentToolDescriptor{Name: mainBotGitLabMRStatus, Kind: "gitlab"}, map[string]any{"repository": "app", "number": 7})
	if err != nil {
		t.Fatal(err)
	}
	var status struct {
		State          string   `json:"state"`
		ChecksState    string   `json:"checksState"`
		ChecksWarnings []string `json:"checksWarnings"`
	}
	if err := json.Unmarshal([]byte(text), &status); err != nil {
		t.Fatal(err)
	}
	if status.State != "opened" || status.ChecksState != "unknown" || len(status.ChecksWarnings) == 0 {
		t.Fatalf("status %+v", status)
	}
}

func gitlabToolIdentity(t *testing.T, site, path string) string {
	t.Helper()
	u, err := url.Parse(site)
	if err != nil {
		t.Fatal(err)
	}
	host := u.Hostname()
	if port := u.Port(); port != "" && port != "80" && port != "443" {
		host += ":" + port
	}
	return "gitlab:" + host + "/" + path
}
