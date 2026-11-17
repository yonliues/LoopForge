package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	payload, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
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
