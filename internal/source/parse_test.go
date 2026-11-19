package source

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testSHA = "0123456789abcdef0123456789abcdef01234567"

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want Source
	}{
		{
			name: "shorthand",
			arg:  "owner/repo",
			want: Source{Kind: KindGitHub, Source: "owner/repo"},
		},
		{
			name: "shorthand trailing slash",
			arg:  "owner/repo/",
			want: Source{Kind: KindGitHub, Source: "owner/repo"},
		},
		{
			name: "shorthand subpath",
			arg:  "owner/repo/skills/x",
			want: Source{Kind: KindGitHub, Source: "owner/repo", Subpath: "skills/x"},
		},
		{
			name: "shorthand double slash subpath",
			arg:  "owner/repo//skills/x",
			want: Source{Kind: KindGitHub, Source: "owner/repo", Subpath: "skills/x"},
		},
		{
			name: "shorthand dots are normalized",
			arg:  "owner/repo.git",
			want: Source{Kind: KindGitHub, Source: "owner/repo"},
		},
		{
			name: "skill selector",
			arg:  "owner/repo@my-skill",
			want: Source{Kind: KindGitHub, Source: "owner/repo", SkillSelector: "my-skill", Direct: true},
		},
		{
			name: "version is a ref",
			arg:  "owner/repo@v1.3.1",
			want: Source{Kind: KindGitHub, Source: "owner/repo", Ref: "v1.3.1"},
		},
		{
			name: "bare version is a ref",
			arg:  "owner/repo@1.2.3",
			want: Source{Kind: KindGitHub, Source: "owner/repo", Ref: "1.2.3"},
		},
		{
			name: "prerelease version is a ref",
			arg:  "owner/repo@v1.2.3-beta.1+build.5",
			want: Source{Kind: KindGitHub, Source: "owner/repo", Ref: "v1.2.3-beta.1+build.5"},
		},
		{
			name: "commit is a ref",
			arg:  "owner/repo@" + testSHA,
			want: Source{Kind: KindGitHub, Source: "owner/repo", Ref: testSHA},
		},
		{
			name: "major only is a skill",
			arg:  "owner/repo@v2",
			want: Source{Kind: KindGitHub, Source: "owner/repo", SkillSelector: "v2", Direct: true},
		},
		{
			name: "single number is a skill",
			arg:  "owner/repo@2",
			want: Source{Kind: KindGitHub, Source: "owner/repo", SkillSelector: "2", Direct: true},
		},
		{
			name: "wildcard version is a skill",
			arg:  "owner/repo@v1.x",
			want: Source{Kind: KindGitHub, Source: "owner/repo", SkillSelector: "v1.x", Direct: true},
		},
		{
			name: "name is a skill",
			arg:  "owner/repo@find-skills",
			want: Source{Kind: KindGitHub, Source: "owner/repo", SkillSelector: "find-skills", Direct: true},
		},
		{
			name: "fragment ref",
			arg:  "owner/repo#branch",
			want: Source{Kind: KindGitHub, Source: "owner/repo", Ref: "branch"},
		},
		{
			name: "fragment ref with subpath",
			arg:  "owner/repo/skills/x#feature/y",
			want: Source{Kind: KindGitHub, Source: "owner/repo", Ref: "feature/y", Subpath: "skills/x"},
		},
		{
			name: "fragment ref and skill",
			arg:  "owner/repo#branch@skill",
			want: Source{Kind: KindGitHub, Source: "owner/repo", Ref: "branch", SkillSelector: "skill", Direct: true},
		},
		{
			name: "fragment ref makes the suffix a skill",
			arg:  "owner/repo@v1.2.3#main",
			want: Source{Kind: KindGitHub, Source: "owner/repo", Ref: "main", SkillSelector: "v1.2.3", Direct: true},
		},
		{
			name: "fragment values are percent-decoded",
			arg:  "owner/repo#release%40v1@code%20review",
			want: Source{Kind: KindGitHub, Source: "owner/repo", Ref: "release@v1", SkillSelector: "code review", Direct: true},
		},
		{
			name: "github prefix",
			arg:  "github:owner/repo",
			want: Source{Kind: KindGitHub, Source: "owner/repo"},
		},
		{
			name: "github prefix with subpath and skill",
			arg:  "github:owner/repo/skills/x@y",
			want: Source{Kind: KindGitHub, Source: "owner/repo", Subpath: "skills/x", SkillSelector: "y", Direct: true},
		},
		{
			name: "github prefix with fragment",
			arg:  "github:owner/repo#main@skill",
			want: Source{Kind: KindGitHub, Source: "owner/repo", Ref: "main", SkillSelector: "skill", Direct: true},
		},
		{
			name: "gitlab prefix",
			arg:  "gitlab:group/sub/repo",
			want: Source{Kind: KindGitLab, Source: "https://gitlab.com/group/sub/repo.git", SourceURL: "https://gitlab.com/group/sub/repo.git"},
		},
		{
			name: "github https",
			arg:  "https://github.com/o/r",
			want: Source{Kind: KindGitHub, Source: "o/r", SourceURL: "https://github.com/o/r.git"},
		},
		{
			name: "github https with git suffix",
			arg:  "https://github.com/o/r.git",
			want: Source{Kind: KindGitHub, Source: "o/r", SourceURL: "https://github.com/o/r.git"},
		},
		{
			name: "github tree ref",
			arg:  "https://github.com/o/r/tree/main",
			want: Source{Kind: KindGitHub, Source: "o/r", SourceURL: "https://github.com/o/r.git", Ref: "main"},
		},
		{
			name: "github tree ref and subpath",
			arg:  "https://github.com/o/r/tree/main/skills/x",
			want: Source{
				Kind: KindGitHub, Source: "o/r", SourceURL: "https://github.com/o/r.git",
				Ref: "main", Subpath: "skills/x",
			},
		},
		{
			name: "github blob carries no ref",
			arg:  "https://github.com/o/r/blob/main/x.md#L10",
			want: Source{Kind: KindGitHub, Source: "o/r", SourceURL: "https://github.com/o/r.git"},
		},
		{
			name: "github extra path is discarded",
			arg:  "https://github.com/o/r/anything",
			want: Source{Kind: KindGitHub, Source: "o/r", SourceURL: "https://github.com/o/r.git"},
		},
		{
			name: "github scheme and www are optional",
			arg:  "www.github.com/o/r?tab=readme-ov-file",
			want: Source{Kind: KindGitHub, Source: "o/r", SourceURL: "https://github.com/o/r.git"},
		},
		{
			name: "github fragment ref",
			arg:  "https://github.com/o/r#feature/x",
			want: Source{Kind: KindGitHub, Source: "o/r", SourceURL: "https://github.com/o/r.git", Ref: "feature/x"},
		},
		{
			name: "gitlab https",
			arg:  "https://gitlab.com/o/r",
			want: Source{Kind: KindGitLab, Source: "https://gitlab.com/o/r.git", SourceURL: "https://gitlab.com/o/r.git"},
		},
		{
			name: "gitlab subgroups",
			arg:  "https://gitlab.com/group/sub/repo.git",
			want: Source{Kind: KindGitLab, Source: "https://gitlab.com/group/sub/repo.git", SourceURL: "https://gitlab.com/group/sub/repo.git"},
		},
		{
			name: "gitlab personal namespace",
			arg:  "https://gitlab.com/~user/project",
			want: Source{Kind: KindGitLab, Source: "https://gitlab.com/~user/project.git", SourceURL: "https://gitlab.com/~user/project.git"},
		},
		{
			name: "gitlab tree",
			arg:  "https://gitlab.com/group/sub/repo/-/tree/main/src",
			want: Source{
				Kind: KindGitLab, Source: "https://gitlab.com/group/sub/repo.git",
				SourceURL: "https://gitlab.com/group/sub/repo.git", Ref: "main", Subpath: "src",
			},
		},
		{
			name: "gitlab fragment ref",
			arg:  "https://gitlab.com/o/r#feature/x",
			want: Source{
				Kind: KindGitLab, Source: "https://gitlab.com/o/r.git",
				SourceURL: "https://gitlab.com/o/r.git", Ref: "feature/x",
			},
		},
		{
			name: "gitlab tree on another host",
			arg:  "https://git.corp.com:8443/group/sub/proj/-/tree/main/src",
			want: Source{
				Kind: KindGitLab, Source: "https://git.corp.com:8443/group/sub/proj.git",
				SourceURL: "https://git.corp.com:8443/group/sub/proj.git", Ref: "main", Subpath: "src",
			},
		},
		{
			name: "azure devops",
			arg:  "https://dev.azure.com/org/project/_git/repo",
			want: Source{
				Kind: KindAzure, Source: "https://dev.azure.com/org/project/_git/repo",
				SourceURL: "https://dev.azure.com/org/project/_git/repo",
			},
		},
		{
			name: "azure branch and path",
			arg:  "https://dev.azure.com/org/project/_git/repo?path=/skills/x&version=GBmain",
			want: Source{
				Kind: KindAzure, Source: "https://dev.azure.com/org/project/_git/repo",
				SourceURL: "https://dev.azure.com/org/project/_git/repo", Ref: "main", Subpath: "skills/x",
			},
		},
		{
			name: "azure tag",
			arg:  "https://org.visualstudio.com/project/_git/repo?version=GTv1.0",
			want: Source{
				Kind: KindAzure, Source: "https://org.visualstudio.com/project/_git/repo",
				SourceURL: "https://org.visualstudio.com/project/_git/repo", Ref: "v1.0",
			},
		},
		{
			name: "azure commit version is ignored",
			arg:  "https://host/tfs/collection/project/_git/repo?version=GC" + testSHA,
			want: Source{
				Kind: KindAzure, Source: "https://host/tfs/collection/project/_git/repo",
				SourceURL: "https://host/tfs/collection/project/_git/repo",
			},
		},
		{
			name: "azure fragment is the ref fallback",
			arg:  "https://dev.azure.com/org/project/_git/repo?version=GC" + testSHA + "#main",
			want: Source{
				Kind: KindAzure, Source: "https://dev.azure.com/org/project/_git/repo",
				SourceURL: "https://dev.azure.com/org/project/_git/repo", Ref: "main",
			},
		},
		{
			name: "azure repo segment is re-encoded",
			arg:  "https://dev.azure.com/org/project/_git/my%20repo",
			want: Source{
				Kind: KindAzure, Source: "https://dev.azure.com/org/project/_git/my%20repo",
				SourceURL: "https://dev.azure.com/org/project/_git/my%20repo",
			},
		},
		{
			name: "git scp",
			arg:  "git@github.com:o/r.git",
			want: Source{Kind: KindGit, Source: "git@github.com:o/r.git", SourceURL: "git@github.com:o/r.git"},
		},
		{
			name: "git scp normalizes the suffix",
			arg:  "git@github.com:o/r",
			want: Source{Kind: KindGit, Source: "git@github.com:o/r.git", SourceURL: "git@github.com:o/r.git"},
		},
		{
			name: "git scp tree",
			arg:  "git@github.com:o/r/tree/main/skills/x",
			want: Source{
				Kind: KindGit, Source: "git@github.com:o/r.git", SourceURL: "git@github.com:o/r.git",
				Ref: "main", Subpath: "skills/x",
			},
		},
		{
			name: "git scp on another host is verbatim",
			arg:  "git@other-host:group/repo/tree/x.git",
			want: Source{
				Kind: KindGit, Source: "git@other-host:group/repo/tree/x.git",
				SourceURL: "git@other-host:group/repo/tree/x.git",
			},
		},
		{
			name: "ssh url",
			arg:  "ssh://git@host:7999/o/r.git",
			want: Source{Kind: KindGit, Source: "ssh://git@host:7999/o/r.git", SourceURL: "ssh://git@host:7999/o/r.git"},
		},
		{
			name: "generic https git with fragment",
			arg:  "https://git.example.com/o/r.git#main",
			want: Source{
				Kind: KindGit, Source: "https://git.example.com/o/r.git",
				SourceURL: "https://git.example.com/o/r.git", Ref: "main",
			},
		},
		{
			name: "file url",
			arg:  "file:///tmp/repo.git#v1",
			want: Source{Kind: KindGit, Source: "file:///tmp/repo.git", SourceURL: "file:///tmp/repo.git", Ref: "v1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.arg)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.arg, err)
			}
			if got != tc.want {
				t.Errorf("Parse(%q) =\n  %+v\nwant\n  %+v", tc.arg, got, tc.want)
			}
		})
	}
}

func TestValidateSubpath(t *testing.T) {
	valid := []string{"", "skills/x", ".hidden/x", "..skill/x", "skill../x"}
	for _, sub := range valid {
		if _, err := validateSubpath(sub); err != nil {
			t.Errorf("validateSubpath(%q) = %v, want it accepted", sub, err)
		}
	}
	invalid := map[string]string{
		"/etc":     "relative",
		"C:/x":     "relative",
		`skills\x`: "separators",
		"a/../b":   "..",
		"..":       "..",
	}
	for sub, want := range invalid {
		_, err := validateSubpath(sub)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("validateSubpath(%q) = %v, want an error mentioning %q", sub, err, want)
		}
	}
}

func TestParseLocalPaths(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	tests := []struct{ name, arg, want string }{
		{name: "dot", arg: ".", want: dir},
		{name: "dot dot", arg: "..", want: filepath.Dir(dir)},
		{name: "relative", arg: "./my-skills", want: filepath.Join(dir, "my-skills")},
		{name: "parent relative", arg: "../x", want: filepath.Join(filepath.Dir(dir), "x")},
		{name: "home", arg: "~/skills", want: filepath.Join(home, "skills")},
	}
	if runtime.GOOS != "windows" {
		tests = append(tests, struct{ name, arg, want string }{
			name: "absolute", arg: "/home/u/skills", want: "/home/u/skills",
		})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.arg)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.arg, err)
			}
			want := Source{Kind: KindLocal, Source: tc.want, SourceURL: tc.want}
			if got != want {
				t.Errorf("Parse(%q) = %+v, want %+v", tc.arg, got, want)
			}
		})
	}
}

func TestParseRejects(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want string
	}{
		{name: "parent subpath", arg: "owner/repo/../x", want: ".."},
		{name: "parent subpath after double slash", arg: "owner/repo//../../x", want: ".."},
		{name: "absolute subpath in tree url", arg: "https://github.com/o/r/tree/main/../etc", want: ".."},
		{name: "encoded parent subpath", arg: "https://github.com/o/r/tree/main/%2E%2E/etc", want: ".."},
		{name: "backslash subpath", arg: `owner/repo/skills\x`, want: "separators"},
		{name: "backslash in tree url", arg: `https://github.com/o/r/tree/main/a\b`, want: "separators"},
		{name: "userinfo", arg: "https://user:token@github.com/o/r", want: "credentials"},
		{name: "azure parent subpath", arg: "https://dev.azure.com/o/p/_git/r?path=/settings/../etc", want: ".."},
		{name: "missing repository", arg: "https://github.com/o", want: "owner/repo"},
		{name: "empty repository", arg: "owner/", want: "repository"},
		{name: "plain word", arg: "skills", want: "not a local directory"},
		{name: "host with port and no scheme", arg: "localhost:8080/o/r.git", want: "not a local directory"},
		{name: "npm spec", arg: "npm:foo", want: "not a local directory"},
		{name: "generic download url", arg: "https://example.com/skills.zip", want: "not a local directory"},
		{name: "ext transport", arg: "ext::sh -c evil", want: "ext"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.arg)
			if err == nil {
				t.Fatalf("Parse(%q) succeeded, want an error", tc.arg)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Parse(%q) error = %q, want containing %q", tc.arg, err, tc.want)
			}
		})
	}
	if _, err := Parse("https://user:token@github.com/o/r"); err != nil &&
		strings.Contains(err.Error(), "token") {
		t.Errorf("error echoes the credential: %v", err)
	}
}

func TestCloneURL(t *testing.T) {
	tests := map[string]string{
		"owner/repo":                      "https://github.com/owner/repo.git",
		"https://github.com/o/r":          "https://github.com/o/r.git",
		"gitlab:group/repo":               "https://gitlab.com/group/repo.git",
		"https://git.example.com/o/r.git": "https://git.example.com/o/r.git",
		"git@github.com:o/r.git":          "git@github.com:o/r.git",
	}
	for arg, want := range tests {
		src, err := Parse(arg)
		if err != nil {
			t.Fatalf("Parse(%q): %v", arg, err)
		}
		if got := src.CloneURL(); got != want {
			t.Errorf("CloneURL(%q) = %q, want %q", arg, got, want)
		}
	}
}
