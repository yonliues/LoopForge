package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"agentflow/internal/domain"
)

type gitlabClient struct {
	base, path, token string
	http              *http.Client
}

func (s *Service) gitlabClient(project string) (*gitlabClient, error) {
	host, _, ok := domain.GitLabIdentityHost(project)
	if !ok {
		return nil, fmt.Errorf("invalid GitLab repository")
	}
	escaped, ok := domain.GitLabAPIProject(project)
	if !ok {
		return nil, fmt.Errorf("invalid GitLab repository")
	}
	var site, token string
	if s != nil && s.projectAuth != nil {
		for _, repo := range s.projectAuth.Repositories() {
			if repo.Provider != "gitlab" || repo.GongfengProjectID != project {
				continue
			}
			if p, found := s.projectAuth.Project(repo.ProjectID); found {
				if u, exists := s.projectAuth.User(p.OwnerID); exists {
					site = strings.TrimSpace(u.GitLabBaseURL)
					token = strings.TrimSpace(u.GitLabToken)
				}
			}
			break
		}
	}
	if site == "" || token == "" {
		return nil, fmt.Errorf("请项目所有者到个人配置填写 GitLab 地址和令牌")
	}
	siteHost, ok := gitlabSiteHost(site)
	if !ok || !strings.EqualFold(siteHost, host) {
		return nil, fmt.Errorf("仓库主机与项目所有者保存的 GitLab 站点不一致")
	}
	return &gitlabClient{
		base:  strings.TrimRight(site, "/") + "/api/v4",
		path:  escaped,
		token: token,
		http: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}, nil
}

func gitlabSiteHost(site string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(site))
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" || (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		return host, true
	}
	return host + ":" + port, true
}

func (g *gitlabClient) request(ctx context.Context, method, suffix string, input, output any) error {
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, method, g.base+"/projects/"+g.path+suffix, body)
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", g.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	res, err := g.http.Do(req)
	if err != nil {
		return fmt.Errorf("GitLab request failed")
	}
	defer res.Body.Close()
	limit := int64(8 << 20)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		limit = 4 << 10
	}
	payload, _ := io.ReadAll(io.LimitReader(res.Body, limit))
	text := strings.ReplaceAll(string(payload), g.token, "")
	if res.StatusCode >= 300 && res.StatusCode < 400 {
		return fmt.Errorf("GitLab returned HTTP %d", res.StatusCode)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("GitLab returned HTTP %d: %s", res.StatusCode, strings.TrimSpace(text))
	}
	if output == nil {
		return nil
	}
	if err := json.Unmarshal(payload, output); err != nil {
		return fmt.Errorf("GitLab response: %s", strings.TrimSpace(text))
	}
	return nil
}

func (s *Service) pullGitLabIssues(store IssueStoreAdapter, repo domain.Repository) (int, error) {
	g, err := s.gitlabClient(repo.GongfengProjectID)
	if err != nil {
		return 0, err
	}
	count := 0
	for page := 1; page <= 100; page++ {
		var list []struct {
			ID          int64     `json:"id"`
			IID         int       `json:"iid"`
			Title       string    `json:"title"`
			Description string    `json:"description"`
			State       string    `json:"state"`
			URL         string    `json:"web_url"`
			CreatedAt   time.Time `json:"created_at"`
		}
		suffix := "/issues?state=all&per_page=100&page=" + strconv.Itoa(page)
		if err := g.request(context.Background(), http.MethodGet, suffix, nil, &list); err != nil {
			return count, err
		}
		for _, item := range list {
			id := fmt.Sprintf("gl-%s-%d", repo.ID, item.IID)
			issue := domain.Issue{
				ID: id, Number: item.IID, ProjectID: repo.ProjectID, RepositoryID: repo.ID,
				BaseBranch: repo.DefaultBranch, Kind: "issue", Title: item.Title, Description: item.Description,
				Priority: "medium", Source: "gitlab", Status: gongfengStateToStatus(item.State),
				CreatedAt: item.CreatedAt, UpdatedAt: time.Now().UTC(),
				ExternalRef: &domain.ExternalRef{
					Source: "gitlab", ExternalID: strconv.FormatInt(item.ID, 10), ExternalIID: item.IID,
					ExternalURL: item.URL, ExternalState: item.State, ExternalType: "issue", SyncedAt: time.Now().UTC(),
				},
			}
			issue.ExternalRef.SourceContentHash = githubBodyHash(item.Description)
			if item.State == "opened" {
				issue.Status = "todo"
			}
			if item.State == "closed" {
				issue.CloseSource = "external"
			}
			if old, ok := store.Issue(id); ok {
				applyExistingIssue(&issue, old)
				if old.Clarification.Status != "" || (old.ExternalRef != nil && githubBodyHash(old.Description) != old.ExternalRef.SourceContentHash) {
					issue.Description = old.Description
				}
			}
			if err := store.PutIssue(issue); err != nil {
				return count, err
			}
			count++
		}
		if len(list) < 100 {
			return count, nil
		}
	}
	return count, fmt.Errorf("GitLab import exceeded 100 pages")
}

type gitlabMR struct {
	IID          int        `json:"iid"`
	State        string     `json:"state"`
	SourceBranch string     `json:"source_branch"`
	TargetBranch string     `json:"target_branch"`
	WebURL       string     `json:"web_url"`
	MergedAt     *time.Time `json:"merged_at"`
}

func (m gitlabMR) delivery(project string) domain.DeliveryMR {
	state := "opened"
	switch m.State {
	case "merged":
		state = "merged"
	case "closed":
		state = "closed"
	}
	link := strings.TrimSpace(m.WebURL)
	if link == "" {
		link = domain.GongfengMRURL(project, m.IID)
	}
	return domain.DeliveryMR{Project: project, IID: m.IID, URL: link, State: state, GongfengState: state, TargetBranch: m.TargetBranch}
}

func (g *gitlabClient) findMR(branch string) (gitlabMR, bool, error) {
	q := url.Values{}
	q.Set("source_branch", branch)
	q.Set("state", "all")
	q.Set("order_by", "updated_at")
	q.Set("sort", "desc")
	q.Set("per_page", "20")
	var list []gitlabMR
	if err := g.request(context.Background(), http.MethodGet, "/merge_requests?"+q.Encode(), nil, &list); err != nil {
		return gitlabMR{}, false, err
	}
	for _, item := range list {
		if item.SourceBranch == branch {
			return item, true, nil
		}
	}
	return gitlabMR{}, false, nil
}

func (s *Service) gitlabFindBranch(project, branch string) (domain.DeliveryMR, bool, error) {
	g, err := s.gitlabClient(project)
	if err != nil {
		return domain.DeliveryMR{}, false, err
	}
	item, ok, err := g.findMR(branch)
	if err != nil || !ok {
		return domain.DeliveryMR{}, ok, err
	}
	return item.delivery(project), true, nil
}

func (s *Service) gitlabMergeBranch(project, branch, target, title, description string) (string, error) {
	if branch == "" {
		return "", fmt.Errorf("source branch is required")
	}
	if branch == target {
		return "", nil
	}
	g, err := s.gitlabClient(project)
	if err != nil {
		return "", err
	}
	item, found, err := g.findMR(branch)
	if err != nil {
		return "", err
	}
	if found && item.State == "merged" {
		return "!" + strconv.Itoa(item.IID), nil
	}
	if !found || item.State == "closed" {
		if target == "" {
			return "", ErrNoOpenMR
		}
		if strings.TrimSpace(title) == "" {
			title = "LoopForge: " + branch + " → " + target
		}
		item = gitlabMR{}
		body := map[string]any{"source_branch": branch, "target_branch": target, "title": title, "description": description}
		if err := g.request(context.Background(), http.MethodPost, "/merge_requests", body, &item); err != nil {
			return "", err
		}
	}
	mergeBody := map[string]any{"should_remove_source_branch": false}
	if err := g.request(context.Background(), http.MethodPut, "/merge_requests/"+strconv.Itoa(item.IID)+"/merge", mergeBody, nil); err != nil {
		return "", err
	}
	return "!" + strconv.Itoa(item.IID), nil
}

func (s *Service) refreshGitLabDelivery(store MRDeliveryStore, project string, iid int) error {
	g, err := s.gitlabClient(project)
	if err != nil {
		return err
	}
	var item gitlabMR
	if err := g.request(context.Background(), http.MethodGet, "/merge_requests/"+strconv.Itoa(iid), nil, &item); err != nil {
		return err
	}
	d := item.delivery(project)
	var mergedAt time.Time
	if d.State == "merged" && item.MergedAt != nil {
		mergedAt = item.MergedAt.UTC()
	}
	return store.UpdateMRDeliveryState(project, iid, d.State, d.TargetBranch, mergedAt, time.Now().UTC())
}
