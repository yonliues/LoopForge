package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"agentflow/internal/auth"
	"agentflow/internal/domain"
	"agentflow/internal/repo"

	"github.com/gin-gonic/gin"
)

func TestGitLabCredentialSiteAndVerify(t *testing.T) {
	if !validGitLabSite("https://git.example.com") {
		t.Fatal("site root rejected")
	}
	for _, raw := range []string{"https://git.example.com/api/v4", "https://user:pw@git.example.com", "ftp://git.example.com"} {
		if validGitLabSite(raw) {
			t.Fatalf("accepted %s", raw)
		}
	}
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("PRIVATE-TOKEN") != "glpat-secret" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("PRIVATE-TOKEN") == "glpat-redirect" {
			http.Redirect(w, r, srvLocation(r), http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(`{"id":1,"username":"ada"}`))
	}))
	defer srv.Close()
	if err := verifyGitLabUser(t.Context(), srv.URL, "glpat-secret"); err != nil {
		t.Fatal(err)
	}
	err := verifyGitLabUser(t.Context(), srv.URL, "nope")
	if err == nil || strings.Contains(err.Error(), "nope") || strings.Contains(err.Error(), "glpat-secret") {
		t.Fatalf("401 error = %v", err)
	}
	before := hits
	if err := verifyGitLabUser(t.Context(), srv.URL, "glpat-redirect"); err == nil {
		t.Fatal("redirect accepted")
	}
	if hits != before+1 {
		t.Fatalf("redirect followed: hits %d -> %d", before, hits)
	}
}

func srvLocation(r *http.Request) string {
	return "http://" + r.Host + "/elsewhere"
}

func TestGitLabCredentialSaveVerifiesBeforeWrite(t *testing.T) {
	st, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.PutUser(domain.User{LoginName: "gl-ada", GitToken: "github-token"}); err != nil {
		t.Fatal(err)
	}
	ada, _ := st.UserByLoginName("gl-ada")
	var mu sync.Mutex
	okToken := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		accept := okToken && r.Header.Get("PRIVATE-TOKEN") == "glpat-secret"
		mu.Unlock()
		if !accept {
			http.Error(w, "glpat-secret", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"id":7,"username":"ada"}`))
	}))
	defer srv.Close()
	authClient := auth.NewClient(auth.Config{SessionSecret: "gitlab-cred-test"})
	s := &Server{deps: &deps{repo: st, authClient: authClient}}
	s.users = &usersHandlers{deps: s.deps}
	g := gin.New()
	g.PUT("/api/users/:id/credentials", s.users.user)
	put := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/users/%d/credentials", ada.ID), strings.NewReader(body))
		token, err := authClient.SignUser(&auth.User{ID: ada.ID, LoginName: ada.LoginName, AuthTime: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(&http.Cookie{Name: authCookieName, Value: token})
		rec := httptest.NewRecorder()
		g.ServeHTTP(rec, req)
		return rec
	}
	bad := put(`{"gitLabBaseUrl":"` + srv.URL + `/api/v4","gitLabToken":"glpat-secret"}`)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("path site status=%d body=%s", bad.Code, bad.Body.String())
	}
	stored, _ := st.User(ada.ID)
	if stored.GitLabToken != "" || stored.GitToken != "github-token" {
		t.Fatalf("path site wrote credentials: %+v", stored)
	}
	good := put(`{"gitLabBaseUrl":"` + srv.URL + `","gitLabToken":"glpat-secret"}`)
	if good.Code != http.StatusOK {
		t.Fatalf("save status=%d body=%s", good.Code, good.Body.String())
	}
	var public domain.User
	if err := json.Unmarshal(good.Body.Bytes(), &public); err != nil {
		t.Fatal(err)
	}
	if public.GitLabToken != "" || !public.GitLabTokenConfigured || public.GitLabBaseURL != srv.URL {
		t.Fatalf("public %+v", public)
	}
	stored, _ = st.User(ada.ID)
	if stored.GitLabToken != "glpat-secret" || stored.GitToken != "github-token" {
		t.Fatalf("stored %+v", stored)
	}
}
