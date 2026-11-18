package domain

import (
	"net/url"
	"regexp"
	"strings"
)

var gitlabPathSegment = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// ParseGitLabRepositoryURL parses a GitLab web, HTTPS, or SSH reference when
// its host matches the saved site. The identity includes the host so two
// instances with the same project path do not collide.
func ParseGitLabRepositoryURL(raw, site string) (GongfengRepoInfo, bool) {
	siteURL, err := url.Parse(strings.TrimSpace(site))
	if err != nil || !gitlabSiteUsable(siteURL) {
		return GongfengRepoInfo{}, false
	}
	host, port, path, ok := splitGitLabRemote(strings.TrimSpace(raw))
	if !ok {
		return GongfengRepoInfo{}, false
	}
	siteKey := gitlabHostKey(siteURL.Scheme, siteURL.Hostname(), siteURL.Port())
	if gitlabHostKey(remoteScheme(port), host, port) != siteKey {
		return GongfengRepoInfo{}, false
	}
	projectPath := normalizeGitLabProjectPath(path)
	if projectPath == "" {
		return GongfengRepoInfo{}, false
	}
	name := projectPath[strings.LastIndex(projectPath, "/")+1:]
	web := siteURL.Scheme + "://" + siteKey + "/" + projectPath
	return GongfengRepoInfo{Project: "gitlab:" + siteKey + "/" + projectPath, Name: name, WebURL: web, CloneURL: web + ".git"}, true
}

func gitlabSiteUsable(u *url.URL) bool {
	if u == nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return false
	}
	path := strings.Trim(u.Path, "/")
	return path == ""
}

// remoteScheme picks the scheme only to decide which default port to omit.
// SSH port 22 is omitted so git@host matches an https site on the same host.
func remoteScheme(port string) string {
	if port == "22" {
		return "ssh"
	}
	if port == "80" {
		return "http"
	}
	return "https"
}

func gitlabHostKey(scheme, host, port string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	port = strings.TrimSpace(port)
	switch {
	case port == "", scheme == "https" && port == "443", scheme == "http" && port == "80", scheme == "ssh" && port == "22":
		return host
	default:
		return host + ":" + port
	}
}

func splitGitLabRemote(raw string) (host, port, path string, ok bool) {
	raw = strings.Trim(raw, `"'`)
	if len(raw) >= 10 && strings.EqualFold(raw[:10], "git clone ") {
		raw = strings.Trim(strings.TrimSpace(raw[10:]), `"'`)
	}
	if raw == "" || strings.ContainsAny(raw, " \t") {
		return "", "", "", false
	}
	if strings.HasPrefix(strings.ToLower(raw), "git@") {
		rest := raw[4:]
		colon := strings.Index(rest, ":")
		if colon <= 0 || colon == len(rest)-1 {
			return "", "", "", false
		}
		return rest[:colon], "22", rest[colon+1:], true
	}
	u, err := url.Parse(raw)
	if err != nil || u.RawQuery != "" || u.Fragment != "" || u.Hostname() == "" {
		return "", "", "", false
	}
	if u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "ssh" {
		return "", "", "", false
	}
	if u.User != nil {
		username := u.User.Username()
		_, hasPassword := u.User.Password()
		if hasPassword || u.Scheme != "ssh" || username != "git" {
			return "", "", "", false
		}
	}
	path = strings.Trim(u.Path, "/")
	if path == "" {
		return "", "", "", false
	}
	port = u.Port()
	if port == "" {
		switch u.Scheme {
		case "http":
			port = "80"
		case "ssh":
			port = "22"
		default:
			port = "443"
		}
	}
	return u.Hostname(), port, path, true
}

func normalizeGitLabProjectPath(path string) string {
	p := strings.Trim(path, "/")
	if p == "" || strings.Contains(p, "..") || strings.HasPrefix(p, "/") {
		return ""
	}
	if strings.HasSuffix(strings.ToLower(p), ".git") {
		p = p[:len(p)-4]
	}
	if i := strings.Index(p, "/-/"); i >= 0 {
		p = p[:i]
	}
	p = strings.Trim(p, "/")
	if !strings.Contains(p, "/") {
		return ""
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." || !gitlabPathSegment.MatchString(seg) {
			return ""
		}
	}
	return p
}

// GitLabIdentityHost splits a gitlab: identity into its host key and project path.
func GitLabIdentityHost(project string) (host, path string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(project), "gitlab:")
	if !found {
		return "", "", false
	}
	slash := strings.Index(rest, "/")
	if slash <= 0 || slash == len(rest)-1 {
		return "", "", false
	}
	host, path = rest[:slash], rest[slash+1:]
	if normalizeGitLabProjectPath(path) != path {
		return "", "", false
	}
	return host, path, true
}

// GitLabAPIProject returns the URL-escaped project path GitLab expects in /projects/:id.
func GitLabAPIProject(project string) (string, bool) {
	_, path, ok := GitLabIdentityHost(project)
	if !ok {
		return "", false
	}
	return url.PathEscape(path), true
}
