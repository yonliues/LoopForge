package tracker

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"agentflow/internal/domain"
)

func TestGitLabMergeBranch(t *testing.T) {
	t.Run("creates then reuses a merged request", func(t *testing.T) {
		var mrs []map[string]any
		var posts atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/merge_requests"):
				raw, _ := json.Marshal(mrs)
				_, _ = w.Write(raw)
			case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/merge_requests"):
				posts.Add(1)
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body["title"] != "LoopForge: feature → main" {
					t.Errorf("title %v", body["title"])
				}
				item := map[string]any{"iid": 9, "state": "opened", "source_branch": "feature", "target_branch": "main", "web_url": "https://git.example.com/team/app/-/merge_requests/9"}
				mrs = []map[string]any{item}
				_ = json.NewEncoder(w).Encode(item)
			case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/merge_requests/9/merge"):
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body["should_remove_source_branch"] != false {
					t.Errorf("remove source %v", body["should_remove_source_branch"])
				}
				if body["squash"] == true {
					t.Error("squash was enabled")
				}
				if _, ok := body["merge_when_pipeline_succeeds"]; ok {
					t.Error("pipeline flag was sent")
				}
				mrs[0]["state"] = "merged"
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			default:
				t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()
		s, project := gitlabMRService(t, srv.URL)
		ref, err := s.gitlabMergeBranch(project, "feature", "main", "", "notes")
		if err != nil || ref != "!9" || posts.Load() != 1 {
			t.Fatalf("create ref=%q err=%v posts=%d", ref, err, posts.Load())
		}
		ref, err = s.gitlabMergeBranch(project, "feature", "main", "", "")
		if err != nil || ref != "!9" || posts.Load() != 1 {
			t.Fatalf("reuse ref=%q err=%v posts=%d", ref, err, posts.Load())
		}
	})

	t.Run("same branch does no work", func(t *testing.T) {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
		}))
		defer srv.Close()
		s, project := gitlabMRService(t, srv.URL)
		ref, err := s.gitlabMergeBranch(project, "main", "main", "", "")
		if err != nil || ref != "" || hits.Load() != 0 {
			t.Fatalf("ref=%q err=%v hits=%d", ref, err, hits.Load())
		}
	})

	t.Run("missing target reports no open request", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`[]`))
		}))
		defer srv.Close()
		s, project := gitlabMRService(t, srv.URL)
		_, err := s.gitlabMergeBranch(project, "feature", "", "", "")
		if !errors.Is(err, ErrNoOpenMR) {
			t.Fatalf("err %v", err)
		}
	})

	t.Run("closed request is recreated", func(t *testing.T) {
		var posts atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet:
				_, _ = w.Write([]byte(`[{"iid":3,"state":"closed","source_branch":"feature","target_branch":"main"}]`))
			case r.Method == http.MethodPost:
				posts.Add(1)
				_, _ = w.Write([]byte(`{"iid":4,"state":"opened","source_branch":"feature","target_branch":"main"}`))
			case r.Method == http.MethodPut:
				_, _ = w.Write([]byte(`{}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()
		s, project := gitlabMRService(t, srv.URL)
		ref, err := s.gitlabMergeBranch(project, "feature", "main", "again", "")
		if err != nil || ref != "!4" || posts.Load() != 1 {
			t.Fatalf("ref=%q err=%v posts=%d", ref, err, posts.Load())
		}
	})

	t.Run("merge failure is returned", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`[{"iid":8,"state":"opened","source_branch":"feature","target_branch":"main"}]`))
				return
			}
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = io.WriteString(w, "pipeline must succeed")
		}))
		defer srv.Close()
		s, project := gitlabMRService(t, srv.URL)
		_, err := s.gitlabMergeBranch(project, "feature", "main", "", "")
		if err == nil || !strings.Contains(err.Error(), "pipeline must succeed") {
			t.Fatalf("err %v", err)
		}
	})

	t.Run("refresh stores the opened state", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"iid":6,"state":"opened","source_branch":"feature","target_branch":"main","web_url":"https://git.example.com/team/app/-/merge_requests/6"}`))
		}))
		defer srv.Close()
		s, project := gitlabMRService(t, srv.URL)
		store := &fakeMRDeliveryStore{}
		if err := s.refreshGitLabDelivery(store, project, 6); err != nil {
			t.Fatal(err)
		}
		if len(store.updates) != 1 || store.updates[0].state != "opened" || store.updates[0].targetBranch != "main" {
			t.Fatalf("updates %+v", store.updates)
		}
	})

	t.Run("promotion skips GitLab", func(t *testing.T) {
		var hits atomic.Int32
		gf := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			hits.Add(1)
		}))
		defer gf.Close()
		s := NewService()
		s.gongfeng = NewMCPClient(gf.URL, nil)
		store := &fakeMRDeliveryStore{repos: []domain.Repository{{
			ProjectID: "gl", Provider: "gitlab", GongfengProjectID: "gitlab:git.example.com/team/app",
			DefaultBranch: "main", IntegrateBranch: "release",
		}}}
		s.discoverPromoteMRs(store)
		if hits.Load() != 0 || len(store.ensured) != 0 {
			t.Fatalf("hits %d ensured %d", hits.Load(), len(store.ensured))
		}
	})
}

func gitlabMRService(t *testing.T, site string) (*Service, string) {
	t.Helper()
	identity := gitlabIdentity(t, site, "team/app")
	st := gitlabClientStore(t, site, "glpat-secret", identity)
	s := NewService()
	s.ConfigureWithStore(domain.SyncConfig{}, st)
	return s, identity
}
