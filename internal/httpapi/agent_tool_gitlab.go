package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"agentflow/internal/domain"
)

const (
	mainBotGitLabIssuesList  = "workbench_gitlab_issues_list"
	mainBotGitLabIssueGet    = "workbench_gitlab_issue_get"
	mainBotGitLabMRsList     = "workbench_gitlab_mrs_list"
	mainBotGitLabMRStatus    = "workbench_gitlab_mr_status"
	mainBotGitLabMRFiles     = "workbench_gitlab_mr_files"
	mainBotGitLabCommentAdd  = "workbench_gitlab_comment_add"
	mainBotGitLabLabelsAdd   = "workbench_gitlab_labels_add"
	mainBotGitLabMRReview    = "workbench_gitlab_mr_review"
	mainBotGitLabMRMerge     = "workbench_gitlab_mr_merge"
	mainBotGitLabIssueClose  = "workbench_gitlab_issue_close"
	mainBotGitLabIssueCreate = "workbench_gitlab_issue_create"
	mainBotGitLabMRClose     = "workbench_gitlab_mr_close"
)

func mainBotGitLabTools(s *AgentToolSession) []agentToolDescriptor {
	if s == nil || s.Mode != AgentToolAssistant || strings.TrimSpace(s.ProjectID) == "" {
		return nil
	}
	const repoProp = `"repository":{"type":"string","description":"项目里已绑定的 GitLab 仓库名或 id"}`
	const numberProp = `"number":{"type":"integer","minimum":1,"description":"Issue 或 Merge Request 的 iid"}`
	return []agentToolDescriptor{
		{Name: mainBotGitLabIssuesList, Kind: "gitlab", Description: "列出项目绑定的 GitLab 仓库里的 Issue。只读，用请求人自己的个人访问令牌。hasMore=true 表示还有未列出的条目。", InputSchema: json.RawMessage(`{"type":"object","properties":{` + repoProp + `,"state":{"type":"string","enum":["opened","closed","all"],"description":"默认 opened"},"limit":{"type":"integer","minimum":1,"maximum":50,"description":"默认 20"}},"required":["repository"],"additionalProperties":false}`)},
		{Name: mainBotGitLabIssueGet, Kind: "gitlab", Description: "读取一个 GitLab Issue 的完整正文和最多 30 条非系统评论。", InputSchema: json.RawMessage(`{"type":"object","properties":{` + repoProp + `,` + numberProp + `},"required":["repository","number"],"additionalProperties":false}`)},
		{Name: mainBotGitLabMRsList, Kind: "gitlab", Description: "列出项目绑定的 GitLab 仓库里的 Merge Request。hasMore=true 表示还有未列出的条目。", InputSchema: json.RawMessage(`{"type":"object","properties":{` + repoProp + `,"state":{"type":"string","enum":["opened","closed","all"],"description":"默认 opened"},"limit":{"type":"integer","minimum":1,"maximum":50,"description":"默认 20"}},"required":["repository"],"additionalProperties":false}`)},
		{Name: mainBotGitLabMRStatus, Kind: "gitlab", Description: "查看一个 Merge Request 的标题、状态、分支、head sha、合并状态、作者和流水线。流水线读取失败时 checksState 为 unknown，原因在 checksWarnings。", InputSchema: json.RawMessage(`{"type":"object","properties":{` + repoProp + `,` + numberProp + `},"required":["repository","number"],"additionalProperties":false}`)},
		{Name: mainBotGitLabMRFiles, Kind: "gitlab", Description: "列出一个 Merge Request 的 diff。单个 patch 和总长度都会截断。", InputSchema: json.RawMessage(`{"type":"object","properties":{` + repoProp + `,` + numberProp + `,"limit":{"type":"integer","minimum":1,"maximum":100,"description":"默认 50"}},"required":["repository","number"],"additionalProperties":false}`)},
		{Name: mainBotGitLabCommentAdd, Kind: "gitlab", Description: "给 Issue 或 Merge Request 加评论。target 必须是 issue 或 merge_request。", InputSchema: json.RawMessage(`{"type":"object","properties":{` + repoProp + `,"target":{"type":"string","enum":["issue","merge_request"]},` + numberProp + `,"body":{"type":"string","minLength":1,"maxLength":10000}},"required":["repository","target","number","body"],"additionalProperties":false}`)},
		{Name: mainBotGitLabLabelsAdd, Kind: "gitlab", Description: "给 Issue 或 Merge Request 添加已存在的标签。不存在的标签会在写入前拒绝。", InputSchema: json.RawMessage(`{"type":"object","properties":{` + repoProp + `,"target":{"type":"string","enum":["issue","merge_request"]},` + numberProp + `,"labels":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":20}},"required":["repository","target","number","labels"],"additionalProperties":false}`)},
		{Name: mainBotGitLabMRReview, Kind: "gitlab", Description: "审查 Merge Request：APPROVE 批准，COMMENT 只留言，REQUEST_CHANGES 留言并在本人已批准时撤销批准。需要「完全权限」或「帮我批准」。", InputSchema: json.RawMessage(`{"type":"object","properties":{` + repoProp + `,` + numberProp + `,"event":{"type":"string","enum":["APPROVE","REQUEST_CHANGES","COMMENT"]},"body":{"type":"string","maxLength":10000}},"required":["repository","number","event"],"additionalProperties":false}`)},
		{Name: mainBotGitLabMRMerge, Kind: "gitlab", Description: "合并 Merge Request。method 只能是 merge 或 squash。需要「完全权限」或「帮我批准」。", InputSchema: json.RawMessage(`{"type":"object","properties":{` + repoProp + `,` + numberProp + `,"method":{"type":"string","enum":["merge","squash"]},"sha":{"type":"string"},"commitMessage":{"type":"string","maxLength":10000}},"required":["repository","number"],"additionalProperties":false}`)},
		{Name: mainBotGitLabIssueClose, Kind: "gitlab", Description: "关闭 Issue。不发送关闭原因。已关闭的返回 alreadyClosed。需要「完全权限」或「帮我批准」。", InputSchema: json.RawMessage(`{"type":"object","properties":{` + repoProp + `,` + numberProp + `,"comment":{"type":"string","maxLength":10000}},"required":["repository","number"],"additionalProperties":false}`)},
		{Name: mainBotGitLabIssueCreate, Kind: "gitlab", Description: "新建 Issue。labels 必须已经存在。", InputSchema: json.RawMessage(`{"type":"object","properties":{` + repoProp + `,"title":{"type":"string","minLength":1,"maxLength":256},"body":{"type":"string","maxLength":20000},"labels":{"type":"array","items":{"type":"string"},"maxItems":20}},"required":["repository","title"],"additionalProperties":false}`)},
		{Name: mainBotGitLabMRClose, Kind: "gitlab", Description: "关闭未合并的 Merge Request。已合并的不能关闭。需要「完全权限」或「帮我批准」。", InputSchema: json.RawMessage(`{"type":"object","properties":{` + repoProp + `,` + numberProp + `,"comment":{"type":"string","maxLength":10000}},"required":["repository","number"],"additionalProperties":false}`)},
	}
}

func (h *agentToolGateway) projectHasGitLabRepository(projectID string) bool {
	projectID = strings.TrimSpace(projectID)
	if h == nil || h.deps == nil || h.repo == nil || projectID == "" {
		return false
	}
	for _, repo := range h.repo.Repositories() {
		if repo.ProjectID == projectID && repo.Provider == "gitlab" {
			return true
		}
	}
	return false
}

func (h *agentToolGateway) callMainBotGitLab(ctx context.Context, session *AgentToolSession, name string, args map[string]any) (string, error) {
	if len(mainBotGitLabTools(session)) == 0 || !h.projectHasGitLabRepository(session.ProjectID) {
		return "", errAgentToolForbidden
	}
	requester, ok := h.repo.User(session.User.ID)
	if !ok {
		return "", errAgentToolForbidden
	}
	repository, _ := args["repository"].(string)
	client, err := h.gitLabToolClient(session, repository)
	if err != nil {
		return "", errors.New(redactAgentToolResult(err.Error(), requester))
	}
	var result string
	switch name {
	case mainBotGitLabIssuesList:
		result, err = client.listIssues(ctx, args)
	case mainBotGitLabIssueGet:
		result, err = client.issueGet(ctx, args)
	case mainBotGitLabMRsList:
		result, err = client.listMRs(ctx, args)
	case mainBotGitLabMRStatus:
		result, err = client.mrStatus(ctx, args)
	case mainBotGitLabMRFiles:
		result, err = client.mrFiles(ctx, args)
	case mainBotGitLabCommentAdd, mainBotGitLabLabelsAdd, mainBotGitLabIssueCreate:
		if rerr := reviewGitHubWrite(ctx, session, name, repository, args, gitlabWriteActions[name]); rerr != nil {
			return "", rerr
		}
		switch name {
		case mainBotGitLabCommentAdd:
			result, err = client.comment(ctx, args)
		case mainBotGitLabLabelsAdd:
			result, err = client.addLabels(ctx, args)
		default:
			result, err = client.createIssue(ctx, args)
		}
	case mainBotGitLabMRReview, mainBotGitLabMRMerge, mainBotGitLabIssueClose, mainBotGitLabMRClose:
		if werr := gitlabWriteAllowed(session, name); werr != nil {
			return "", werr
		}
		switch name {
		case mainBotGitLabMRReview:
			result, err = client.review(ctx, args)
		case mainBotGitLabMRMerge:
			result, err = client.merge(ctx, args)
		case mainBotGitLabIssueClose:
			result, err = client.closeIssue(ctx, args)
		default:
			result, err = client.closeMR(ctx, args)
		}
	default:
		return "", errAgentToolUnknown
	}
	if err != nil {
		return "", errors.New(redactAgentToolResult(err.Error(), requester))
	}
	return redactAgentToolResult(result, requester), nil
}

type gitlabToolClient struct {
	base, path, token string
	http              *http.Client
}

func (h *agentToolGateway) gitLabToolClient(session *AgentToolSession, repository string) (*gitlabToolClient, error) {
	user, ok := h.repo.User(session.User.ID)
	if !ok {
		return nil, errAgentToolForbidden
	}
	site := strings.TrimSpace(user.GitLabBaseURL)
	token := strings.TrimSpace(user.GitLabToken)
	if site == "" || token == "" {
		return nil, fmt.Errorf("你还没有配置 GitLab 地址和令牌，请到「个人配置」填写站点地址和个人访问令牌")
	}
	repository = strings.TrimSpace(repository)
	if repository == "" {
		return nil, fmt.Errorf("repository is required：请填项目里已绑定的仓库名")
	}
	var names []string
	var match *domain.Repository
	for _, repo := range h.repo.Repositories() {
		if repo.ProjectID != session.ProjectID {
			continue
		}
		names = append(names, repo.Name)
		if !strings.EqualFold(repo.Name, repository) && repo.ID != repository {
			continue
		}
		if repo.Provider != "gitlab" {
			return nil, fmt.Errorf("仓库 %s 不是 GitLab 仓库", repo.Name)
		}
		copied := repo
		match = &copied
		break
	}
	if match == nil {
		if len(names) == 0 {
			return nil, fmt.Errorf("当前项目还没有绑定仓库")
		}
		return nil, fmt.Errorf("找不到仓库 %q，当前项目已绑定：%s", repository, strings.Join(names, "、"))
	}
	host, _, ok := domain.GitLabIdentityHost(match.GongfengProjectID)
	siteHost, siteOK := gitlabToolSiteHost(site)
	if !ok || !siteOK || !strings.EqualFold(host, siteHost) {
		return nil, fmt.Errorf("仓库主机与你保存的 GitLab 站点不一致")
	}
	apiPath, ok := domain.GitLabAPIProject(match.GongfengProjectID)
	if !ok {
		return nil, fmt.Errorf("仓库 %s 的 GitLab 标识无效", match.Name)
	}
	return &gitlabToolClient{
		base: strings.TrimRight(site, "/") + "/api/v4", path: apiPath, token: token,
		http: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}, nil
}

func gitlabToolSiteHost(site string) (string, bool) {
	u, err := url.Parse(site)
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

func (g *gitlabToolClient) request(ctx context.Context, method, suffix string, input, output any) error {
	return g.requestURL(ctx, method, g.base+"/projects/"+g.path+suffix, input, output)
}

func (g *gitlabToolClient) requestURL(ctx context.Context, method, rawURL string, input, output any) error {
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
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
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

func gitlabToolState(args map[string]any) (string, error) {
	state := "opened"
	if raw, ok := args["state"].(string); ok && strings.TrimSpace(raw) != "" {
		state = strings.ToLower(strings.TrimSpace(raw))
	}
	if state != "opened" && state != "closed" && state != "all" {
		return "", fmt.Errorf("state 只能是 opened、closed 或 all")
	}
	return state, nil
}

func (g *gitlabToolClient) listIssues(ctx context.Context, args map[string]any) (string, error) {
	return g.list(ctx, "issues", args)
}

func (g *gitlabToolClient) listMRs(ctx context.Context, args map[string]any) (string, error) {
	return g.list(ctx, "merge_requests", args)
}

func (g *gitlabToolClient) list(ctx context.Context, kind string, args map[string]any) (string, error) {
	state, err := gitlabToolState(args)
	if err != nil {
		return "", err
	}
	limit := githubArgLimit(args)
	var page []map[string]any
	suffix := fmt.Sprintf("/%s?state=%s&per_page=%d&page=1", kind, url.QueryEscape(state), limit+1)
	if err := g.request(ctx, http.MethodGet, suffix, nil, &page); err != nil {
		return "", err
	}
	hasMore := len(page) > limit
	if hasMore {
		page = page[:limit]
	}
	items := make([]map[string]any, 0, len(page))
	for _, item := range page {
		items = append(items, map[string]any{
			"iid": item["iid"], "title": item["title"], "state": item["state"], "webUrl": item["web_url"],
		})
	}
	raw, err := json.Marshal(map[string]any{"items": items, "hasMore": hasMore})
	return string(raw), err
}

func (g *gitlabToolClient) issueGet(ctx context.Context, args map[string]any) (string, error) {
	number, err := githubArgNumber(args)
	if err != nil {
		return "", err
	}
	var issue map[string]any
	if err := g.request(ctx, http.MethodGet, "/issues/"+strconv.Itoa(number), nil, &issue); err != nil {
		return "", err
	}
	var notes []struct {
		Body   string `json:"body"`
		System bool   `json:"system"`
		Author struct {
			Username string `json:"username"`
		} `json:"author"`
	}
	if err := g.request(ctx, http.MethodGet, "/issues/"+strconv.Itoa(number)+"/notes?per_page=100", nil, &notes); err != nil {
		return "", err
	}
	comments := []map[string]string{}
	for _, note := range notes {
		if note.System {
			continue
		}
		comments = append(comments, map[string]string{"author": note.Author.Username, "body": truncateRunes(note.Body, githubIssueCommentRunes)})
		if len(comments) == githubIssueMaxComments {
			break
		}
	}
	raw, err := json.Marshal(map[string]any{
		"iid": issue["iid"], "title": issue["title"], "state": issue["state"], "description": issue["description"], "comments": comments,
	})
	return string(raw), err
}

func (g *gitlabToolClient) mrStatus(ctx context.Context, args map[string]any) (string, error) {
	number, err := githubArgNumber(args)
	if err != nil {
		return "", err
	}
	var mr struct {
		IID          int    `json:"iid"`
		Title        string `json:"title"`
		State        string `json:"state"`
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
		SHA          string `json:"sha"`
		MergeStatus  string `json:"merge_status"`
		Author       struct {
			Username string `json:"username"`
		} `json:"author"`
	}
	if err := g.request(ctx, http.MethodGet, "/merge_requests/"+strconv.Itoa(number), nil, &mr); err != nil {
		return "", err
	}
	out := map[string]any{
		"iid": mr.IID, "title": mr.Title, "state": mr.State, "sourceBranch": mr.SourceBranch,
		"targetBranch": mr.TargetBranch, "sha": mr.SHA, "mergeStatus": mr.MergeStatus, "author": mr.Author.Username,
	}
	var pipelines []struct {
		Status string `json:"status"`
	}
	if err := g.request(ctx, http.MethodGet, "/merge_requests/"+strconv.Itoa(number)+"/pipelines?per_page=1", nil, &pipelines); err != nil {
		out["checksState"] = "unknown"
		out["checksWarnings"] = []string{err.Error()}
	} else if len(pipelines) == 0 {
		out["checksState"] = "none"
	} else {
		out["checksState"] = pipelines[0].Status
	}
	raw, err := json.Marshal(out)
	return string(raw), err
}

func (g *gitlabToolClient) mrFiles(ctx context.Context, args map[string]any) (string, error) {
	number, err := githubArgNumber(args)
	if err != nil {
		return "", err
	}
	limit := githubArgInt(args, "limit", 50)
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var diffs []struct {
		OldPath string `json:"old_path"`
		NewPath string `json:"new_path"`
		Diff    string `json:"diff"`
	}
	if err := g.request(ctx, http.MethodGet, "/merge_requests/"+strconv.Itoa(number)+"/diffs?per_page=100", nil, &diffs); err != nil {
		return "", err
	}
	budget := githubPRFilesPatchBudget
	files := []map[string]any{}
	for _, diff := range diffs {
		if len(files) == limit {
			break
		}
		patch, omitted, remaining := githubPRFilePatch(diff.Diff, budget)
		budget = remaining
		files = append(files, map[string]any{"oldPath": diff.OldPath, "newPath": diff.NewPath, "patch": patch, "omitted": omitted})
		if budget <= 0 {
			break
		}
	}
	raw, err := json.Marshal(map[string]any{"files": files, "hasMore": len(diffs) > len(files)})
	return string(raw), err
}
