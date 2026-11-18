package tracker

import (
	"context"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"agentflow/internal/domain"
	"agentflow/internal/repo"
)

func TestGitLabLiveImportAndDelivery(t *testing.T) {
	if os.Getenv("AGENTFLOW_GITLAB_LIVE_WRITE") != "1" {
		t.Skip("opt in with AGENTFLOW_GITLAB_LIVE_WRITE=1 and a disposable repository")
	}
	path := strings.Trim(strings.TrimSpace(os.Getenv("AGENTFLOW_GITLAB_LIVE_REPO")), "/")
	site := strings.TrimRight(strings.TrimSpace(os.Getenv("AGENTFLOW_GITLAB_LIVE_SITE")), "/")
	if path == "" || site == "" {
		t.Fatal("AGENTFLOW_GITLAB_LIVE_REPO and AGENTFLOW_GITLAB_LIVE_SITE are required")
	}
	info, ok := domain.ParseGitLabRepositoryURL(site+"/"+path, site)
	if !ok {
		t.Fatal("an explicitly authorized GitLab repository is required")
	}
	raw, err := os.ReadFile(os.Getenv("AGENTFLOW_GITLAB_LIVE_TOKEN_FILE"))
	if err != nil || strings.TrimSpace(string(raw)) == "" {
		t.Fatal("a protected GitLab token file is required")
	}
	st, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.PutUser(domain.User{LoginName: "gitlab-live", GitLabBaseURL: site, GitLabToken: strings.TrimSpace(string(raw))}); err != nil {
		t.Fatal(err)
	}
	owner, _ := st.UserByLoginName("gitlab-live")
	if err := st.PutProject(domain.Project{ID: "live-gitlab", Name: "GitLab live test", OwnerID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	repository := domain.Repository{ID: "live-gitlab-repo", ProjectID: "live-gitlab", Provider: "gitlab", Name: info.Name, GongfengProjectID: info.Project, WebURL: info.WebURL, CloneURL: info.CloneURL, DefaultBranch: "main"}
	if err := st.PutRepository(repository); err != nil {
		t.Fatal(err)
	}
	s := NewService()
	s.ConfigureWithStore(domain.SyncConfig{}, st)
	if _, err := s.pullGitLabIssues(st, repository); err != nil {
		t.Fatal(err)
	}
	client, err := s.gitlabClient(info.Project)
	if err != nil {
		t.Fatal(err)
	}
	var remote struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := client.request(t.Context(), "GET", "", nil, &remote); err != nil {
		t.Fatal(err)
	}
	var tip struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := client.request(t.Context(), "GET", "/repository/branches/"+remote.DefaultBranch, nil, &tip); err != nil {
		t.Fatal(err)
	}
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	base := "loopforge-e2e-base-" + stamp
	head := "loopforge-e2e-head-" + stamp
	for _, branch := range []string{base, head} {
		if err := client.request(t.Context(), "POST", "/repository/branches", map[string]string{"branch": branch, "ref": tip.Commit.ID}, nil); err != nil {
			t.Fatal(err)
		}
		defer func(branch string) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = client.request(ctx, "DELETE", "/repository/branches/"+url.PathEscape(branch), nil, nil)
		}(branch)
	}
	ref, err := s.gitlabMergeBranch(info.Project, head, base, "LoopForge live check", "throwaway")
	if err != nil || !strings.HasPrefix(ref, "!") {
		t.Fatalf("merge %q %v", ref, err)
	}
	var after struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := client.request(t.Context(), "GET", "", nil, &after); err != nil {
		t.Fatal(err)
	}
	if after.DefaultBranch != remote.DefaultBranch {
		t.Fatalf("default branch changed from %s to %s", remote.DefaultBranch, after.DefaultBranch)
	}
	var again struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := client.request(t.Context(), "GET", "/repository/branches/"+remote.DefaultBranch, nil, &again); err != nil {
		t.Fatal(err)
	}
	if again.Commit.ID != tip.Commit.ID {
		t.Fatalf("default branch moved from %s to %s", tip.Commit.ID, again.Commit.ID)
	}
}
