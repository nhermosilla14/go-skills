package source

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// recordingRunner returns canned output and records the commands it received.
type recordingRunner struct {
	out  Output
	err  error
	cmds []Command
}

func (r *recordingRunner) Run(_ context.Context, cmd Command) (Output, error) {
	r.cmds = append(r.cmds, cmd)
	return r.out, r.err
}

func TestRemoteHeadDefaultRef(t *testing.T) {
	head := strings.Repeat("a", 40)
	runner := &recordingRunner{out: Output{Stdout: head + "\tHEAD\n"}}
	src := Source{Kind: KindGit, Source: "file:///repo", SourceURL: "file:///repo"}

	commit, err := RemoteHead(context.Background(), src, runner)
	if err != nil {
		t.Fatalf("RemoteHead: %v", err)
	}
	if commit != head {
		t.Errorf("commit = %s, want %s", commit, head)
	}
	args := strings.Join(runner.cmds[0].Args, " ")
	for _, want := range []string{"ls-remote", "file:///repo", "HEAD"} {
		if !strings.Contains(args, want) {
			t.Errorf("args = %q, want %q", args, want)
		}
	}
}

func TestRemoteHeadPrefersThePeeledTag(t *testing.T) {
	tagObject := strings.Repeat("b", 40)
	commit := strings.Repeat("c", 40)
	runner := &recordingRunner{out: Output{Stdout: tagObject + "\trefs/tags/v1\n" +
		commit + "\trefs/tags/v1^{}\n"}}
	src := Source{Kind: KindGit, Source: "file:///repo", SourceURL: "file:///repo", Ref: "v1"}

	got, err := RemoteHead(context.Background(), src, runner)
	if err != nil {
		t.Fatalf("RemoteHead: %v", err)
	}
	if got != commit {
		t.Errorf("commit = %s, want the peeled %s", got, commit)
	}
}

func TestRemoteHeadPrefersTheBranch(t *testing.T) {
	branch := strings.Repeat("d", 40)
	tag := strings.Repeat("e", 40)
	runner := &recordingRunner{out: Output{Stdout: tag + "\trefs/tags/x\n" + branch + "\trefs/heads/x\n"}}
	src := Source{Kind: KindGit, Source: "file:///repo", SourceURL: "file:///repo", Ref: "x"}

	got, err := RemoteHead(context.Background(), src, runner)
	if err != nil {
		t.Fatalf("RemoteHead: %v", err)
	}
	if got != branch {
		t.Errorf("commit = %s, want the branch %s", got, branch)
	}
}

func TestRemoteHeadFailures(t *testing.T) {
	src := Source{Kind: KindGit, Source: "file:///repo", SourceURL: "file:///repo"}
	runner := &recordingRunner{out: Output{Stderr: "fatal: not found"}, err: errors.New("exit status 128")}
	if _, err := RemoteHead(context.Background(), src, runner); err == nil ||
		!strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v, want the command output", err)
	}

	empty := &recordingRunner{out: Output{Stdout: "no refs here\n"}}
	if _, err := RemoteHead(context.Background(), src, empty); err == nil ||
		!strings.Contains(err.Error(), "no ref matching") {
		t.Errorf("err = %v, want a missing-ref error", err)
	}

	local := Source{Kind: KindLocal, Source: "/tmp/x", SourceURL: "/tmp/x"}
	if _, err := RemoteHead(context.Background(), local, runner); err == nil {
		t.Error("RemoteHead accepted a local source")
	}
}
