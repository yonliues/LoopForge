package tracker

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentflow/internal/domain"
	"agentflow/internal/repo"
)

func TestPullGitLabIssues(t *testing.T) {
	t.Run("keeps a clarified description", func(t *testing.T) {
		var pages int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pages++
			if r.URL.Query().Get("page") == "2" {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_, _ = w.Write([]byte(`[
				{"id":101,"iid":1,"title":"Open","description":"remote","state":"opened","web_url":"https://git.example.com/team/app/-/issues/1","created_at":"2026-01-02T03:04:05Z"},
				{"id":102,"iid":2,"title":"Done","description":"finished","state":"closed","web_url":"https://git.example.com/team/app/-/issues/2","created_at":"2026-01-02T03:04:05Z"}
			]`))
		}))
		defer srv.Close()
		identity := gitlabIdentity(t, srv.URL, "team/app")
		st := gitlabClientStore(t, srv.URL, "glpat-secret", identity)
		repoRow, _ := st.Repository("gl-repo")
		s := NewService()
		s.ConfigureWithStore(domain.SyncConfig{}, st)
		n, err := s.pullGitLabIssues(st, repoRow)
		if err != nil || n != 2 {
			t.Fatalf("pull %d %v", n, err)
		}
		open, ok := st.Issue("gl-gl-repo-1")
		if !ok || open.Status != "todo" || open.Source != "gitlab" || open.ExternalRef == nil || open.ExternalRef.ExternalID != "101" || open.ExternalRef.ExternalIID != 1 {
			t.Fatalf("open issue %+v", open)
		}
		closed, ok := st.Issue("gl-gl-repo-2")
		if !ok || closed.Status != "closed" || closed.CloseSource != "external" {
			t.Fatalf("closed issue %+v", closed)
		}
		open.Clarification.Status = "ready"
		open.Description = "local edit"
		if err := st.PutIssue(open); err != nil {
			t.Fatal(err)
		}
		if _, err := s.pullGitLabIssues(st, repoRow); err != nil {
			t.Fatal(err)
		}
		got, _ := st.Issue(open.ID)
		if got.Description != "local edit" {
			t.Fatalf("description overwritten: %q", got.Description)
		}
		if pages < 2 {
			t.Fatalf("pages %d", pages)
		}
	})

	t.Run("stops after 100 full pages", func(t *testing.T) {
		page := gitlabIssuePage(100)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(page)
		}))
		defer srv.Close()
		identity := gitlabIdentity(t, srv.URL, "team/app")
		st := gitlabClientStore(t, srv.URL, "glpat-secret", identity)
		repoRow, _ := st.Repository("gl-repo")
		s := NewService()
		s.ConfigureWithStore(domain.SyncConfig{}, st)
		mem := &countingIssueStore{IssueStoreAdapter: st}
		n, err := s.pullGitLabIssues(mem, repoRow)
		if err == nil || !strings.Contains(err.Error(), "exceeded 100 pages") {
			t.Fatalf("err %v", err)
		}
		if n != 10000 || mem.puts != 10000 {
			t.Fatalf("stored %d puts %d", n, mem.puts)
		}
	})

	t.Run("continues after one GitLab repository fails", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "/api/v4/"):
				http.Error(w, "nope", http.StatusInternalServerError)
			case r.URL.Path == "/repos/octo/app/issues":
				_, _ = w.Write([]byte(`[{"id":7,"number":4,"title":"From GitHub","body":"ok","state":"open","html_url":"https://github.com/octo/app/issues/4"}]`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()
		st, err := repo.OpenTest()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(st.Close)
		if err := st.PutUser(domain.User{LoginName: "mixed-owner", GitToken: "ghp", GitLabBaseURL: srv.URL, GitLabToken: "glpat-secret"}); err != nil {
			t.Fatal(err)
		}
		owner, _ := st.UserByLoginName("mixed-owner")
		if err := st.PutProject(domain.Project{ID: "mixed", Name: "Mixed", OwnerID: owner.ID}); err != nil {
			t.Fatal(err)
		}
		identity := gitlabIdentity(t, srv.URL, "team/app")
		if err := st.PutRepository(domain.Repository{ID: "gl-repo", ProjectID: "mixed", Provider: "gitlab", Name: "app", GongfengProjectID: identity, DefaultBranch: "main"}); err != nil {
			t.Fatal(err)
		}
		if err := st.PutRepository(domain.Repository{ID: "gh-repo", ProjectID: "mixed", Provider: "github", Name: "app", GongfengProjectID: "github:octo/app", DefaultBranch: "main"}); err != nil {
			t.Fatal(err)
		}
		s := NewService()
		s.ConfigureWithStore(domain.SyncConfig{}, st)
		s.githubAPIBase = srv.URL
		if err := s.PullAll(st); err == nil {
			t.Fatal("expected an error from the GitLab repository")
		}
		if _, ok := st.Issue("gh-gh-repo-4"); !ok {
			t.Fatal("GitHub issue was not imported")
		}
		var gitlabLog bool
		for _, row := range s.Logs() {
			if row.Source == "gitlab" && row.Status == "failed" {
				gitlabLog = true
			}
		}
		if !gitlabLog {
			t.Fatal("missing GitLab failure log")
		}
	})
}

func gitlabIssuePage(n int) []byte {
	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"id":%d,"iid":%d,"title":"t","description":"d","state":"opened","web_url":"http://x/%d"}`, i+1, i+1, i+1)
	}
	b.WriteByte(']')
	return []byte(b.String())
}

type countingIssueStore struct {
	IssueStoreAdapter
	puts int
}

func (c *countingIssueStore) Issue(string) (domain.Issue, bool) { return domain.Issue{}, false }

func (c *countingIssueStore) PutIssue(domain.Issue) error {
	c.puts++
	return nil
}
