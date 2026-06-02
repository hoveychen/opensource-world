// Package ghtoken resolves a GitHub API token from the environment or gh CLI.
package ghtoken

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Resolve returns a GitHub token, trying in order: GITHUB_TOKEN, GH_TOKEN, then
// `gh auth token`. It returns an error if none of these yield a token.
func Resolve() (string, error) {
	for _, env := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v, nil
		}
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err == nil {
		if tok := strings.TrimSpace(string(out)); tok != "" {
			return tok, nil
		}
	}
	return "", fmt.Errorf("no GitHub token found: set GITHUB_TOKEN/GH_TOKEN or run `gh auth login`")
}
