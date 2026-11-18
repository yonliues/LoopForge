package web

import (
	"os/exec"
	"testing"
)

func TestGitLabSource(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	if out, err := exec.Command(node, "gitlab_source_test.js").CombinedOutput(); err != nil {
		t.Fatalf("gitlab source: %v\n%s", err, out)
	}
}
