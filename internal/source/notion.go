package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nhermosilla14/go-skills/internal/skill"
)

// notionAPIVersion is the Notion API version every ntn api call pins.
const notionAPIVersion = "2026-03-11"

// notionPacksArgument is the argument that opens the pack browser.
const notionPacksArgument = "notion"

// notionPackScheme is the lock prefix of a pack, whose ntn download URL expires.
const notionPackScheme = "notion://packs/"

// Notion limits are the fallback of an exported page or pack. The documented SKILLS_* variables can
// still raise them.
const (
	// NotionDownloadMaxBytes bounds the body of one Notion export.
	NotionDownloadMaxBytes int64 = 50 << 20
	// NotionExtractMaxBytes bounds the bytes one Notion export may extract to.
	NotionExtractMaxBytes int64 = 100 << 20
	// NotionExtractMaxFiles bounds the entries of one Notion export.
	NotionExtractMaxFiles = 5000
)

// Notion invocation limits are variables so tests can lower them.
var (
	// ntnCommand is the executable every Notion call runs. Tests point it at a fake.
	ntnCommand = "ntn"
	// ntnTimeout is the budget of one ntn invocation.
	ntnTimeout = 30 * time.Second
	// ntnMaxOutput bounds the output of one ntn invocation, both streams together.
	ntnMaxOutput = 10 << 20
)

// notionDiagnostic carries one documented ntn message, which reads as a capitalized sentence.
type notionDiagnostic struct{ text string }

func (e notionDiagnostic) Error() string { return e.text }

// notionError builds a notionDiagnostic.
func notionError(text string) error { return notionDiagnostic{text: text} }

// errNotionCLIMissing is the documented diagnostic of a machine without ntn.
var errNotionCLIMissing = notionDiagnostic{text: "Notion CLI (ntn) is required. Install it from:\n" +
	"https://developers.notion.com/cli/get-started/overview\nThen run 'ntn login'"}

// NotionPack is one pack published in the workspace of the authenticated account.
type NotionPack struct {
	ID          string
	Name        string
	Description string
	VersionID   string
}

// notionDirectory is the temporary download ntn hands out for one page or pack.
type notionDirectory struct {
	ID        string
	VersionID string
	URL       string
}

// NotionPackDir is the staging directory of one exported pack, slash-separated and relative to the
// export root. It is derived from the pack id, so a re-export lands in the same place and the
// recorded skill paths stay valid.
func NotionPackDir(id string) string {
	return path.Join("packs", skill.SanitizeName(id))
}

// notionPageURL is the lock URL of a page: its 32-hex id without the title slug or query.
func notionPageURL(pageID string) string {
	return "https://www.notion.so/" + strings.ReplaceAll(pageID, "-", "")
}

// NotionPackURL is the lock URL of a pack. ntn hands out a temporary download URL, so the lock
// records the pack id and a re-export asks ntn for a fresh one.
func NotionPackURL(packID string) string {
	return notionPackScheme + url.PathEscape(packID)
}

// isNotionPacksArgument reports whether arg opens the pack browser instead of naming a source.
func isNotionPacksArgument(arg string) bool {
	return strings.EqualFold(arg, notionPacksArgument)
}

// RequireNTN reports the documented missing-tool error when ntn is not on PATH. It runs before any
// network call, so a machine without the CLI never reaches Notion.
func RequireNTN() error {
	if _, err := exec.LookPath(ntnCommand); err != nil {
		return errNotionCLIMissing
	}
	return nil
}

var (
	// notionHostRE matches the hosts that serve Notion pages.
	notionHostRE = regexp.MustCompile(`(^|\.)notion\.(so|com)$`)
	// notionPageIDRE matches a page id at the end of a path segment, as upstream does.
	notionPageIDRE = regexp.MustCompile(`(?:^|-)([0-9a-f]{32}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)
	// notionSecretRE matches the credential shapes ntn could echo, so no error ever carries one.
	notionSecretRE = regexp.MustCompile(`(?i)\b(?:ntn|secret)_[A-Za-z0-9]{6,}`)
	// notionTokenRE matches a labelled credential such as "Authorization: Bearer x".
	notionTokenRE = regexp.MustCompile(`(?i)\b(bearer|token|authorization|password)(\s*[:=]\s*|\s+)[^\s"']+`)
)

// parseNotionPageURL returns the dashed page id of a Notion page URL, or false so the caller can
// keep parsing the argument as an ordinary source.
func parseNotionPageURL(arg string) (string, bool) {
	u, err := url.Parse(arg)
	if err != nil {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || !notionHostRE.MatchString(host) {
		return "", false
	}
	segments := pathSegments(u)
	if len(segments) == 0 {
		return "", false
	}
	match := notionPageIDRE.FindStringSubmatch(strings.ToLower(segments[len(segments)-1]))
	if match == nil {
		return "", false
	}
	hex := strings.ReplaceAll(match[1], "-", "")
	return hex[:8] + "-" + hex[8:12] + "-" + hex[12:16] + "-" + hex[16:20] + "-" + hex[20:], true
}

// discoverNotionSource recognizes the Notion argument forms: the bare pack browser, a page URL, and
// the pack lock URL a previous install recorded. It reports false for every other argument.
func discoverNotionSource(arg string) (Source, bool) {
	if isNotionPacksArgument(arg) {
		return Source{Kind: KindNotion, Source: notionPacksArgument}, true
	}
	if pageID, ok := parseNotionPageURL(arg); ok {
		lockURL := notionPageURL(pageID)
		return Source{Kind: KindDownload, Source: lockURL, SourceURL: lockURL, NotionPageID: pageID}, true
	}
	if packID, ok := notionPackID(arg); ok {
		lockURL := NotionPackURL(packID)
		return Source{Kind: KindDownload, Source: lockURL, SourceURL: lockURL, NotionPackID: packID}, true
	}
	return Source{}, false
}

// notionPackID extracts the pack id of a pack lock URL.
func notionPackID(raw string) (string, bool) {
	if !strings.HasPrefix(raw, notionPackScheme) {
		return "", false
	}
	id, err := url.PathUnescape(raw[len(notionPackScheme):])
	if err != nil || id == "" {
		return "", false
	}
	return id, true
}

// FetchNotionPacks lists every pack of the authenticated workspace, sorted by name so the run is
// deterministic.
func FetchNotionPacks(ctx context.Context) ([]NotionPack, error) {
	var packs []NotionPack
	seen := map[string]bool{}
	cursor := ""
	for {
		exprs := []string{"page_size==100"}
		if cursor != "" {
			exprs = append(exprs, "start_cursor=="+cursor)
		}
		out, err := runNTN(ctx, notionAPIArgs("/v1/ai/plugins", exprs...))
		if err != nil {
			return nil, err
		}
		page, err := parseNotionPackPage(out)
		if err != nil {
			return nil, err
		}
		packs = append(packs, page.packs...)
		if !page.hasMore {
			break
		}
		if page.nextCursor == "" || seen[page.nextCursor] {
			return nil, notionError("Notion Agent Plugins pagination returned an invalid cursor")
		}
		seen[page.nextCursor] = true
		cursor = page.nextCursor
	}
	sort.SliceStable(packs, func(i, j int) bool {
		a, b := strings.ToLower(packs[i].Name), strings.ToLower(packs[j].Name)
		if a != b {
			return a < b
		}
		return packs[i].ID < packs[j].ID
	})
	return packs, nil
}

// NotionWorkspaceName is the workspace ntn is authenticated against. It is best effort: any failure
// yields an empty name and never blocks an install.
func NotionWorkspaceName(ctx context.Context) string {
	out, err := runNTN(ctx, notionAPIArgs("/v1/users/me"))
	if err != nil {
		return ""
	}
	var response struct {
		Bot struct {
			WorkspaceName string `json:"workspace_name"`
		} `json:"bot"`
	}
	if err := json.Unmarshal([]byte(out), &response); err != nil {
		return ""
	}
	return skill.SanitizeDisplay(strings.TrimSpace(response.Bot.WorkspaceName))
}

// ExportNotionPack downloads one pack into root, at the place NotionPackDir names. A pack that
// changed since it was listed is rejected instead of being exported under a stale version.
func ExportNotionPack(ctx context.Context, pack NotionPack, root string) error {
	if root == "" {
		return errors.New("no temporary directory was provided for the Notion export")
	}
	directory, err := notionPackDirectory(ctx, pack)
	if err != nil {
		return err
	}
	target := filepath.Join(root, filepath.FromSlash(NotionPackDir(pack.ID)))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("preparing Notion pack %q: %w", pack.Name, err)
	}
	// The pack is exported into a scratch directory first, so the archive's one top-level folder is
	// collapsed and the pack lands at the same place on every export.
	scratch, err := os.MkdirTemp(root, ".notion-export-*")
	if err != nil {
		return fmt.Errorf("preparing Notion pack %q: %w", pack.Name, err)
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	dl, err := fetchDownloadLimited(ctx, directory.URL, scratch, notionDownloadLimits())
	if err != nil {
		return fmt.Errorf("preparing Notion pack %q: %w", pack.Name, err)
	}
	if err := os.Rename(dl.Root, target); err != nil {
		return fmt.Errorf("preparing Notion pack %q: %w", pack.Name, err)
	}
	return nil
}

// acquireNotion exports a page or pack into dir and hands the result to the download pipeline. It
// requires ntn before anything is fetched.
func acquireNotion(ctx context.Context, src Source, dir string) (Tree, error) {
	if err := RequireNTN(); err != nil {
		return Tree{}, err
	}
	if src.NotionPackID != "" {
		pack, err := findNotionPack(ctx, src.NotionPackID)
		if err != nil {
			return Tree{}, err
		}
		if err := ExportNotionPack(ctx, pack, dir); err != nil {
			return Tree{}, err
		}
		return Tree{Dir: dir}, nil
	}
	if src.NotionPageID == "" {
		return Tree{}, errors.New("the Notion source names neither a page nor a pack")
	}
	directory, err := notionSkillDirectory(ctx, src.NotionPageID)
	if err != nil {
		return Tree{}, err
	}
	dl, err := fetchDownloadLimited(ctx, directory.URL, dir, notionDownloadLimits())
	if err != nil {
		return Tree{}, err
	}
	return Tree{Dir: dl.Root, SkillPath: dl.SkillPath}, nil
}

// findNotionPack looks a pack up by id in the workspace listing.
func findNotionPack(ctx context.Context, id string) (NotionPack, error) {
	packs, err := FetchNotionPacks(ctx)
	if err != nil {
		return NotionPack{}, err
	}
	for _, pack := range packs {
		if pack.ID == id {
			return pack, nil
		}
	}
	return NotionPack{}, notionError(fmt.Sprintf("No matching Notion packs found for: %s", skill.SanitizeDisplay(id)))
}

// notionSkillDirectory asks ntn for the download of one skill page.
func notionSkillDirectory(ctx context.Context, pageID string) (notionDirectory, error) {
	out, err := runNTN(ctx, notionAPIArgs("/v1/ai/skills/"+url.PathEscape(pageID)))
	if err != nil {
		return notionDirectory{}, err
	}
	return parseNotionDirectory(out, "Notion skill "+skill.SanitizeDisplay(pageID))
}

// notionPackDirectory asks ntn for the download of one pack version.
func notionPackDirectory(ctx context.Context, pack NotionPack) (notionDirectory, error) {
	out, err := runNTN(ctx, notionAPIArgs("/v1/ai/plugins/"+url.PathEscape(pack.ID)))
	if err != nil {
		return notionDirectory{}, err
	}
	directory, err := parseNotionDirectory(out, "pack")
	if err != nil {
		return notionDirectory{}, err
	}
	if directory.ID != pack.ID || directory.VersionID != pack.VersionID {
		return notionDirectory{}, notionError(fmt.Sprintf(
			"Notion pack %q changed while preparing installation", skill.SanitizeDisplay(pack.Name)))
	}
	return directory, nil
}

// notionAPIArgs builds `ntn api <path> [exprs...] --notion-version <version>`.
func notionAPIArgs(apiPath string, exprs ...string) []string {
	args := append([]string{"api", apiPath}, exprs...)
	return append(args, "--notion-version", notionAPIVersion)
}

// notionPackPage is one validated page of the packs listing.
type notionPackPage struct {
	packs      []NotionPack
	hasMore    bool
	nextCursor string
}

// parseNotionPackPage validates one packs listing response.
func parseNotionPackPage(out string) (notionPackPage, error) {
	fields, err := decodeNotionObject(out, "the Notion packs list")
	if err != nil {
		return notionPackPage{}, err
	}
	rawResults, ok := fields["results"]
	if !ok || !isJSONArray(rawResults) {
		return notionPackPage{}, notionError("Notion Agent Plugins list response is invalid")
	}
	var results []json.RawMessage
	if err := json.Unmarshal(rawResults, &results); err != nil {
		return notionPackPage{}, notionError("Notion Agent Plugins list response is invalid")
	}
	var hasMore bool
	if raw, ok := fields["has_more"]; !ok {
		return notionPackPage{}, notionError("Notion Agent Plugins list response is missing has_more")
	} else if err := json.Unmarshal(raw, &hasMore); err != nil {
		return notionPackPage{}, notionError("Notion Agent Plugins list response has an invalid has_more")
	}
	page := notionPackPage{hasMore: hasMore}
	if raw, ok := fields["next_cursor"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if err := json.Unmarshal(raw, &page.nextCursor); err != nil {
			return notionPackPage{}, notionError("Notion Agent Plugins list response has an invalid next_cursor")
		}
	}
	for _, raw := range results {
		pack, ok, err := parseNotionPack(raw)
		if err != nil {
			return notionPackPage{}, err
		}
		if ok {
			page.packs = append(page.packs, pack)
		}
	}
	return page, nil
}

// parseNotionPack validates one pack. A pack without a usable name is skipped, as upstream does.
func parseNotionPack(raw json.RawMessage) (NotionPack, bool, error) {
	fields, err := decodeNotionObject(string(raw), "response")
	if err != nil {
		return NotionPack{}, false, notionError("Notion Agent Plugins response contains an invalid pack")
	}
	name, err := notionStringField(fields, "name", "pack.name")
	if err != nil {
		return NotionPack{}, false, err
	}
	name = skill.SanitizeDisplay(strings.TrimSpace(name))
	if name == "" {
		return NotionPack{}, false, nil
	}
	id, err := notionStringField(fields, "id", "pack.id")
	if err != nil {
		return NotionPack{}, false, err
	}
	versionID, err := notionStringField(fields, "version_id", "pack.version_id")
	if err != nil {
		return NotionPack{}, false, err
	}
	description, err := notionOptionalField(fields, "description", "pack.description")
	if err != nil {
		return NotionPack{}, false, err
	}
	return NotionPack{
		ID: id, Name: name, Description: description, VersionID: versionID,
	}, true, nil
}

// parseNotionDirectory validates one directory response, whose id, version_id and url are required.
func parseNotionDirectory(out, label string) (notionDirectory, error) {
	fields, err := decodeNotionObject(out, "the "+label+" directory")
	if err != nil {
		return notionDirectory{}, notionError("Notion " + label + " directory response is invalid")
	}
	id, err := notionStringField(fields, "id", label+" directory id")
	if err != nil {
		return notionDirectory{}, err
	}
	versionID, err := notionStringField(fields, "version_id", label+" directory version_id")
	if err != nil {
		return notionDirectory{}, err
	}
	downloadURL, err := notionStringField(fields, "url", label+" directory url")
	if err != nil {
		return notionDirectory{}, err
	}
	return notionDirectory{ID: id, VersionID: versionID, URL: downloadURL}, nil
}

// decodeNotionObject decodes one JSON object, reporting invalid JSON with the response label.
func decodeNotionObject(out, label string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &fields); err != nil || fields == nil {
		return nil, errors.New("ntn returned invalid JSON for " + label)
	}
	return fields, nil
}

// notionStringField reads a required, non-empty string field.
func notionStringField(fields map[string]json.RawMessage, name, label string) (string, error) {
	raw, ok := fields[name]
	if !ok {
		return "", notionError("Notion Agent Plugins response is missing " + label)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || value == "" {
		return "", notionError("Notion Agent Plugins response has an invalid " + label)
	}
	return value, nil
}

// notionOptionalField reads an optional string field.
func notionOptionalField(fields map[string]json.RawMessage, name, label string) (string, error) {
	raw, ok := fields[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", notionError("Notion Agent Plugins response has an invalid " + label)
	}
	return skill.SanitizeDisplay(value), nil
}

// runNTN runs one ntn invocation under the timeout and returns its stdout. Stderr only reaches an
// error message, cleaned of terminal escapes and any credential shape it might echo.
func runNTN(ctx context.Context, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, ntnTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ntnCommand, args...)
	cmd.Stdin = nil
	out := newNotionOutput(ntnMaxOutput)
	cmd.Stdout = out.stream(&out.stdout)
	cmd.Stderr = out.stream(&out.stderr)
	err := cmd.Run()
	if out.truncated() {
		return "", errors.New("ntn api output exceeded 10 MiB")
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("ntn api timed out after %d seconds", int(ntnTimeout.Seconds()))
	}
	if err != nil {
		return "", notionCommandError(err, out.stderr.String())
	}
	return out.stdout.String(), nil
}

// notionCommandError maps the failure of one invocation, never repeating a raw stderr.
func notionCommandError(err error, stderr string) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if text := cleanNotionText(stderr); text != "" {
			return fmt.Errorf("ntn api failed: %s", text)
		}
		return fmt.Errorf("ntn api failed with exit code %d", exitErr.ExitCode())
	}
	if errors.Is(err, exec.ErrNotFound) {
		return errNotionCLIMissing
	}
	return notionError("Unable to start ntn: " + cleanNotionText(err.Error()))
}

// cleanNotionText strips terminal escapes, URL credentials and credential-shaped tokens from text,
// and caps it so a long stderr does not flood the terminal.
func cleanNotionText(text string) string {
	text = RedactCredentials(skill.SanitizeDisplay(strings.TrimSpace(text)))
	text = notionSecretRE.ReplaceAllString(text, "***")
	text = notionTokenRE.ReplaceAllString(text, "$1$2***")
	if runes := []rune(text); len(runes) > 800 {
		return string(runes[:800]) + "…"
	}
	return text
}

// notionOutput captures both streams of one invocation under a shared byte budget.
type notionOutput struct {
	mu         sync.Mutex
	remaining  int
	overflowed bool
	stdout     bytes.Buffer
	stderr     bytes.Buffer
}

func newNotionOutput(limit int) *notionOutput {
	return &notionOutput{remaining: limit}
}

// stream returns a writer that feeds one buffer through the shared budget.
func (o *notionOutput) stream(dst *bytes.Buffer) *notionStream {
	return &notionStream{out: o, dst: dst}
}

// truncated reports whether the budget was spent, which rejects the invocation.
func (o *notionOutput) truncated() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.overflowed
}

// notionStream writes at most the remaining budget, remembering the overflow.
type notionStream struct {
	out *notionOutput
	dst *bytes.Buffer
}

func (s *notionStream) Write(p []byte) (int, error) {
	s.out.mu.Lock()
	defer s.out.mu.Unlock()
	if s.out.overflowed {
		return len(p), nil
	}
	if len(p) > s.out.remaining {
		s.dst.Write(p[:s.out.remaining])
		s.out.remaining = 0
		s.out.overflowed = true
		return len(p), nil
	}
	s.dst.Write(p)
	s.out.remaining -= len(p)
	return len(p), nil
}

// notionDownloadLimits is the Notion fallback of the download limits. An exported SKILLS_* value
// still raises them.
func notionDownloadLimits() limits {
	return downloadLimitsWith(limits{
		downloadBytes: NotionDownloadMaxBytes,
		extractBytes:  NotionExtractMaxBytes,
		extractFiles:  NotionExtractMaxFiles,
	})
}
