package tracker

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"agentflow/internal/domain"
	"agentflow/internal/repo"
)

func TestGitLabClientSendsTokenToTheOwnerSite(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("PRIVATE-TOKEN") != "glpat-secret" || r.Header.Get("Authorization") != "" {
			t.Errorf("headers token=%q auth=%q", r.Header.Get("PRIVATE-TOKEN"), r.Header.Get("Authorization"))
		}
		if r.URL.EscapedPath() != "/api/v4/projects/team%2Fapp/issues" {
			t.Errorf("path %s", r.URL.EscapedPath())
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	st := gitlabClientStore(t, srv.URL, "glpat-secret", gitlabIdentity(t, srv.URL, "team/app"))
	s := NewService()
	s.ConfigureWithStore(domain.SyncConfig{}, st)
	client, err := s.gitlabClient(gitlabIdentity(t, srv.URL, "team/app"))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.request(t.Context(), http.MethodGet, "/issues?per_page=1", nil, nil); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits %d", hits.Load())
	}
}

func TestGitLabClientRefusesAnotherHost(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	identity := "gitlab:evil.example/team/app"
	st := gitlabClientStore(t, srv.URL, "glpat-secret", identity)
	s := NewService()
	s.ConfigureWithStore(domain.SyncConfig{}, st)
	if _, err := s.gitlabClient(identity); err == nil {
		t.Fatal("mismatched host was accepted")
	}
	if hits.Load() != 0 {
		t.Fatalf("request sent: %d", hits.Load())
	}
}

func TestGitLabClientStopsOnRedirect(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer srv.Close()
	st := gitlabClientStore(t, srv.URL, "glpat-secret", gitlabIdentity(t, srv.URL, "team/app"))
	s := NewService()
	s.ConfigureWithStore(domain.SyncConfig{}, st)
	client, err := s.gitlabClient(gitlabIdentity(t, srv.URL, "team/app"))
	if err != nil {
		t.Fatal(err)
	}
	err = client.request(t.Context(), http.MethodGet, "/issues", nil, nil)
	if err == nil || strings.Contains(err.Error(), "glpat-secret") {
		t.Fatalf("redirect error %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("followed redirect: %d", hits.Load())
	}
}

func gitlabIdentity(t *testing.T, site, path string) string {
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

func gitlabClientStore(t *testing.T, site, token, project string) *repo.Store {
	t.Helper()
	st, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.PutUser(domain.User{LoginName: "gl-owner", GitLabBaseURL: site, GitLabToken: token}); err != nil {
		t.Fatal(err)
	}
	owner, _ := st.UserByLoginName("gl-owner")
	if err := st.PutProject(domain.Project{ID: "gl", Name: "GL", OwnerID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutRepository(domain.Repository{ID: "gl-repo", ProjectID: "gl", Provider: "gitlab", Name: "app", GongfengProjectID: project, DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	return st
}
