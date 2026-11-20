package source

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// skillDoc is a valid SKILL.md body used by the download tests.
const skillDoc = "---\nname: alpha\ndescription: test skill\n---\nbody\n"

// staticServer serves body at every path. The returned URL has no extension, so the tests prove
// that the format is sniffed from the bytes.
func staticServer(t *testing.T, contentType string, body []byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); !strings.Contains(ua, "skills-cli") {
			t.Errorf("User-Agent = %q, want a descriptive client", ua)
		}
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/download"
}

// archiveEntry is one file of a test archive.
type archiveEntry struct {
	name    string
	body    []byte
	size    int64 // when positive, zero bytes are written instead of body
	symlink bool
	link    string // symlink target
}

// zeroReader yields an endless run of zero bytes without allocating them.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func zipBytes(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.symlink {
			hdr.SetMode(os.ModeSymlink | 0o777)
		} else {
			hdr.SetMode(0o644)
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("zip entry %q: %v", e.name, err)
		}
		writeEntryBody(t, w, e)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("closing the zip: %v", err)
	}
	return buf.Bytes()
}

func tarBytes(t *testing.T, entries []archiveEntry, compress bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	var out io.Writer = &buf
	var gz *gzip.Writer
	if compress {
		gz = gzip.NewWriter(&buf)
		out = gz
	}
	tw := tar.NewWriter(out)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body))}
		if e.size > 0 {
			hdr.Size = e.size
		}
		switch {
		case e.symlink:
			hdr.Typeflag = tar.TypeSymlink
			hdr.Linkname = e.link
			hdr.Size = 0
		case strings.HasSuffix(e.name, "/"):
			hdr.Typeflag = tar.TypeDir
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar entry %q: %v", e.name, err)
		}
		if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeDir {
			continue
		}
		writeEntryBody(t, tw, e)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("closing the tar: %v", err)
	}
	if gz != nil {
		if err := gz.Close(); err != nil {
			t.Fatalf("closing gzip: %v", err)
		}
	}
	return buf.Bytes()
}

// writeEntryBody writes the content of a test entry, padding with zeros when size is set.
func writeEntryBody(t *testing.T, w io.Writer, e archiveEntry) {
	t.Helper()
	if e.size > 0 {
		if _, err := io.CopyN(w, zeroReader{}, e.size); err != nil {
			t.Fatalf("writing %d zero bytes: %v", e.size, err)
		}
		return
	}
	if _, err := w.Write(e.body); err != nil {
		t.Fatalf("writing entry %q: %v", e.name, err)
	}
}

// fetchTo downloads rawURL into a fresh directory and returns the result and that directory.
func fetchTo(t *testing.T, rawURL string) (Download, string, error) {
	t.Helper()
	dir := t.TempDir()
	dl, err := FetchDownload(context.Background(), rawURL, dir)
	return dl, dir, err
}

func TestFetchDownloadSingleSkill(t *testing.T) {
	url := staticServer(t, "text/markdown", []byte(skillDoc))
	dl, dir, err := fetchTo(t, url)
	if err != nil {
		t.Fatalf("FetchDownload: %v", err)
	}
	if dl.SkillPath != skillFile {
		t.Errorf("SkillPath = %q, want %q", dl.SkillPath, skillFile)
	}
	if filepath.Dir(dl.Root) != dir {
		t.Errorf("Root = %q, want a directory inside %q", dl.Root, dir)
	}
	got, err := os.ReadFile(filepath.Join(dl.Root, skillFile))
	if err != nil {
		t.Fatalf("materialized SKILL.md: %v", err)
	}
	if string(got) != skillDoc {
		t.Errorf("SKILL.md = %q, want the served body", got)
	}
	if _, err := os.Stat(filepath.Join(dir, bodyFile)); !os.IsNotExist(err) {
		t.Errorf("the staged body was left behind: %v", err)
	}
}

func TestFetchDownloadRejectsABodyThatIsNotASkill(t *testing.T) {
	url := staticServer(t, "text/html; charset=utf-8", []byte("<html><body>nope</body></html>"))
	_, dir, err := fetchTo(t, url)
	if err == nil {
		t.Fatal("FetchDownload succeeded, want a content-type error")
	}
	if !strings.Contains(err.Error(), "content type text/html") {
		t.Errorf("err = %q, want the content type", err)
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Errorf("the temporary tree was left behind: %v", statErr)
	}
}

func TestFetchDownloadSniffsArchives(t *testing.T) {
	pkg := []archiveEntry{{name: "pkg/alpha/" + skillFile, body: []byte(skillDoc)}}
	tarBody := tarBytes(t, pkg, false)
	tests := map[string][]byte{
		"zip":    zipBytes(t, pkg),
		"tar":    tarBody,
		"tar.gz": tarBytes(t, pkg, true),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			url := staticServer(t, "application/octet-stream", body)
			dl, _, err := fetchTo(t, url)
			if err != nil {
				t.Fatalf("FetchDownload: %v", err)
			}
			if dl.SkillPath != "" {
				t.Errorf("SkillPath = %q, want empty for an archive", dl.SkillPath)
			}
			if _, err := os.Stat(filepath.Join(dl.Root, "alpha", skillFile)); err != nil {
				t.Errorf("collapsed archive missing alpha/SKILL.md: %v", err)
			}
		})
	}
}

func TestFetchDownloadCollapsesOneTopLevelDirectory(t *testing.T) {
	dl, _, err := fetchTo(t, staticServer(t, "", zipBytes(t, []archiveEntry{
		{name: "pkg/" + skillFile, body: []byte(skillDoc)},
		{name: "__MACOSX/junk", body: []byte("x")},
	})))
	if err != nil {
		t.Fatalf("FetchDownload: %v", err)
	}
	if filepath.Base(dl.Root) != "pkg" {
		t.Errorf("Root = %q, want the collapsed pkg directory", dl.Root)
	}

	dl, _, err = fetchTo(t, staticServer(t, "", zipBytes(t, []archiveEntry{
		{name: "a/" + skillFile, body: []byte(skillDoc)},
		{name: "b/" + skillFile, body: []byte(skillDoc)},
	})))
	if err != nil {
		t.Fatalf("FetchDownload: %v", err)
	}
	if filepath.Base(dl.Root) != extractDir {
		t.Errorf("Root = %q, want the extraction root when two directories exist", dl.Root)
	}
	for _, dir := range []string{"a", "b"} {
		if _, err := os.Stat(filepath.Join(dl.Root, dir, skillFile)); err != nil {
			t.Errorf("%s/SKILL.md missing: %v", dir, err)
		}
	}
}

// paddedSkill is the skill document padded with spaces to exactly size bytes.
func paddedSkill(size int64) []byte {
	if size <= int64(len(skillDoc)) {
		return []byte(skillDoc)[:size]
	}
	return append([]byte(skillDoc), bytes.Repeat([]byte(" "), int(size)-len(skillDoc))...)
}

func TestFetchDownloadBodyLimit(t *testing.T) {
	if _, _, err := fetchTo(t, staticServer(t, "", paddedSkill(DownloadMaxBytes))); err != nil {
		t.Fatalf("a body of exactly the limit was refused: %v", err)
	}

	_, dir, err := fetchTo(t, staticServer(t, "", paddedSkill(DownloadMaxBytes+1)))
	if err == nil || !strings.Contains(err.Error(), "larger than 10485760 bytes") {
		t.Fatalf("err = %v, want the body limit", err)
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Errorf("the temporary tree was left behind: %v", statErr)
	}
}

func TestFetchDownloadBodyLimitAppliesWithoutContentLength(t *testing.T) {
	body := paddedSkill(DownloadMaxBytes + 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	_, err := FetchDownload(context.Background(), srv.URL+"/download", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "larger than 10485760 bytes") {
		t.Fatalf("err = %v, want the streaming body limit", err)
	}
}

func TestFetchDownloadEnvironmentRaisesButNeverLowersLimits(t *testing.T) {
	t.Setenv(envDownloadMaxBytes, strconv.FormatInt(DownloadMaxBytes+4096, 10))
	if _, _, err := fetchTo(t, staticServer(t, "", paddedSkill(DownloadMaxBytes+1))); err != nil {
		t.Fatalf("a raised SKILLS_DOWNLOAD_MAX_BYTES did not allow the body: %v", err)
	}

	t.Setenv(envDownloadMaxBytes, "16")
	if _, _, err := fetchTo(t, staticServer(t, "text/markdown", []byte(skillDoc))); err != nil {
		t.Fatalf("a lowered SKILLS_DOWNLOAD_MAX_BYTES was honoured: %v", err)
	}
}

func TestFetchDownloadExtractLimit(t *testing.T) {
	entries := func(pad int64) []archiveEntry {
		return []archiveEntry{
			{name: "skill/" + skillFile, body: []byte(skillDoc)},
			{name: "skill/pad.bin", size: pad},
		}
	}
	exact := ExtractMaxBytes - int64(len(skillDoc))
	atLimit, overLimit := zipBytes(t, entries(exact)), zipBytes(t, entries(exact+1))

	if _, _, err := fetchTo(t, staticServer(t, "", atLimit)); err != nil {
		t.Fatalf("an archive of exactly the extracted limit was refused: %v", err)
	}
	_, err := FetchDownload(context.Background(), staticServer(t, "", overLimit), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "more than 26214400 bytes") {
		t.Fatalf("err = %v, want the extracted-bytes limit", err)
	}

	t.Setenv(envExtractMaxBytes, strconv.FormatInt(ExtractMaxBytes+1<<20, 10))
	if _, _, err := fetchTo(t, staticServer(t, "", overLimit)); err != nil {
		t.Fatalf("a raised SKILLS_EXTRACT_MAX_BYTES did not allow the archive: %v", err)
	}

	t.Setenv(envExtractMaxBytes, "16")
	if _, _, err := fetchTo(t, staticServer(t, "", atLimit)); err != nil {
		t.Fatalf("a lowered SKILLS_EXTRACT_MAX_BYTES was honoured: %v", err)
	}
}

func TestFetchDownloadEntryLimit(t *testing.T) {
	entries := func(n int) []archiveEntry {
		out := []archiveEntry{{name: skillFile, body: []byte(skillDoc)}}
		for i := 1; i < n; i++ {
			out = append(out, archiveEntry{name: fmt.Sprintf("f%04d.txt", i), body: []byte("x")})
		}
		return out
	}
	if _, _, err := fetchTo(t, staticServer(t, "", zipBytes(t, entries(ExtractMaxFiles)))); err != nil {
		t.Fatalf("an archive of exactly the entry limit was refused: %v", err)
	}
	_, err := FetchDownload(context.Background(),
		staticServer(t, "", zipBytes(t, entries(ExtractMaxFiles+1))), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "too many files (1001)") {
		t.Fatalf("err = %v, want the entry limit", err)
	}

	t.Setenv(envExtractMaxFiles, "1005")
	over := append(entries(ExtractMaxFiles), archiveEntry{name: "extra.txt", body: []byte("x")})
	over = append(over, archiveEntry{name: "extra2.txt", body: []byte("x")})
	if _, _, err := fetchTo(t, staticServer(t, "", zipBytes(t, over))); err != nil {
		t.Fatalf("a raised SKILLS_EXTRACT_MAX_FILES did not allow the entries: %v", err)
	}

	t.Setenv(envExtractMaxFiles, "1")
	if _, _, err := fetchTo(t, staticServer(t, "", zipBytes(t, entries(2)))); err != nil {
		t.Fatalf("a lowered SKILLS_EXTRACT_MAX_FILES was honoured: %v", err)
	}
}

func TestFetchDownloadRejectsUnsafeArchivePaths(t *testing.T) {
	unsafe := []string{"../evil.txt", "/abs.txt", "C:/evil.txt", `..\evil.txt`, "a/../b.txt"}
	builders := map[string]func([]archiveEntry) []byte{
		"zip":    func(es []archiveEntry) []byte { return zipBytes(t, es) },
		"tar":    func(es []archiveEntry) []byte { return tarBytes(t, es, false) },
		"tar.gz": func(es []archiveEntry) []byte { return tarBytes(t, es, true) },
	}
	for format, build := range builders {
		for _, name := range unsafe {
			t.Run(format+"/"+name, func(t *testing.T) {
				entries := []archiveEntry{{name: skillFile, body: []byte(skillDoc)}, {name: name, body: []byte("evil")}}
				url := staticServer(t, "", build(entries))
				_, dir, err := fetchTo(t, url)
				if err == nil || !strings.Contains(err.Error(), "unsafe path") {
					t.Fatalf("err = %v, want an unsafe-path rejection", err)
				}
				if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
					t.Errorf("the temporary tree was left behind: %v", statErr)
				}
				if _, statErr := os.Stat(filepath.Join(filepath.Dir(dir), "evil.txt")); !os.IsNotExist(statErr) {
					t.Errorf("an archive entry escaped the temporary tree: %v", statErr)
				}
			})
		}
	}
}

func TestFetchDownloadSymlinkPolicy(t *testing.T) {
	_, err := FetchDownload(context.Background(), staticServer(t, "", zipBytes(t, []archiveEntry{
		{name: skillFile, body: []byte(skillDoc)},
		{name: "link", symlink: true, link: "../../etc/passwd"},
	})), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "Archive links are not supported") {
		t.Fatalf("err = %v, want a zip symlink rejection", err)
	}

	dl, _, err := fetchTo(t, staticServer(t, "", tarBytes(t, []archiveEntry{
		{name: skillFile, body: []byte(skillDoc)},
		{name: "link", symlink: true, link: "../../etc/passwd"},
	}, false)))
	if err != nil {
		t.Fatalf("FetchDownload: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dl.Root, "link")); !os.IsNotExist(err) {
		t.Errorf("the tar symlink was materialized: %v", err)
	}
}

func TestFetchDownloadHTTPFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/empty":
			w.WriteHeader(http.StatusOK)
		case "/moved":
			http.Redirect(w, r, "/skill", http.StatusFound)
		case "/skill":
			_, _ = w.Write([]byte(skillDoc))
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	if _, _, err := fetchTo(t, srv.URL+"/moved"); err != nil {
		t.Fatalf("a redirect was not followed: %v", err)
	}
	if _, err := FetchDownload(context.Background(), srv.URL+"/empty", t.TempDir()); err == nil ||
		!strings.Contains(err.Error(), "Downloaded URL is empty") {
		t.Fatalf("err = %v, want the empty-body error", err)
	}
	if _, err := FetchDownload(context.Background(), srv.URL+"/loop", t.TempDir()); err == nil ||
		!strings.Contains(err.Error(), "stopped after 5 redirects") {
		t.Fatalf("err = %v, want the redirect limit", err)
	}
}

// TestFetchDownloadErrorsHideTheURL proves a failing download never echoes credentials or a secret
// query string.
func TestFetchDownloadErrorsHideTheURL(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	raw := strings.Replace(srv.URL, "http://", "http://user:secret@", 1) + "/missing?token=abc123"

	_, err := FetchDownload(context.Background(), raw, t.TempDir())
	if err == nil {
		t.Fatal("FetchDownload succeeded, want a failure")
	}
	if text := err.Error(); strings.Contains(text, "secret") || strings.Contains(text, "abc123") {
		t.Errorf("error leaks the URL: %q", text)
	}
	if !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("err = %q, want the status", err)
	}
}

func TestSourceDisplayAndRecordable(t *testing.T) {
	tests := []struct {
		name       string
		source     Source
		display    string
		recordable bool
	}{
		{
			name:       "plain download",
			source:     Source{Kind: KindDownload, Source: "https://example.com/a/skill.md"},
			display:    "https://example.com/a/skill.md",
			recordable: true,
		},
		{
			name:       "download with a query",
			source:     Source{Kind: KindDownload, Source: "https://example.com/a/skill.md?token=x"},
			display:    "https://example.com/a/skill.md",
			recordable: false,
		},
		{
			name:       "download with userinfo and fragment",
			source:     Source{Kind: KindDownload, Source: "https://u:p@example.com/a/skill.md#frag"},
			display:    "https://example.com/a/skill.md",
			recordable: false,
		},
		{
			name:       "git source is not sanitized",
			source:     Source{Kind: KindGit, Source: "git@github.com:o/r.git"},
			display:    "git@github.com:o/r.git",
			recordable: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.source.Display(); got != tc.display {
				t.Errorf("Display = %q, want %q", got, tc.display)
			}
			if got := tc.source.Recordable(); got != tc.recordable {
				t.Errorf("Recordable = %v, want %v", got, tc.recordable)
			}
		})
	}
}
