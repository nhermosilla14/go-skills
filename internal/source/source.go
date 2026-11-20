// Package source parses skill source arguments and acquires the tree they point at, either a local
// directory or a shallow git checkout of a remote repository.
package source

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Kind is the family of a source. It is also the value written to the lock's sourceType, except
// that Azure repositories are recorded as "git", the only spelling the lock schema allows.
type Kind string

const (
	KindGitHub Kind = "github"
	KindGitLab Kind = "gitlab"
	KindAzure  Kind = "azure"
	KindGit    Kind = "git"
	KindLocal  Kind = "local"
	// KindDownload is a plain HTTP(S) URL that is not a git repository and serves a SKILL.md or an
	// archive. The lock records it as "download".
	KindDownload Kind = "download"
	// KindWellKnown is the lock vocabulary of a skill served by a host's well-known index. It is
	// never produced by Parse: the provider is discovered at fetch time for a download source.
	KindWellKnown Kind = "well-known"
)

// Source is a parsed source argument.
type Source struct {
	// Kind is the source family.
	Kind Kind
	// Source is the normalized value recorded in the lock: owner/repo for GitHub, a full URL for
	// the other git hosts, and the resolved path for a local directory.
	Source string
	// SourceURL is the location a git source is fetched from, and is empty only for GitHub
	// shorthand, whose clone URL derives from Source. A download keeps its URL in both fields. It
	// is written to the lock when it differs from Source.
	SourceURL string
	// Ref is the branch, tag or full commit SHA to check out; empty means the default branch.
	Ref string
	// Subpath is the slash-separated directory inside the source that discovery starts from.
	Subpath string
	// SkillSelector is the skill named by an "@name" suffix or a "#ref@name" fragment. It selects
	// one skill out of the source.
	SkillSelector string
	// Direct reports whether the source names exactly one skill rather than a catalog.
	Direct bool
}

// ErrUnsupported is returned for source forms this build does not fetch yet.
var ErrUnsupported = errors.New("unsupported source")

// CloneURL returns the location a git source is cloned from. Local sources return their path and a
// download returns its URL.
func (s Source) CloneURL() string {
	if s.SourceURL != "" {
		return s.SourceURL
	}
	if s.Kind == KindGitHub {
		return "https://github.com/" + s.Source + ".git"
	}
	return s.Source
}

// Display is the source as user-facing output may show it. A download URL loses its userinfo, query
// and fragment, which can carry secrets.
func (s Source) Display() string {
	if s.Kind != KindDownload {
		return s.Source
	}
	return sanitizedURL(s.Source)
}

// Recordable reports whether the lock may keep this source's location, so update, check and restore
// can fetch it again. A download URL with a query string or userinfo is not recorded, because it
// may hold credentials.
func (s Source) Recordable() bool {
	if s.Kind != KindDownload {
		return true
	}
	u, err := url.Parse(s.Source)
	if err != nil {
		return false
	}
	return u.RawQuery == "" && u.User == nil
}

var (
	commitRE  = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
	versionRE = regexp.MustCompile(`^v?[0-9]+(?:\.[0-9]+)+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
)

// fragment is a "#ref" or "#ref@skill" suffix.
type fragment struct {
	Ref   string
	Skill string
}

// Parse turns a source argument into a Source, following upstream's precedence: local directory,
// prefixes, SSH, host URLs (GitHub, GitLab, Azure, generic git), and finally the owner/repo
// shorthand. It performs no network access and does not check whether a local path exists.
func Parse(arg string) (Source, error) {
	if arg == "" || strings.TrimSpace(arg) == "" {
		return Source{}, errors.New("source is empty")
	}
	if p, ok := parseLocal(arg); ok {
		return Source{Kind: KindLocal, Source: p, SourceURL: p}, nil
	}
	if strings.HasPrefix(strings.ToLower(arg), "ext::") {
		return Source{}, fmt.Errorf("%w: unsupported git transport: ext", ErrUnsupported)
	}
	argLower := strings.ToLower(arg)
	at := strings.Index(arg, "@")
	if at > 0 && !strings.Contains(arg[:at], "://") &&
		!strings.HasPrefix(argLower, "github:") && !strings.HasPrefix(argLower, "gitlab:") &&
		strings.Contains(arg[:at], ":") {
		return Source{}, errors.New("source URLs must not contain credentials")
	}

	base, fragmentText := splitFragment(arg)
	var frag fragment
	switch {
	case fragmentText == "":
		// no fragment
	case !looksLikeGitSource(base):
		// The "#" belongs to the URL, not to a ref; URL shapes that carry it are handled below.
		base = arg
	default:
		frag = parseFragment(fragmentText)
	}

	lower := strings.ToLower(base)
	switch {
	case strings.HasPrefix(lower, "github:"):
		return parseShorthand(base[len("github:"):], frag)
	case strings.HasPrefix(lower, "gitlab:"):
		rest := strings.TrimPrefix(base[len("gitlab:"):], "//")
		if rest == "" {
			return Source{}, fmt.Errorf("invalid GitLab source %q: expected group/repo", base)
		}
		return parseGitLabURL("https://gitlab.com/"+rest, frag)
	case isSCP(base):
		return parseSCP(base, frag)
	case isURLOrHostPath(base):
		return parseURLSource(base, frag)
	}
	return parseShorthand(base, frag)
}

// splitFragment splits at the first "#". The second result is empty when there is none.
func splitFragment(arg string) (string, string) {
	base, frag, _ := strings.Cut(arg, "#")
	return base, frag
}

// parseFragment decodes a "#ref@skill" value. Failed percent-decoding keeps the raw text, as
// upstream does.
func parseFragment(text string) fragment {
	ref, skill, hasSkill := strings.Cut(text, "@")
	frag := fragment{Ref: decode(ref)}
	if hasSkill {
		frag.Skill = decode(skill)
	}
	return frag
}

func decode(s string) string {
	if v, err := url.PathUnescape(s); err == nil {
		return v
	}
	return s
}

// parseLocal recognizes the local path forms and resolves them against the working directory.
func parseLocal(arg string) (string, bool) {
	if !isLocalPath(arg) {
		return "", false
	}
	p := arg
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				p = home
			} else {
				p = filepath.Join(home, p[2:])
			}
		}
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return p, true
}

func isLocalPath(s string) bool {
	switch {
	case s == ".", s == "..", s == "~":
		return true
	case strings.HasPrefix(s, "./"), strings.HasPrefix(s, "../"),
		strings.HasPrefix(s, `.\`), strings.HasPrefix(s, `..\`),
		strings.HasPrefix(s, "~/"), strings.HasPrefix(s, `~\`):
		return true
	case filepath.IsAbs(s):
		return true
	}
	return isWindowsAbs(s)
}

// isWindowsAbs reports whether s starts with a drive letter and a separator, on every platform.
func isWindowsAbs(s string) bool {
	return len(s) >= 3 && isLetter(s[0]) && s[1] == ':' && (s[2] == '/' || s[2] == '\\')
}

func isLetter(b byte) bool {
	return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

// isSCP reports whether s has the scp-like user@host:path shape.
func isSCP(s string) bool {
	at := strings.Index(s, "@")
	if at <= 0 {
		return false
	}
	rest := s[at+1:]
	colon := strings.Index(rest, ":")
	return colon > 0 && !strings.Contains(rest[:colon], "/")
}

// isURLOrHostPath reports whether s carries a scheme or starts with a host-like first segment.
func isURLOrHostPath(s string) bool {
	if strings.Contains(s, "://") {
		return true
	}
	first, _, ok := strings.Cut(s, "/")
	return ok && strings.Contains(first, ".")
}

// checkUserinfo rejects the credentials of a git URL, which would end up in the lock or an error
// message. A download may authenticate with the userinfo of its URL, so download entries are kept
// out of the lock instead (see Source.Recordable). The "git" user of an SSH URL is not a credential.
func checkUserinfo(u *url.URL) error {
	if u.User == nil {
		return nil
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "http" || scheme == "https" {
		return errors.New("source URLs must not contain credentials")
	}
	if _, hasPassword := u.User.Password(); hasPassword {
		return errors.New("source URLs must not contain credentials")
	}
	return nil
}

// parseURLSource parses every URL shape except scp-like ones.
func parseURLSource(raw string, frag fragment) (Source, error) {
	withScheme := raw
	if !strings.Contains(raw, "://") {
		withScheme = "https://" + raw
	}
	u, err := url.Parse(withScheme)
	if err != nil {
		if strings.Contains(raw, "@") && strings.Contains(strings.SplitN(raw, "/", 2)[0], ":") {
			return Source{}, errors.New("source URLs must not contain credentials")
		}
		return Source{}, fmt.Errorf("invalid source %q", raw)
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "http", "https":
		return parseHTTPS(u, frag)
	case "ssh", "git", "file":
		if err := checkUserinfo(u); err != nil {
			return Source{}, err
		}
		if strings.ContainsAny(raw, "\n\r") {
			return Source{}, fmt.Errorf("invalid source %q", raw)
		}
		return Source{Kind: KindGit, Source: u.String(), SourceURL: u.String(),
			Ref: frag.Ref, SkillSelector: frag.Skill, Direct: frag.Skill != ""}, nil
	case "ext":
		return Source{}, fmt.Errorf("%w: unsupported git transport: ext", ErrUnsupported)
	default:
		return Source{}, fmt.Errorf("%w: unsupported URL scheme %q", ErrUnsupported, u.Scheme)
	}
}

// parseHTTPS routes an http(s) URL by host and path shape, in upstream's order. A URL that is not a
// git repository shape is a direct download; what it serves is decided by its bytes when it is
// fetched.
func parseHTTPS(u *url.URL, frag fragment) (Source, error) {
	segs := pathSegments(u)
	if !isGitHTTPS(u, segs) {
		if looksLikeRepositoryPage(u.Hostname(), segs) {
			return Source{}, fmt.Errorf("%w: %s is not a local directory or a git repository",
				ErrUnsupported, RedactCredentials(u.String()))
		}
		return parseDownload(u), nil
	}
	if err := checkUserinfo(u); err != nil {
		return Source{}, err
	}
	tree, azure := treeMarker(segs), azureMarker(segs)
	switch {
	case isGitHubHost(u.Hostname()):
		return parseGitHubURL(u, segs, frag)
	case tree >= 0:
		return parseGitLabTree(u, segs, tree, frag)
	case isGitLabHost(u.Hostname()):
		return parseGitLabURL(u.String(), frag)
	case azure >= 0:
		return parseAzure(u, segs, azure, frag)
	case len(segs) == 0:
		return Source{}, fmt.Errorf("invalid git URL %q", u.String())
	}
	return Source{Kind: KindGit, Source: u.String(), SourceURL: u.String(),
		Ref: frag.Ref, SkillSelector: frag.Skill, Direct: frag.Skill != ""}, nil
}

// isGitHTTPS reports whether an http(s) URL has a shape this build clones instead of downloading.
func isGitHTTPS(u *url.URL, segs []string) bool {
	return isGitHubHost(u.Hostname()) || treeMarker(segs) >= 0 || isGitLabHost(u.Hostname()) ||
		azureMarker(segs) >= 0 || strings.HasSuffix(strings.ToLower(u.Path), ".git")
}

// isForgeHost reports whether a host is a known git forge, whose URLs that are not a repository
// shape must not be downloaded.
func isForgeHost(host string) bool {
	h := strings.ToLower(host)
	return h == "bitbucket.org" || h == "www.bitbucket.org" || h == "dev.azure.com" ||
		strings.HasSuffix(h, ".visualstudio.com")
}

// looksLikeRepositoryPage reports whether an HTTP(S) URL that is not a git repository shape still
// names a repository page rather than a downloadable file: a known forge host or a "/tree/" path.
func looksLikeRepositoryPage(host string, segs []string) bool {
	if isForgeHost(host) {
		return true
	}
	for _, segment := range segs {
		if segment == "tree" {
			return true
		}
	}
	return false
}

// parseDownload classifies a plain HTTP(S) URL as a direct download. The URL is kept as written.
func parseDownload(u *url.URL) Source {
	raw := u.String()
	return Source{Kind: KindDownload, Source: raw, SourceURL: raw}
}

func isGitHubHost(host string) bool {
	h := strings.ToLower(host)
	return h == "github.com" || h == "www.github.com"
}

func isGitLabHost(host string) bool {
	return strings.ToLower(host) == "gitlab.com"
}

// pathSegments returns the decoded, non-empty path segments of u. Encoded slashes stay inside one
// segment instead of splitting it.
func pathSegments(u *url.URL) []string {
	raw := strings.Trim(u.EscapedPath(), "/")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, "/")
	segs := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		segs = append(segs, decode(part))
	}
	return segs
}

// treeMarker returns the index of "-" in a GitLab "/-/tree/" path, or -1.
func treeMarker(segs []string) int {
	for i := 0; i+1 < len(segs); i++ {
		if segs[i] == "-" && segs[i+1] == "tree" {
			return i
		}
	}
	return -1
}

// azureMarker returns the index of the "_git" segment in an Azure Repos path, or -1.
func azureMarker(segs []string) int {
	for i := 0; i+1 < len(segs); i++ {
		if segs[i] == "_git" && segs[i+1] != "" {
			return i
		}
	}
	return -1
}

func parseGitHubURL(u *url.URL, segs []string, frag fragment) (Source, error) {
	if len(segs) < 2 {
		return Source{}, fmt.Errorf("invalid GitHub URL %q: expected owner/repo", u.String())
	}
	owner, repo := segs[0], strings.TrimSuffix(segs[1], ".git")
	if err := validOwnerRepo(owner, repo); err != nil {
		return Source{}, fmt.Errorf("invalid GitHub URL %q: %w", u.String(), err)
	}
	src := Source{
		Kind:      KindGitHub,
		Source:    owner + "/" + repo,
		SourceURL: "https://github.com/" + owner + "/" + repo + ".git",
	}
	// A tree URL pins a ref and may add a subpath. Blob and other extra paths are ignored, as
	// upstream does, and carry no ref.
	if len(segs) >= 4 && segs[2] == "tree" {
		src.Ref = segs[3]
		src.Subpath = strings.Join(segs[4:], "/")
	}
	if src.Ref == "" {
		src.Ref = frag.Ref
	}
	src.SkillSelector = frag.Skill
	sub, err := validateSubpath(src.Subpath)
	if err != nil {
		return Source{}, err
	}
	src.Subpath = sub
	src.Direct = src.SkillSelector != ""
	return src, nil
}

func validOwnerRepo(owner, repo string) error {
	if owner == "" || repo == "" || owner == "." || owner == ".." || repo == "." || repo == ".." {
		return errors.New("owner and repository are required")
	}
	return nil
}

func parseGitLabURL(raw string, frag fragment) (Source, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Source{}, fmt.Errorf("invalid GitLab URL %q", raw)
	}
	segs := pathSegments(u)
	if len(segs) < 2 {
		return Source{}, fmt.Errorf("invalid GitLab URL %q: expected group/repo", raw)
	}
	repo := append([]string(nil), segs...)
	repo[len(repo)-1] = strings.TrimSuffix(repo[len(repo)-1], ".git")
	if repo[len(repo)-1] == "" {
		return Source{}, fmt.Errorf("invalid GitLab URL %q: repository is required", raw)
	}
	clone := schemeHost(u) + "/" + joinSegments(repo) + ".git"
	return Source{Kind: KindGitLab, Source: clone, SourceURL: clone,
		Ref: frag.Ref, SkillSelector: frag.Skill, Direct: frag.Skill != ""}, nil
}

func parseGitLabTree(u *url.URL, segs []string, marker int, frag fragment) (Source, error) {
	repo := append([]string(nil), segs[:marker]...)
	if len(repo) < 2 {
		return Source{}, fmt.Errorf("invalid GitLab tree URL %q: expected group/repo/-/tree/ref", u.String())
	}
	if len(segs) < marker+3 || segs[marker+2] == "" {
		return Source{}, fmt.Errorf("invalid GitLab tree URL %q: missing ref", u.String())
	}
	repo[len(repo)-1] = strings.TrimSuffix(repo[len(repo)-1], ".git")
	if repo[len(repo)-1] == "" {
		return Source{}, fmt.Errorf("invalid GitLab tree URL %q: repository is required", u.String())
	}
	sub, err := validateSubpath(strings.Join(segs[marker+3:], "/"))
	if err != nil {
		return Source{}, err
	}
	clone := schemeHost(u) + "/" + joinSegments(repo) + ".git"
	src := Source{Kind: KindGitLab, Source: clone, SourceURL: clone, Subpath: sub}
	// The ref embedded in the path wins over the fragment.
	src.Ref = segs[marker+2]
	if frag.Skill != "" {
		src.SkillSelector = frag.Skill
		src.Direct = true
	}
	return src, nil
}

// schemeHost rebuilds "scheme://host[:port]" from a parsed URL.
func schemeHost(u *url.URL) string {
	return u.Scheme + "://" + u.Host
}

// joinSegments escapes each path segment and joins them with "/".
func joinSegments(segs []string) string {
	escaped := make([]string, len(segs))
	for i, seg := range segs {
		escaped[i] = url.PathEscape(seg)
	}
	return strings.Join(escaped, "/")
}

func parseAzure(u *url.URL, segs []string, marker int, frag fragment) (Source, error) {
	repo := strings.TrimSuffix(segs[marker+1], ".git")
	if repo == "" {
		return Source{}, fmt.Errorf("invalid Azure Repos URL %q: repository is required", u.String())
	}
	parts := append(append([]string(nil), segs[:marker+1]...), repo)
	escaped := make([]string, len(parts))
	for i, p := range parts {
		escaped[i] = url.PathEscape(p)
	}
	clone := schemeHost(u) + "/" + strings.Join(escaped, "/")

	src := Source{Kind: KindAzure, Source: clone, SourceURL: clone}
	query := u.Query()
	switch version := query.Get("version"); {
	case strings.HasPrefix(version, "GB") && len(version) > 2:
		src.Ref = version[2:]
	case strings.HasPrefix(version, "GT") && len(version) > 2:
		src.Ref = version[2:]
	}
	// ?version=GC<sha> is ignored: git clone --branch cannot check out a commit.
	if src.Ref == "" {
		src.Ref = frag.Ref
	}
	sub, err := validateSubpath(strings.TrimLeft(query.Get("path"), "/"))
	if err != nil {
		return Source{}, err
	}
	src.Subpath = sub
	src.SkillSelector = frag.Skill
	src.Direct = src.SkillSelector != ""
	return src, nil
}

// parseSCP parses git@host:owner/repo[.git][/tree/ref[/subpath]]. Only a GitHub host has its tree
// path interpreted; any other host keeps the whole string as the clone URL.
func parseSCP(raw string, frag fragment) (Source, error) {
	at := strings.Index(raw, "@")
	rest := raw[at+1:]
	colon := strings.Index(rest, ":")
	host, p := rest[:colon], strings.TrimPrefix(rest[colon+1:], "/")
	if !isGitHubHost(host) {
		return Source{Kind: KindGit, Source: raw, SourceURL: raw,
			Ref: frag.Ref, SkillSelector: frag.Skill, Direct: frag.Skill != ""}, nil
	}
	segs := strings.Split(p, "/")
	if len(segs) < 2 {
		return Source{}, fmt.Errorf("invalid SSH URL %q: expected owner/repo", raw)
	}
	owner, repo := segs[0], strings.TrimSuffix(segs[1], ".git")
	if err := validOwnerRepo(owner, repo); err != nil {
		return Source{}, fmt.Errorf("invalid SSH URL %q: %w", raw, err)
	}
	clone := "git@" + host + ":" + owner + "/" + repo + ".git"
	src := Source{Kind: KindGit, Source: clone, SourceURL: clone}
	if len(segs) >= 4 && segs[2] == "tree" {
		src.Ref = segs[3]
		src.Subpath = strings.Join(segs[4:], "/")
	}
	if src.Ref == "" {
		src.Ref = frag.Ref
	}
	sub, err := validateSubpath(src.Subpath)
	if err != nil {
		return Source{}, err
	}
	src.Subpath = sub
	src.SkillSelector = frag.Skill
	src.Direct = src.SkillSelector != ""
	return src, nil
}

// parseShorthand parses owner/repo[/subpath][@version-or-skill][#ref[@skill]].
func parseShorthand(arg string, frag fragment) (Source, error) {
	if arg == "" {
		return Source{}, errors.New("invalid source: owner/repo is required")
	}
	head := arg
	suffix := ""
	hasSuffix := false
	if i := strings.Index(head, "@"); i >= 0 {
		suffix = head[i+1:]
		head = head[:i]
		hasSuffix = suffix != ""
	}
	segs := strings.SplitN(head, "/", 3)
	if len(segs) < 2 {
		return Source{}, fmt.Errorf("%w: %s is not a local directory or a git repository", ErrUnsupported, arg)
	}
	owner, repo := segs[0], strings.TrimSuffix(segs[1], ".git")
	if strings.Contains(owner, ":") {
		return Source{}, fmt.Errorf("%w: %s is not a local directory or a git repository", ErrUnsupported, arg)
	}
	if err := validOwnerRepo(owner, repo); err != nil {
		return Source{}, fmt.Errorf("invalid source %q: %w", arg, err)
	}
	subpath := ""
	if len(segs) == 3 {
		subpath = strings.TrimLeft(segs[2], "/")
	}
	sub, err := validateSubpath(subpath)
	if err != nil {
		return Source{}, err
	}
	src := Source{Kind: KindGitHub, Source: owner + "/" + repo, Subpath: sub}
	if hasSuffix {
		// A version-looking suffix is a ref pin only when no fragment already carries a ref or a
		// skill; otherwise it names a skill. A bare number stays ambiguous and is a skill name.
		if frag.Ref == "" && frag.Skill == "" && isVersionPin(suffix) {
			src.Ref = suffix
		} else {
			src.SkillSelector = suffix
		}
	}
	if frag.Ref != "" {
		src.Ref = frag.Ref
	}
	if frag.Skill != "" {
		src.SkillSelector = frag.Skill
	}
	src.Direct = src.SkillSelector != ""
	return src, nil
}

// isVersionPin reports whether an "@" suffix is a version or commit pin rather than a skill name.
func isVersionPin(s string) bool {
	return commitRE.MatchString(s) || versionRE.MatchString(s)
}

// validateSubpath checks the parts of a subpath that would let it escape the source.
func validateSubpath(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	if strings.Contains(s, `\`) {
		return "", fmt.Errorf("unsafe subpath %q: subpaths must use %q separators", s, "/")
	}
	if strings.HasPrefix(s, "/") || filepath.IsAbs(s) || isWindowsAbs(s) {
		return "", fmt.Errorf("unsafe subpath %q: subpaths must be relative", s)
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == ".." {
			return "", fmt.Errorf("unsafe subpath %q: subpaths must not contain %q components", s, "..")
		}
	}
	return s, nil
}

// looksLikeGitSource reports whether a "#" fragment on base is a ref rather than part of a URL.
func looksLikeGitSource(base string) bool {
	lower := strings.ToLower(base)
	switch {
	case strings.HasPrefix(lower, "github:"), strings.HasPrefix(lower, "gitlab:"),
		strings.HasPrefix(lower, "git@"):
		return true
	case strings.HasPrefix(lower, "ssh://"), strings.HasPrefix(lower, "git://"),
		strings.HasPrefix(lower, "file://"):
		return true
	}
	raw := base
	if !strings.Contains(raw, "://") {
		if first, _, ok := strings.Cut(raw, "/"); ok && strings.Contains(first, ".") {
			raw = "https://" + raw
		}
	}
	if strings.HasPrefix(strings.ToLower(raw), "http") {
		u, err := url.Parse(raw)
		if err != nil {
			return false
		}
		segs := pathSegments(u)
		if isGitHubHost(u.Hostname()) {
			// Blob URLs carry no ref, so their fragment is not a ref.
			if len(segs) >= 3 && segs[2] == "blob" {
				return false
			}
			return len(segs) >= 2
		}
		if treeMarker(segs) >= 0 || azureMarker(segs) >= 0 {
			return true
		}
		if isGitLabHost(u.Hostname()) {
			return len(segs) >= 2
		}
		return strings.HasSuffix(strings.ToLower(u.Path), ".git")
	}
	if strings.Contains(base, ":") {
		return false
	}
	parts := strings.SplitN(base, "@", 2)
	segs := strings.Split(parts[0], "/")
	return len(segs) >= 2 && segs[0] != "" && segs[1] != ""
}
