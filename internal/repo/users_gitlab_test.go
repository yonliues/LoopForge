package repo

import (
	"testing"

	"agentflow/internal/domain"
)

func TestGitLabCredentialsStayOffTheGitHubToken(t *testing.T) {
	st, err := OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
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
	if err := st.PutUser(domain.User{ID: saved.ID, LoginName: "gl"}); err != nil {
		t.Fatal(err)
	}
	kept, _ := st.User(saved.ID)
	if kept.GitLabToken != "glpat-secret" || kept.GitToken != "github-token" {
		t.Fatalf("login refresh wiped credentials: %+v", kept)
	}
	cleared, err := st.UpdateUserCredentials(saved.ID, UserCredentialUpdate{GitLabClear: true})
	if err != nil || cleared.GitLabToken != "" || cleared.GitLabBaseURL != "" || cleared.GitToken != "github-token" {
		t.Fatalf("clear %+v %v", cleared, err)
	}
}
