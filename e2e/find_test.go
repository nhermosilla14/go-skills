package e2e

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// searchServer serves a fixed skills.sh document and records the queries it saw.
func searchServer(t *testing.T, body func(query, owner string) string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" {
			t.Errorf("path = %q, want /api/search", r.URL.Path)
		}
		_, _ = w.Write([]byte(body(r.URL.Query().Get("q"), r.URL.Query().Get("owner"))))
	}))
	t.Cleanup(server.Close)
	return server
}

// oneSkillDoc is a document with one result for the query react.
func oneSkillDoc(query, _ string) string {
	if query != "react" {
		return `{"skills":[]}`
	}
	return `{"skills":[{"id":"vercel/ai/react","name":"react","source":"vercel/ai","installs":1234}]}`
}

func TestE2EFindKeyword(t *testing.T) {
	server := searchServer(t, oneSkillDoc)
	project := t.TempDir()
	res := runEnv(t, project, []string{"SKILLS_API_URL=" + server.URL}, "find", "react")
	if res.exit != 0 {
		t.Fatalf("find exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	for _, want := range []string{
		"Install with npx skills add vercel/ai@react",
		"vercel/ai@react 1.2K installs",
		"└ https://skills.sh/vercel/ai/react",
	} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("stdout misses %q:\n%s", want, res.stdout)
		}
	}
	if res.stderr != "" {
		t.Errorf("stderr = %q, want empty", res.stderr)
	}
	entries, err := os.ReadDir(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("find wrote to the project: %v", entries)
	}
}

func TestE2EFindOwnerAndAliases(t *testing.T) {
	var mu sync.Mutex
	owner := ""
	server := searchServer(t, func(query, from string) string {
		mu.Lock()
		owner = from
		mu.Unlock()
		return oneSkillDoc(query, from)
	})
	readOwner := func() string {
		mu.Lock()
		defer mu.Unlock()
		return owner
	}
	for _, alias := range []string{"find", "f", "s", "search"} {
		t.Run(alias, func(t *testing.T) {
			res := runEnv(t, t.TempDir(), []string{"SKILLS_API_URL=" + server.URL},
				alias, "react", "--owner", "Vercel")
			if res.exit != 0 {
				t.Fatalf("%s exit = %d\nstderr:\n%s", alias, res.exit, res.stderr)
			}
			if got := readOwner(); got != "vercel" {
				t.Errorf("owner = %q, want vercel", got)
			}
			if !strings.Contains(res.stdout, "vercel/ai@react") {
				t.Errorf("%s stdout misses the result:\n%s", alias, res.stdout)
			}
		})
	}
}

func TestE2EFindNoResults(t *testing.T) {
	server := searchServer(t, func(string, string) string { return `{"skills":[]}` })
	res := runEnv(t, t.TempDir(), []string{"SKILLS_API_URL=" + server.URL}, "find", "nope", "--owner", "vercel")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	want := "No skills found for \"nope\" from owner \"vercel\""
	if !strings.Contains(res.stdout, want) {
		t.Errorf("stdout misses %q:\n%s", want, res.stdout)
	}
}

func TestE2EFindAPIFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	res := runEnv(t, t.TempDir(), []string{"SKILLS_API_URL=" + server.URL}, "find", "react")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstdout:\n%s", res.exit, res.stdout)
	}
	if res.stdout != "" {
		t.Errorf("stdout = %q, want empty", res.stdout)
	}
	if lines := strings.Count(res.stderr, "\n"); lines != 1 || !strings.Contains(res.stderr, "error:") {
		t.Errorf("stderr = %q, want exactly one error line", res.stderr)
	}
}

func TestE2EFindInvalidOwner(t *testing.T) {
	res := runEnv(t, t.TempDir(), nil, "find", "react", "--owner", "bad_owner")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "--owner must be a valid GitHub owner") {
		t.Errorf("stderr = %q", res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("stdout = %q, want empty", res.stdout)
	}
}

func TestE2EFindWithoutTTYPrintsTheTip(t *testing.T) {
	res := runEnv(t, t.TempDir(), nil, "find")
	if res.exit != 0 {
		t.Fatalf("exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	for _, want := range []string{
		"Tip: if running in a coding agent, follow these steps:",
		"Usage: npx skills find <query> [--owner <owner>]",
	} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("stdout misses %q:\n%s", want, res.stdout)
		}
	}
}

// TestE2EFindPartialDivider covers the documented divider between exact and partial matches.
func TestE2EFindPartialDivider(t *testing.T) {
	server := searchServer(t, func(query, _ string) string {
		switch query {
		case "a b":
			return `{"skills":[{"id":"o/r/exact","name":"exact","source":"o/r","installs":2}]}`
		case "a":
			return `{"skills":[{"id":"o/r/partial","name":"partial","source":"o/r","installs":1}]}`
		default:
			return `{"skills":[]}`
		}
	})
	res := runEnv(t, t.TempDir(), []string{"SKILLS_API_URL=" + server.URL}, "find", "a", "b")
	if res.exit != 0 {
		t.Fatalf("exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stdout, `Partial matches (some terms of "a b"):`) {
		t.Errorf("stdout misses the divider:\n%s", res.stdout)
	}
	if !strings.Contains(res.stdout, "o/r@partial") {
		t.Errorf("stdout misses the partial result:\n%s", res.stdout)
	}
}

// writeSkill writes one SKILL.md under root.
func writeSkill(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: fixture\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
