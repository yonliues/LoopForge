package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"agentflow/internal/domain"
	"agentflow/internal/repo"
)

func TestGitLabAssistantWrite(t *testing.T) {
	h, owner := workbenchFixture(t)
	var mu sync.Mutex
	var calls []string
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if r.Body != nil {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &payload)
		}
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		bodies = append(bodies, payload)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/labels"):
			_, _ = w.Write([]byte(`[{"name":"bug"}]`))
		case strings.HasSuffix(r.URL.Path, "/user"):
			_, _ = w.Write([]byte(`{"username":"ada"}`))
		case strings.Contains(r.URL.Path, "/merge_requests/8/approve"):
			http.Error(w, "bad "+r.Header.Get("PRIVATE-TOKEN"), http.StatusUnauthorized)
		case strings.HasSuffix(r.URL.Path, "/merge_requests/5"):
			_, _ = w.Write([]byte(`{"iid":5,"state":"opened","approved_by":[{"user":{"username":"ada"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/issues/3") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"iid":3,"state":"opened"}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer srv.Close()
	if _, err := h.repo.UpdateUserCredentials(owner.ID, repo.UserCredentialUpdate{GitLabBaseURL: srv.URL, GitLabToken: "glpat-requester"}); err != nil {
		t.Fatal(err)
	}
	owner, _ = h.repo.User(owner.ID)
	identity := gitlabToolIdentity(t, srv.URL, "team/app")
	if err := h.repo.PutRepository(domain.Repository{ID: "gl-app", ProjectID: "workbench", Provider: "gitlab", Name: "app", GongfengProjectID: identity, CloneURL: srv.URL + "/team/app.git"}); err != nil {
		t.Fatal(err)
	}
	gw := &agentToolGateway{deps: h.deps}
	session := &AgentToolSession{Mode: AgentToolAssistant, ProjectID: "workbench", UserID: owner.ID, User: owner, ExecMode: assistantExecMode{AutoWrite: true}}
	tool := func(name string) agentToolDescriptor {
		return agentToolDescriptor{Name: name, Kind: "gitlab"}
	}
	before := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(calls)
	}
	if _, err := gw.dispatch(t.Context(), session, tool(mainBotGitLabCommentAdd), map[string]any{"repository": "app", "number": 3, "body": "hello"}); err == nil || before() != 0 {
		t.Fatal("missing target reached GitLab")
	}
	if _, err := gw.dispatch(t.Context(), session, tool(mainBotGitLabCommentAdd), map[string]any{"repository": "app", "target": "issue", "number": 3, "body": "hello"}); err != nil {
		t.Fatal(err)
	}
	if !hasCall(t, &mu, calls, "POST", "/issues/3/notes") {
		t.Fatalf("calls %v", calls)
	}
	if _, err := gw.dispatch(t.Context(), session, tool(mainBotGitLabLabelsAdd), map[string]any{"repository": "app", "target": "issue", "number": 3, "labels": []any{"missing"}}); err == nil {
		t.Fatal("missing label was accepted")
	}
	if hasCall(t, &mu, calls, "PUT", "/issues/3") {
		t.Fatalf("label put happened: %v", calls)
	}
	if _, err := gw.dispatch(t.Context(), session, tool(mainBotGitLabMRReview), map[string]any{"repository": "app", "number": 4, "event": "APPROVE"}); err != nil {
		t.Fatal(err)
	}
	if !hasCall(t, &mu, calls, "POST", "/merge_requests/4/approve") {
		t.Fatalf("calls %v", calls)
	}
	if _, err := gw.dispatch(t.Context(), session, tool(mainBotGitLabMRReview), map[string]any{"repository": "app", "number": 5, "event": "REQUEST_CHANGES", "body": "please fix"}); err != nil {
		t.Fatal(err)
	}
	if !hasCall(t, &mu, calls, "POST", "/merge_requests/5/notes") || !hasCall(t, &mu, calls, "POST", "/merge_requests/5/unapprove") {
		t.Fatalf("calls %v", calls)
	}
	n := before()
	if _, err := gw.dispatch(t.Context(), session, tool(mainBotGitLabMRMerge), map[string]any{"repository": "app", "number": 6, "method": "rebase", "sha": "abc"}); err == nil || before() != n {
		t.Fatal("rebase reached GitLab")
	}
	if _, err := gw.dispatch(t.Context(), session, tool(mainBotGitLabMRMerge), map[string]any{"repository": "app", "number": 6, "method": "merge", "sha": "abc123"}); err != nil {
		t.Fatal(err)
	}
	foundMerge := false
	mu.Lock()
	for i, call := range calls {
		if strings.Contains(call, "PUT") && strings.Contains(call, "/merge_requests/6/merge") {
			foundMerge = true
			if bodies[i]["sha"] != "abc123" || bodies[i]["should_remove_source_branch"] != false {
				t.Fatalf("merge body %v", bodies[i])
			}
		}
	}
	mu.Unlock()
	if !foundMerge {
		t.Fatalf("calls %v", calls)
	}
	if _, err := gw.dispatch(t.Context(), session, tool(mainBotGitLabIssueClose), map[string]any{"repository": "app", "number": 3}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	for i, call := range calls {
		if strings.Contains(call, "PUT") && strings.HasSuffix(call, "/issues/3") {
			if bodies[i]["state_event"] != "close" {
				t.Fatalf("close body %v", bodies[i])
			}
			if _, ok := bodies[i]["state_reason"]; ok {
				t.Fatalf("close sent a reason: %v", bodies[i])
			}
		}
	}
	mu.Unlock()
	blocked := *session
	blocked.ExecMode = assistantExecMode{}
	if _, err := gw.dispatch(t.Context(), &blocked, tool(mainBotGitLabMRMerge), map[string]any{"repository": "app", "number": 6, "method": "merge"}); err == nil || !strings.Contains(err.Error(), "请求批准") {
		t.Fatalf("approval gate: %v", err)
	}
	_, err := gw.dispatch(t.Context(), session, tool(mainBotGitLabMRReview), map[string]any{"repository": "app", "number": 8, "event": "APPROVE"})
	if err == nil || strings.Contains(err.Error(), "glpat-requester") {
		t.Fatalf("token leaked: %v", err)
	}
}

func hasCall(t *testing.T, mu *sync.Mutex, calls []string, method, suffix string) bool {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	for _, call := range calls {
		if strings.HasPrefix(call, method+" ") && strings.HasSuffix(call, suffix) {
			return true
		}
	}
	return false
}
