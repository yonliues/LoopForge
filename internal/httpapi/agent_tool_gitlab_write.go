package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func gitlabWriteAllowed(s *AgentToolSession, name string) error {
	if s != nil && s.ExecMode.AutoWrite {
		return nil
	}
	return fmt.Errorf("%s 会直接修改 GitLab，当前「请求批准」模式没有可确认的草稿，不能执行。请让用户把审批模式切到「完全权限」或「帮我批准」后再试", name)
}

var gitlabWriteActions = map[string]string{
	mainBotGitLabCommentAdd:  "用请求人自己的 GitLab 令牌写入评论，对外公开可见",
	mainBotGitLabLabelsAdd:   "用请求人自己的 GitLab 令牌给 Issue 或 Merge Request 添加已有标签",
	mainBotGitLabIssueCreate: "用请求人自己的 GitLab 令牌新建 Issue，建好后只能关闭不能删除",
}

func gitlabTarget(args map[string]any) (string, error) {
	target, _ := args["target"].(string)
	switch strings.TrimSpace(target) {
	case "issue":
		return "issues", nil
	case "merge_request":
		return "merge_requests", nil
	default:
		return "", fmt.Errorf("target 必须是 issue 或 merge_request")
	}
}

func gitlabStringList(args map[string]any, key string) []string {
	var out []string
	switch raw := args[key].(type) {
	case []any:
		for _, item := range raw {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, strings.TrimSpace(text))
			}
		}
	case []string:
		for _, item := range raw {
			if strings.TrimSpace(item) != "" {
				out = append(out, strings.TrimSpace(item))
			}
		}
	}
	return out
}

func (g *gitlabToolClient) comment(ctx context.Context, args map[string]any) (string, error) {
	kind, err := gitlabTarget(args)
	if err != nil {
		return "", err
	}
	number, body, err := githubCommentArgs(args)
	if err != nil {
		return "", err
	}
	if err := g.request(ctx, http.MethodPost, "/"+kind+"/"+strconv.Itoa(number)+"/notes", map[string]any{"body": body}, nil); err != nil {
		return "", err
	}
	return `{"ok":true}`, nil
}

func (g *gitlabToolClient) knownLabels(ctx context.Context) (map[string]bool, error) {
	known := map[string]bool{}
	for page := 1; page <= 20; page++ {
		var labels []struct {
			Name string `json:"name"`
		}
		if err := g.request(ctx, http.MethodGet, fmt.Sprintf("/labels?per_page=100&page=%d", page), nil, &labels); err != nil {
			return nil, err
		}
		for _, label := range labels {
			known[label.Name] = true
		}
		if len(labels) < 100 {
			break
		}
	}
	return known, nil
}

func (g *gitlabToolClient) requireLabels(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}
	known, err := g.knownLabels(ctx)
	if err != nil {
		return err
	}
	var missing []string
	for _, name := range names {
		if !known[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("标签不存在，已拒绝写入：%s", strings.Join(missing, "、"))
	}
	return nil
}

func (g *gitlabToolClient) addLabels(ctx context.Context, args map[string]any) (string, error) {
	kind, err := gitlabTarget(args)
	if err != nil {
		return "", err
	}
	number, err := githubArgNumber(args)
	if err != nil {
		return "", err
	}
	labels := gitlabStringList(args, "labels")
	if len(labels) == 0 {
		return "", fmt.Errorf("labels is required")
	}
	if err := g.requireLabels(ctx, labels); err != nil {
		return "", err
	}
	if err := g.request(ctx, http.MethodPut, "/"+kind+"/"+strconv.Itoa(number), map[string]any{"add_labels": strings.Join(labels, ",")}, nil); err != nil {
		return "", err
	}
	return `{"ok":true}`, nil
}

func (g *gitlabToolClient) createIssue(ctx context.Context, args map[string]any) (string, error) {
	title, _ := args["title"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		return "", fmt.Errorf("title is required")
	}
	labels := gitlabStringList(args, "labels")
	if err := g.requireLabels(ctx, labels); err != nil {
		return "", err
	}
	body := map[string]any{"title": title, "description": githubArgString(args, "body")}
	if len(labels) > 0 {
		body["labels"] = strings.Join(labels, ",")
	}
	var created map[string]any
	if err := g.request(ctx, http.MethodPost, "/issues", body, &created); err != nil {
		return "", err
	}
	raw, err := json.Marshal(created)
	return string(raw), err
}

func (g *gitlabToolClient) review(ctx context.Context, args map[string]any) (string, error) {
	number, err := githubArgNumber(args)
	if err != nil {
		return "", err
	}
	event := strings.ToUpper(githubArgString(args, "event"))
	body := githubArgString(args, "body")
	iid := strconv.Itoa(number)
	switch event {
	case "APPROVE":
		if err := g.request(ctx, http.MethodPost, "/merge_requests/"+iid+"/approve", map[string]any{}, nil); err != nil {
			return "", err
		}
	case "COMMENT":
		if body == "" {
			return "", fmt.Errorf("body is required")
		}
		if err := g.request(ctx, http.MethodPost, "/merge_requests/"+iid+"/notes", map[string]any{"body": body}, nil); err != nil {
			return "", err
		}
	case "REQUEST_CHANGES":
		if body == "" {
			return "", fmt.Errorf("body is required")
		}
		if err := g.request(ctx, http.MethodPost, "/merge_requests/"+iid+"/notes", map[string]any{"body": body}, nil); err != nil {
			return "", err
		}
		var me struct {
			Username string `json:"username"`
		}
		if err := g.requestURL(ctx, http.MethodGet, g.base+"/user", nil, &me); err != nil {
			return "", err
		}
		var mr struct {
			ApprovedBy []struct {
				User struct {
					Username string `json:"username"`
				} `json:"user"`
			} `json:"approved_by"`
		}
		if err := g.request(ctx, http.MethodGet, "/merge_requests/"+iid, nil, &mr); err != nil {
			return "", err
		}
		for _, approval := range mr.ApprovedBy {
			if strings.EqualFold(approval.User.Username, me.Username) {
				if err := g.request(ctx, http.MethodPost, "/merge_requests/"+iid+"/unapprove", map[string]any{}, nil); err != nil {
					return "", err
				}
				break
			}
		}
	default:
		return "", fmt.Errorf("event 只能是 APPROVE、REQUEST_CHANGES 或 COMMENT")
	}
	return `{"ok":true}`, nil
}

func (g *gitlabToolClient) merge(ctx context.Context, args map[string]any) (string, error) {
	number, err := githubArgNumber(args)
	if err != nil {
		return "", err
	}
	method := strings.ToLower(githubArgString(args, "method"))
	if method == "" {
		method = "merge"
	}
	if method == "rebase" {
		return "", fmt.Errorf("GitLab 合并不支持 rebase，请改用 merge 或 squash")
	}
	if method != "merge" && method != "squash" {
		return "", fmt.Errorf("method 只能是 merge 或 squash")
	}
	payload := map[string]any{"should_remove_source_branch": false}
	if sha := githubArgString(args, "sha"); sha != "" {
		payload["sha"] = sha
	}
	if method == "squash" {
		payload["squash"] = true
	}
	if message := githubArgString(args, "commitMessage"); message != "" {
		payload["merge_commit_message"] = message
	}
	if err := g.request(ctx, http.MethodPut, "/merge_requests/"+strconv.Itoa(number)+"/merge", payload, nil); err != nil {
		return "", err
	}
	return `{"ok":true}`, nil
}

func (g *gitlabToolClient) closeIssue(ctx context.Context, args map[string]any) (string, error) {
	number, err := githubArgNumber(args)
	if err != nil {
		return "", err
	}
	iid := strconv.Itoa(number)
	var issue struct {
		State string `json:"state"`
	}
	if err := g.request(ctx, http.MethodGet, "/issues/"+iid, nil, &issue); err != nil {
		return "", err
	}
	if issue.State == "closed" {
		return `{"alreadyClosed":true}`, nil
	}
	if comment := githubArgString(args, "comment"); comment != "" {
		if err := g.request(ctx, http.MethodPost, "/issues/"+iid+"/notes", map[string]any{"body": comment}, nil); err != nil {
			return "", err
		}
	}
	if err := g.request(ctx, http.MethodPut, "/issues/"+iid, map[string]any{"state_event": "close"}, nil); err != nil {
		return "", err
	}
	return `{"ok":true}`, nil
}

func (g *gitlabToolClient) closeMR(ctx context.Context, args map[string]any) (string, error) {
	number, err := githubArgNumber(args)
	if err != nil {
		return "", err
	}
	iid := strconv.Itoa(number)
	var mr struct {
		State string `json:"state"`
	}
	if err := g.request(ctx, http.MethodGet, "/merge_requests/"+iid, nil, &mr); err != nil {
		return "", err
	}
	if mr.State == "merged" {
		return "", fmt.Errorf("这个 Merge Request 已经合并，不能关闭")
	}
	if mr.State == "closed" {
		return `{"alreadyClosed":true}`, nil
	}
	if comment := githubArgString(args, "comment"); comment != "" {
		if err := g.request(ctx, http.MethodPost, "/merge_requests/"+iid+"/notes", map[string]any{"body": comment}, nil); err != nil {
			return "", err
		}
	}
	if err := g.request(ctx, http.MethodPut, "/merge_requests/"+iid, map[string]any{"state_event": "close"}, nil); err != nil {
		return "", err
	}
	return `{"ok":true}`, nil
}
