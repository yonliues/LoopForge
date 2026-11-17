package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func validGitLabSite(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return false
	}
	return strings.Trim(u.Path, "/") == ""
}

func verifyGitLabUser(ctx context.Context, site, token string) error {
	const denied = "请检查个人配置中的 GitLab 地址和令牌"
	if !validGitLabSite(site) || strings.TrimSpace(token) == "" {
		return errors.New(denied)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(site, "/")+"/api/v4/user", nil)
	if err != nil {
		return errors.New(denied)
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Do(req)
	if err != nil {
		return errors.New(denied)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errors.New(denied)
	}
	var user struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&user) != nil || (user.ID == 0 && user.Username == "") {
		return errors.New(denied)
	}
	return nil
}
