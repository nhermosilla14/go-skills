package source

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nhermosilla14/go-skills/internal/skill"
)

// Default limits of a direct download. The environment can raise them, never lower them.
const (
	// DownloadMaxBytes bounds the body of one download.
	DownloadMaxBytes int64 = 10 << 20
	// ExtractMaxBytes bounds the bytes an archive may extract to.
	ExtractMaxBytes int64 = 25 << 20
	// ExtractMaxFiles bounds the entries an archive may contain.
	ExtractMaxFiles = 1000
)

// Environment variables that may raise the download limits.
const (
	envDownloadMaxBytes = "SKILLS_DOWNLOAD_MAX_BYTES"
	envExtractMaxBytes  = "SKILLS_EXTRACT_MAX_BYTES"
	envExtractMaxFiles  = "SKILLS_EXTRACT_MAX_FILES"
)

const (
	// userAgent identifies this client to the server that serves a download.
	userAgent = "skills-cli (+https://github.com/nhermosilla14/go-skills)"
	// downloadTimeout is the budget of one download request, redirects included.
	downloadTimeout = 10 * time.Second
	// maxRedirects is how many redirects one download may follow.
	maxRedirects = 5
	// bodyFile is the file a downloaded body is staged in before it is sniffed.
	bodyFile = "source.download"
	// extractDir is the directory an archive is extracted to before it is collapsed.
	extractDir = "extract"
	// skillFile is the name of the skill document.
	skillFile = "SKILL.md"
)

// downloadClient is shared by every download. TLS verification stays on, no credential is added,
// and redirects are bounded.
var downloadClient = &http.Client{
	Timeout: downloadTimeout,
	CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return errTooManyRedirects
		}
		return nil
	},
}

// errTooManyRedirects is what a redirect chain over the limit reports.
var errTooManyRedirects = fmt.Errorf("stopped after %d redirects", maxRedirects)

// errNotAnArchive marks a body that looked like an archive but could not be read as one. It is
// reported as "not a valid SKILL.md or supported archive", not as an archive rejection.
var errNotAnArchive = errors.New("not a supported archive")

// downloadError carries one documented download message. Those texts are capitalized sentences, so
// they are carried by a type instead of an error literal.
type downloadError struct{ text string }

func (e downloadError) Error() string { return e.text }

// Download is the materialized content of one direct download.
type Download struct {
	// Root is the directory discovery starts from: the folder holding a single downloaded
	// SKILL.md, or the extracted archive with a single top-level directory collapsed.
	Root string
	// SkillPath is the SKILL.md path relative to Root when the body was one skill file. It is
	// empty for an archive, whose skills are discovered like any other tree.
	SkillPath string
}

// limits are the bounds enforced while downloading and extracting.
type limits struct {
	downloadBytes int64
	extractBytes  int64
	extractFiles  int
}

// FetchDownload downloads rawURL into dir, the empty directory the caller owns, and returns the tree
// it materialized. The whole directory is removed on failure, so a rejected download leaves nothing
// behind. The body is sniffed by magic bytes; a single skill becomes <dir>/<name>/SKILL.md and an
// archive is extracted under the documented limits.
func FetchDownload(ctx context.Context, rawURL, dir string) (Download, error) {
	return fetchDownloadLimited(ctx, rawURL, dir, downloadLimits())
}

// fetchDownloadLimited is FetchDownload with other fallback limits, which the documented variables
// can still raise. Notion exports use the larger Notion limits.
func fetchDownloadLimited(ctx context.Context, rawURL, dir string, l limits) (Download, error) {
	if dir == "" {
		return Download{}, errors.New("no temporary directory was provided for the download")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Download{}, fmt.Errorf("creating the download directory: %w", err)
	}
	dl, err := fetchDownload(ctx, rawURL, dir, l)
	if err != nil {
		_ = os.RemoveAll(dir)
		return Download{}, err
	}
	return dl, nil
}

// fetchDownload stages the body, sniffs it and materializes it inside dir.
func fetchDownload(ctx context.Context, rawURL, dir string, l limits) (Download, error) {
	body := filepath.Join(dir, bodyFile)
	contentType, head, err := fetchBody(ctx, rawURL, body, l.downloadBytes)
	if err != nil {
		return Download{}, err
	}
	format := sniffFormat(head)
	if format == formatSkill {
		return singleFileDownload(body, dir, rawURL, contentType)
	}
	root, err := extractArchive(body, filepath.Join(dir, extractDir), format, l)
	if err != nil {
		if errors.Is(err, errNotAnArchive) {
			return Download{}, skillContentError(contentType, head)
		}
		return Download{}, err
	}
	_ = os.Remove(body)
	return Download{Root: collapseTopLevel(root)}, nil
}

// fetchBody performs one request and writes its body to dest, refusing anything over limit. It
// returns the declared content type and the first bytes of the body, for sniffing and for the
// content-type error.
func fetchBody(ctx context.Context, rawURL, dest string, limit int64) (string, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", nil, fmt.Errorf("invalid download URL: %s", requestDetail(err))
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := downloadClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("download failed: %s", requestDetail(err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", nil, downloadError{fmt.Sprintf("Download failed with HTTP %d", resp.StatusCode)}
	}
	if resp.ContentLength > limit {
		return "", nil, downloadTooLargeError(limit)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", nil, fmt.Errorf("writing the download: %w", err)
	}
	written, copyErr := io.Copy(f, io.LimitReader(resp.Body, limit+1))
	closeErr := f.Close()
	if copyErr != nil {
		return "", nil, fmt.Errorf("download failed: %s", requestDetail(copyErr))
	}
	if closeErr != nil {
		return "", nil, fmt.Errorf("writing the download: %w", closeErr)
	}
	if written > limit {
		return "", nil, downloadTooLargeError(limit)
	}
	if written == 0 {
		return "", nil, downloadError{"Downloaded URL is empty"}
	}
	head, err := readHead(dest, 512)
	if err != nil {
		return "", nil, err
	}
	return resp.Header.Get("Content-Type"), head, nil
}

// readHead returns up to n leading bytes of the file at p.
func readHead(p string, n int) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, fmt.Errorf("reading the download: %w", err)
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, n)
	read, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("reading the download: %w", err)
	}
	return buf[:read], nil
}

// requestDetail renders a request failure without the URL, which can carry credentials or a secret
// query string.
func requestDetail(err error) string {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return RedactCredentials(uerr.Err.Error())
	}
	return RedactCredentials(err.Error())
}

// sanitizedURL is a URL as it may be shown: userinfo, query and fragment removed.
func sanitizedURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return RedactCredentials(raw)
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}

// downloadLimits reads the documented limits. An environment value only raises a default: a smaller
// or malformed one is ignored, so the defaults are always a floor.
func downloadLimits() limits {
	return downloadLimitsWith(limits{
		downloadBytes: DownloadMaxBytes,
		extractBytes:  ExtractMaxBytes,
		extractFiles:  ExtractMaxFiles,
	})
}

// downloadLimitsWith raises the given fallbacks with the documented variables.
func downloadLimitsWith(fallback limits) limits {
	return limits{
		downloadBytes: raisedBytes(envDownloadMaxBytes, fallback.downloadBytes),
		extractBytes:  raisedBytes(envExtractMaxBytes, fallback.extractBytes),
		extractFiles:  raisedCount(envExtractMaxFiles, fallback.extractFiles),
	}
}

// raisedBytes returns the default unless the named variable holds a larger positive integer.
func raisedBytes(name string, def int64) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(name)), 10, 64)
	if err != nil || v <= def {
		return def
	}
	return v
}

// raisedCount is raisedBytes for a count, parsed as an int so a 32-bit build cannot overflow.
func raisedCount(name string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || v <= def {
		return def
	}
	return v
}

// downloadTooLargeError is the documented message of a body over the limit.
func downloadTooLargeError(limit int64) error {
	return downloadError{fmt.Sprintf("Download is larger than %d bytes. Set %s to override.", limit, envDownloadMaxBytes)}
}

// extractTooLargeError is the documented message of an archive over the extracted-bytes limit.
func extractTooLargeError(limit int64) error {
	return downloadError{fmt.Sprintf("Archive extracts to more than %d bytes. Set %s to override.", limit, envExtractMaxBytes)}
}

// tooManyEntriesError is the documented message of an archive over the entry limit.
func tooManyEntriesError(count, limit int) error {
	return downloadError{fmt.Sprintf("Archive contains too many files (%d). Maximum is %d. Set %s to override.",
		count, limit, envExtractMaxFiles)}
}

// skillContentError names the content type of a body that is neither a skill nor a supported
// archive.
func skillContentError(contentType string, head []byte) error {
	if contentType == "" {
		contentType = http.DetectContentType(head)
	}
	return downloadError{fmt.Sprintf("Downloaded URL is not a valid SKILL.md file or supported archive (content type %s)",
		skill.SanitizeDisplay(contentType))}
}

// archiveFormat is the format of a downloaded body, decided by its magic bytes.
type archiveFormat int

const (
	formatSkill archiveFormat = iota
	formatZip
	formatTar
	formatTarGzip
)

// sniffFormat classifies a body by magic bytes. The URL extension is never trusted.
func sniffFormat(head []byte) archiveFormat {
	switch {
	case len(head) >= 2 && head[0] == 0x1f && head[1] == 0x8b:
		return formatTarGzip
	case bytes.HasPrefix(head, []byte("PK\x03\x04")), bytes.HasPrefix(head, []byte("PK\x05\x06")):
		return formatZip
	case len(head) >= 262 && string(head[257:262]) == "ustar":
		return formatTar
	}
	return formatSkill
}

// singleFileDownload materializes a body that is not an archive as one skill. Anything without
// valid skill frontmatter is an error naming the content type.
func singleFileDownload(body, dir, rawURL, contentType string) (Download, error) {
	raw, err := os.ReadFile(body)
	if err != nil {
		return Download{}, fmt.Errorf("reading the download: %w", err)
	}
	if _, err := skill.ParseSkillMD(raw); err != nil {
		return Download{}, skillContentError(contentType, raw)
	}
	// The body is in memory now, so the staged file can go before the skill folder is created.
	_ = os.Remove(body)
	root := filepath.Join(dir, downloadDirName(rawURL))
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Download{}, fmt.Errorf("writing the download: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, skillFile), raw, 0o644); err != nil {
		return Download{}, fmt.Errorf("writing the download: %w", err)
	}
	return Download{Root: root, SkillPath: skillFile}, nil
}

// downloadDirName is the folder a single downloaded skill is materialized under: the base name of
// the URL without its extension, sanitized, or "skill" when the URL has none.
func downloadDirName(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "skill"
	}
	base := path.Base(u.Path)
	if base == "" || base == "." || base == "/" {
		return "skill"
	}
	return skill.SanitizeName(strings.TrimSuffix(base, path.Ext(base)))
}

// extractArchive extracts a zip or tar body into dest and returns dest.
func extractArchive(body, dest string, format archiveFormat, l limits) (string, error) {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", fmt.Errorf("extracting the archive: %w", err)
	}
	switch format {
	case formatZip:
		r, err := zip.OpenReader(body)
		if err != nil {
			return "", fmt.Errorf("%w: %v", errNotAnArchive, err)
		}
		defer func() { _ = r.Close() }()
		return dest, extractZip(&r.Reader, dest, l)
	case formatTar:
		f, err := os.Open(body)
		if err != nil {
			return "", fmt.Errorf("reading the download: %w", err)
		}
		defer func() { _ = f.Close() }()
		return dest, extractTar(f, dest, l)
	case formatTarGzip:
		f, err := os.Open(body)
		if err != nil {
			return "", fmt.Errorf("reading the download: %w", err)
		}
		defer func() { _ = f.Close() }()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return "", fmt.Errorf("%w: %v", errNotAnArchive, err)
		}
		defer func() { _ = gz.Close() }()
		return dest, extractTar(gz, dest, l)
	default:
		return "", fmt.Errorf("%w: unknown format", errNotAnArchive)
	}
}

// extractZip writes the regular entries of a zip archive under dest, counting every entry towards
// the file limit and every written byte towards the extracted limit.
func extractZip(r *zip.Reader, dest string, l limits) error {
	var total int64
	for i, f := range r.File {
		if i+1 > l.extractFiles {
			return tooManyEntriesError(i+1, l.extractFiles)
		}
		rel, err := archivePath(f.Name)
		if err != nil {
			return err
		}
		target, err := archiveTarget(dest, rel)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("extracting the archive: %w", err)
			}
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 || !f.Mode().IsRegular() {
			return downloadError{"Archive links are not supported"}
		}
		if f.Method != zip.Store && f.Method != zip.Deflate {
			return downloadError{fmt.Sprintf("Unsupported zip compression method: %d", f.Method)}
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("%w: %v", errNotAnArchive, err)
		}
		written, err := writeArchiveFile(target, rc, l.extractBytes-total)
		closeErr := rc.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return fmt.Errorf("%w: %v", errNotAnArchive, closeErr)
		}
		total += written
		if total > l.extractBytes {
			return extractTooLargeError(l.extractBytes)
		}
	}
	return nil
}

// extractTar writes the regular entries of a tar stream under dest. Symlinks, hardlinks and special
// entries are skipped, as upstream does.
func extractTar(r io.Reader, dest string, l limits) error {
	tr := tar.NewReader(r)
	var total int64
	for entries := 0; ; {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: %v", errNotAnArchive, err)
		}
		entries++
		if entries > l.extractFiles {
			return tooManyEntriesError(entries, l.extractFiles)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			rel, err := archivePath(hdr.Name)
			if err != nil {
				return err
			}
			target, err := archiveTarget(dest, rel)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("extracting the archive: %w", err)
			}
		case tar.TypeReg:
			rel, err := archivePath(hdr.Name)
			if err != nil {
				return err
			}
			target, err := archiveTarget(dest, rel)
			if err != nil {
				return err
			}
			written, err := writeArchiveFile(target, tr, l.extractBytes-total)
			if err != nil {
				return err
			}
			total += written
			if total > l.extractBytes {
				return extractTooLargeError(l.extractBytes)
			}
		}
	}
}

// archivePath validates one archive entry name and returns the slash-separated relative path it may
// be extracted to. Absolute paths, drive letters, backslash separators and traversal segments are
// unsafe.
func archivePath(name string) (string, error) {
	p := strings.TrimSuffix(strings.TrimPrefix(name, "./"), "/")
	if p == "" || p == "." || strings.Contains(p, `\`) || strings.HasPrefix(p, "/") || isWindowsAbs(p) {
		return "", unsafeArchivePath(name)
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", unsafeArchivePath(name)
		}
	}
	return p, nil
}

// archiveTarget joins an archive-relative path onto dest and refuses anything that would land
// outside it. Extraction never creates a link, so a lexical check is also true of the real paths.
func archiveTarget(dest, rel string) (string, error) {
	target := filepath.Join(dest, filepath.FromSlash(rel))
	inside, err := filepath.Rel(dest, target)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return "", unsafeArchivePath(rel)
	}
	return target, nil
}

// unsafeArchivePath is the documented rejection of an entry that could escape the extraction root.
func unsafeArchivePath(name string) error {
	return downloadError{fmt.Sprintf("Archive contains unsafe path: %s", skill.SanitizeDisplay(name))}
}

// writeArchiveFile creates target and copies at most remaining+1 bytes into it, so the caller can
// detect an overrun without buffering the entry.
func writeArchiveFile(target string, r io.Reader, remaining int64) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return 0, fmt.Errorf("extracting the archive: %w", err)
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, fmt.Errorf("extracting the archive: %w", err)
	}
	written, copyErr := io.Copy(f, io.LimitReader(r, remaining+1))
	closeErr := f.Close()
	if copyErr != nil {
		return written, fmt.Errorf("extracting the archive: %w", copyErr)
	}
	if closeErr != nil {
		return written, fmt.Errorf("extracting the archive: %w", closeErr)
	}
	return written, nil
}

// collapseTopLevel returns the single visible top-level directory of an extracted archive, so that
// pkg/skill/SKILL.md installs as skill. Finder's __MACOSX metadata is ignored. Anything else keeps
// the extraction root.
func collapseTopLevel(root string) string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return root
	}
	dirs := make([]string, 0, 1)
	for _, entry := range entries {
		if entry.Name() == "__MACOSX" {
			continue
		}
		if !entry.IsDir() {
			return root
		}
		dirs = append(dirs, entry.Name())
	}
	if len(dirs) != 1 {
		return root
	}
	return filepath.Join(root, dirs[0])
}
