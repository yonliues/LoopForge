package domain

import "testing"

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
