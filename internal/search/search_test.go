package search

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestServer serves one JSON document per query, keyed by the q parameter.
func newTestServer(t *testing.T, documents map[string]string) (*httptest.Server, *int) {
	t.Helper()
	var mu sync.Mutex
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/search" {
			t.Errorf("path = %q, want /api/search", r.URL.Path)
		}
		if ua := r.Header.Get("User-Agent"); ua == "" {
			t.Error("request has no User-Agent")
		}
		body, ok := documents[r.URL.Query().Get("q")]
		if !ok {
			t.Errorf("unexpected query %q", r.URL.Query().Get("q"))
			body = `{"skills":[]}`
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

// skillJSON builds one entry of the documented document.
func skillJSON(id, name, source string, installs int) string {
	return fmt.Sprintf(`{"id":%q,"name":%q,"source":%q,"installs":%d}`, id, name, source, installs)
}

// doc wraps entries in the documented document.
func doc(entries ...string) string {
	return `{"skills":[` + strings.Join(entries, ",") + `]}`
}

func TestSearchExact(t *testing.T) {
	server, requests := newTestServer(t, map[string]string{
		"react": doc(skillJSON("facebook/react/react", "react", "facebook/react", 42)),
	})
	client := New(server.URL)
	res, err := client.Search(t.Context(), "react", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if *requests != 1 {
		t.Errorf("requests = %d, want 1", *requests)
	}
	if res.Exact != 1 || len(res.Results) != 1 {
		t.Fatalf("response = %+v, want one exact result", res)
	}
	want := Result{ID: "facebook/react/react", Name: "react", Source: "facebook/react", Installs: 42}
	if res.Results[0] != want {
		t.Errorf("result = %+v, want %+v", res.Results[0], want)
	}
}

func TestSearchEncodesTheQuery(t *testing.T) {
	server, _ := newTestServer(t, map[string]string{
		"react&native": doc(skillJSON("o/r/react", "react", "o/r", 1)),
	})
	res, err := New(server.URL).Search(t.Context(), "react&native", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("results = %+v, want one", res.Results)
	}
}

func TestSearchOwnerFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("owner"); got != "vercel" {
			t.Errorf("owner = %q, want vercel", got)
		}
		if got := r.URL.Query().Get("q"); got != "react" {
			t.Errorf("q = %q, want react", got)
		}
		_, _ = w.Write([]byte(doc(skillJSON("vercel/ai/react", "react", "vercel/ai", 10))))
	}))
	defer server.Close()
	res, err := New(server.URL).Search(t.Context(), "react", "vercel")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("results = %+v, want one", res.Results)
	}
}

func TestSearchSkipsMalformedEntries(t *testing.T) {
	body := `{"skills":[
		{"id":"o/r/good","name":"good","source":"o/r","installs":5},
		{"name":"no-id"},
		{"id":"o/r/no-name"},
		{"id":42,"name":"number-id"},
		{"id":"o/r/typed","name":"typed","installs":"many","unknown":true},
		"not an object",
		{"id":"o/r/source","name":"source","source":42}
	]}`
	server, _ := newTestServer(t, map[string]string{"q": body})
	res, err := New(server.URL).Search(t.Context(), "q", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var ids []string
	for _, r := range res.Results {
		ids = append(ids, r.ID)
	}
	want := []string{"o/r/good", "o/r/typed", "o/r/source"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("ids = %v, want %v", ids, want)
	}
	if res.Results[1].Installs != 0 {
		t.Errorf("mistyped installs = %d, want 0", res.Results[1].Installs)
	}
}

func TestSearchErrors(t *testing.T) {
	t.Run("non-2xx", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()
		_, err := New(server.URL).Search(t.Context(), "react", "")
		var apiErr *Error
		if !errors.As(err, &apiErr) {
			t.Fatalf("err = %v, want a *Error", err)
		}
	})
	t.Run("invalid body", func(t *testing.T) {
		for _, body := range []string{`not json`, `{"skills":42}`, `{"nope":[]}`, `{}`} {
			server, _ := newTestServer(t, map[string]string{"react": body})
			_, err := New(server.URL).Search(t.Context(), "react", "")
			server.Close()
			var apiErr *Error
			if !errors.As(err, &apiErr) {
				t.Errorf("body %q: err = %v, want a *Error", body, err)
			}
		}
	})
	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(200 * time.Millisecond)
			_, _ = w.Write([]byte(doc()))
		}))
		defer server.Close()
		client := &Client{BaseURL: server.URL, HTTP: &http.Client{Timeout: 20 * time.Millisecond}}
		_, err := client.Search(t.Context(), "react", "")
		var apiErr *Error
		if !errors.As(err, &apiErr) {
			t.Fatalf("err = %v, want a *Error", err)
		}
	})
	t.Run("unreachable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := server.URL
		server.Close()
		_, err := New(url).Search(t.Context(), "react", "")
		var apiErr *Error
		if !errors.As(err, &apiErr) {
			t.Fatalf("err = %v, want a *Error", err)
		}
	})
}

func TestSearchPartialFanOut(t *testing.T) {
	server, requests := newTestServer(t, map[string]string{
		"a b c": doc(
			skillJSON("o/r/exact1", "exact1", "o/r", 5),
			skillJSON("o/r/exact2", "exact2", "o/r", 4),
		),
		"b c": doc(
			skillJSON("o/r/p", "p", "o/r", 1),
			skillJSON("o/r/q", "q", "o/r", 2),
			skillJSON("o/r/rr", "rr", "o/r", 3),
		),
		"a c": doc(
			skillJSON("o/r/q", "q", "o/r", 2),
			skillJSON("o/r/rr", "rr", "o/r", 3),
			skillJSON("o/r/s", "s", "o/r", 4),
		),
		"a b": doc(
			skillJSON("o/r/rr", "rr", "o/r", 3),
			skillJSON("o/r/exact1", "exact1", "o/r", 5),
		),
	})
	res, err := New(server.URL).Search(t.Context(), "a b c", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if *requests != 4 {
		t.Errorf("requests = %d, want the exact query plus three subqueries", *requests)
	}
	var ids []string
	for _, r := range res.Results {
		ids = append(ids, r.ID)
	}
	want := []string{"o/r/exact1", "o/r/exact2", "o/r/rr", "o/r/q", "o/r/p", "o/r/s"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	if res.Exact != 2 {
		t.Errorf("exact = %d, want 2", res.Exact)
	}
}

func TestSearchNoFanOut(t *testing.T) {
	entries := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		entries = append(entries, skillJSON(fmt.Sprintf("o/r/s%d", i), "s", "o/r", i))
	}
	tests := map[string]string{
		"ten exact results": doc(entries...),
		"one term":          doc(skillJSON("o/r/a", "a", "o/r", 1)),
		"seven terms":       doc(skillJSON("o/r/a", "a", "o/r", 1)),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			query := "a b c"
			switch name {
			case "one term":
				query = "a"
			case "seven terms":
				query = "a b c d e f g"
			}
			server, requests := newTestServer(t, map[string]string{query: body})
			if _, err := New(server.URL).Search(t.Context(), query, ""); err != nil {
				t.Fatalf("Search: %v", err)
			}
			if *requests != 1 {
				t.Errorf("requests = %d, want 1", *requests)
			}
		})
	}
}

func TestSearchCapsMergedResults(t *testing.T) {
	exact := doc(skillJSON("o/r/exact", "exact", "o/r", 1))
	var b, a []string
	for i := 0; i < 30; i++ {
		b = append(b, skillJSON(fmt.Sprintf("o/r/b%d", i), "b", "o/r", i))
		a = append(a, skillJSON(fmt.Sprintf("o/r/a%d", i), "a", "o/r", i))
	}
	server, _ := newTestServer(t, map[string]string{
		"a b": exact,
		"b":   doc(b...),
		"a":   doc(a...),
	})
	res, err := New(server.URL).Search(t.Context(), "a b", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Results) != 25 {
		t.Fatalf("results = %d, want the 25 cap", len(res.Results))
	}
	if res.Exact != 1 {
		t.Errorf("exact = %d, want 1", res.Exact)
	}
}

func TestFormatInstalls(t *testing.T) {
	tests := map[int]string{
		0:        "",
		-5:       "",
		1:        "1 install",
		2:        "2 installs",
		999:      "999 installs",
		1000:     "1K installs",
		1234:     "1.2K installs",
		1000000:  "1M installs",
		3400000:  "3.4M installs",
		12500000: "12.5M installs",
	}
	for count, want := range tests {
		if got := FormatInstalls(count); got != want {
			t.Errorf("FormatInstalls(%d) = %q, want %q", count, got, want)
		}
	}
}
