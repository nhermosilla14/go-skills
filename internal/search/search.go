// Package search is the client for the public skills.sh search endpoint. It is the only code in the
// tool that talks to a third-party service, so it sends no credentials and reports every failure as
// an error instead of returning an empty list.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultBaseURL is the public endpoint used when SKILLS_API_URL is not set.
	DefaultBaseURL = "https://skills.sh"
	// requestTimeout bounds one search request.
	requestTimeout = 10 * time.Second
	// userAgent identifies the client to skills.sh.
	userAgent = "skills-cli (+https://skills.sh)"
	// maxBodyBytes bounds the response the client is willing to decode.
	maxBodyBytes = 4 << 20
)

// Fan-out limits, as documented in the behavior reference.
const (
	minExactResults  = 10
	maxFallbackTerms = 6
	maxMergedResults = 25
)

// Result is one skill of a search response.
type Result struct {
	ID       string // skills.sh identifier, owner/repo/skill
	Name     string // skill name
	Source   string // owner/repo, empty when the API omits it
	Installs int
}

// Response is the outcome of a search, with the exact matches first.
type Response struct {
	Results []Result
	Exact   int // number of leading results that matched every query term
}

// Error reports a failed search call: a network failure, a non-2xx response or a body that is not
// the documented document.
type Error struct {
	msg string
	err error
}

func (e *Error) Error() string { return e.msg }

// Unwrap returns the underlying failure, if any.
func (e *Error) Unwrap() error { return e.err }

// Client calls the search API. A caller that leaves HTTP nil gets a client with a ten-second
// timeout.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// New returns a client for baseURL, falling back to the public endpoint when it is blank.
func New(baseURL string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		HTTP:    &http.Client{Timeout: requestTimeout},
	}
}

// Search runs the query, optionally restricted to one owner. When the exact query returns fewer
// than ten results and it has two to six unique terms, each term is dropped in turn and the
// partial matches are merged after the exact ones.
func (c *Client) Search(ctx context.Context, query, owner string) (Response, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return Response{}, nil
	}
	exact, err := c.call(ctx, query, owner)
	if err != nil {
		return Response{}, err
	}
	terms := uniqueTerms(query)
	if len(exact) >= minExactResults || len(terms) < 2 || len(terms) > maxFallbackTerms {
		return Response{Results: exact, Exact: len(exact)}, nil
	}
	partial := c.partialMatches(ctx, terms, owner, exact)
	return Response{Results: append(exact, partial...), Exact: len(exact)}, nil
}

// partialMatches drops each term in turn, queries the remaining terms in parallel and merges the
// unseen results. Partial failures are ignored: the exact matches are already worth showing.
func (c *Client) partialMatches(ctx context.Context, terms []string, owner string, exact []Result) []Result {
	queries := make([]string, len(terms))
	for i := range terms {
		queries[i] = joinExcept(terms, i)
	}
	lists := make([][]Result, len(queries))
	var wg sync.WaitGroup
	for i, q := range queries {
		wg.Add(1)
		go func(i int, q string) {
			defer wg.Done()
			results, err := c.call(ctx, q, owner)
			if err == nil {
				lists[i] = results
			}
		}(i, q)
	}
	wg.Wait()

	seen := make(map[string]bool, len(exact))
	for _, r := range exact {
		seen[r.ID] = true
	}
	type candidate struct {
		result Result
		hits   int
		rank   int
	}
	merged := map[string]*candidate{}
	var order []string
	for _, list := range lists {
		ranked := map[string]int{}
		for rank, r := range list {
			if _, ok := ranked[r.ID]; ok {
				continue
			}
			ranked[r.ID] = rank
			if seen[r.ID] {
				continue
			}
			if entry, ok := merged[r.ID]; ok {
				entry.hits++
				if rank < entry.rank {
					entry.rank = rank
				}
				continue
			}
			merged[r.ID] = &candidate{result: r, hits: 1, rank: rank}
			order = append(order, r.ID)
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := merged[order[i]], merged[order[j]]
		if a.hits != b.hits {
			return a.hits > b.hits
		}
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.result.Installs != b.result.Installs {
			return a.result.Installs > b.result.Installs
		}
		return a.result.ID < b.result.ID
	})
	limit := maxMergedResults - len(exact)
	if limit > len(order) {
		limit = len(order)
	}
	out := make([]Result, 0, limit)
	for _, id := range order[:limit] {
		out = append(out, merged[id].result)
	}
	return out
}

// uniqueTerms returns the distinct whitespace-separated terms of the query, in order.
func uniqueTerms(query string) []string {
	fields := strings.Fields(query)
	seen := make(map[string]bool, len(fields))
	terms := make([]string, 0, len(fields))
	for _, term := range fields {
		if seen[term] {
			continue
		}
		seen[term] = true
		terms = append(terms, term)
	}
	return terms
}

// joinExcept joins every term but the one at index.
func joinExcept(terms []string, index int) string {
	rest := make([]string, 0, len(terms)-1)
	for i, term := range terms {
		if i != index {
			rest = append(rest, term)
		}
	}
	return strings.Join(rest, " ")
}

// call performs one request and decodes its results.
func (c *Client) call(ctx context.Context, query, owner string) ([]Result, error) {
	endpoint := c.endpoint(query, owner)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, &Error{msg: "building the search request", err: err}
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http().Do(req)
	if err != nil {
		return nil, &Error{msg: "searching skills.sh: " + err.Error(), err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodyBytes))
		return nil, &Error{msg: fmt.Sprintf("skills.sh returned HTTP %d", resp.StatusCode)}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, &Error{msg: "reading the skills.sh response", err: err}
	}
	results, err := parse(body)
	if err != nil {
		return nil, &Error{msg: "skills.sh returned an invalid response", err: err}
	}
	return results, nil
}

// endpoint builds the request URL with the documented parameters and no others.
func (c *Client) endpoint(query, owner string) string {
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	params := url.Values{}
	params.Set("q", query)
	if owner != "" {
		params.Set("owner", owner)
	}
	return base + "/api/search?" + params.Encode()
}

// http returns the client to use, creating the default one lazily.
func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: requestTimeout}
}

// parse decodes the documented document defensively: entries that are not objects with a non-empty
// id and name are skipped, and unknown fields are ignored.
func parse(body []byte) ([]Result, error) {
	var doc struct {
		Skills json.RawMessage `json:"skills"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&doc); err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(doc.Skills)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, errors.New("missing skills array")
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(trimmed, &entries); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(entries))
	for _, raw := range entries {
		if result, ok := parseEntry(raw); ok {
			results = append(results, result)
		}
	}
	return results, nil
}

// parseEntry reads one entry, reporting whether it is usable.
func parseEntry(raw json.RawMessage) (Result, bool) {
	var entry struct {
		ID       any `json:"id"`
		Name     any `json:"name"`
		Source   any `json:"source"`
		Installs any `json:"installs"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return Result{}, false
	}
	id, ok := entry.ID.(string)
	if !ok || strings.TrimSpace(id) == "" {
		return Result{}, false
	}
	name, ok := entry.Name.(string)
	if !ok || strings.TrimSpace(name) == "" {
		return Result{}, false
	}
	source, _ := entry.Source.(string)
	return Result{ID: id, Name: name, Source: source, Installs: installCount(entry.Installs)}, true
}

// installCount reads a JSON number leniently; a missing or mistyped count is zero.
func installCount(value any) int {
	number, ok := value.(float64)
	if !ok || number < 0 {
		return 0
	}
	return int(number)
}

// FormatInstalls is the install badge the CLI prints: N installs, 1 install, 1.2K installs,
// 3.4M installs, and nothing for a count that is zero or negative.
func FormatInstalls(count int) string {
	switch {
	case count <= 0:
		return ""
	case count >= 1_000_000:
		return trimZero(float64(count)/1_000_000) + "M installs"
	case count >= 1_000:
		return trimZero(float64(count)/1_000) + "K installs"
	case count == 1:
		return "1 install"
	default:
		return strconv.Itoa(count) + " installs"
	}
}

// trimZero renders one decimal place and drops a trailing .0.
func trimZero(value float64) string {
	return strings.TrimSuffix(strconv.FormatFloat(value, 'f', 1, 64), ".0")
}
