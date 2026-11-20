// Command ntn is a fake Notion CLI for the tests: it answers the documented `ntn api` calls from a
// fixture file and never touches the network. It is not part of the shipped binary.
//
// The fixture is a JSON object read from FAKE_NTN_FIXTURE:
//
//	workspace  workspace name reported by /v1/users/me
//	packs      raw body of /v1/ai/plugins
//	plugins    raw bodies of /v1/ai/plugins/<id>, keyed by unescaped id
//	skills     raw bodies of /v1/ai/skills/<id>, keyed by unescaped id
//	errors     per-path failures, keyed by api path: {"exit":1,"stderr":"..."}
//	calls      file the full argument list is appended to, one line per call
//	sleep_ms   delay before answering, to exercise the client timeout
//	stdout     raw body printed for any path, to exercise invalid output
package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// fixture is the script of one test.
type fixture struct {
	Workspace     string                     `json:"workspace"`
	Packs         json.RawMessage            `json:"packs"`
	PacksByCursor map[string]json.RawMessage `json:"packs_by_cursor"`
	Plugins       map[string]json.RawMessage `json:"plugins"`
	Skills        map[string]json.RawMessage `json:"skills"`
	Errors        map[string]failure         `json:"errors"`
	Calls         string                     `json:"calls"`
	SleepMS       int                        `json:"sleep_ms"`
	Stdout        string                     `json:"stdout"`
}

// failure is one scripted error answer.
type failure struct {
	Exit   int    `json:"exit"`
	Stderr string `json:"stderr"`
}

// defaultPacks is the answer when the fixture declares none.
const defaultPacks = `{"results":[],"has_more":false,"next_cursor":null}`

func main() {
	apiPath, version, err := parseArgs(os.Args[1:])
	if err != nil {
		fail(2, err.Error())
	}
	if version != "2026-03-11" {
		fail(2, "unsupported --notion-version "+version)
	}
	fixturePath := os.Getenv("FAKE_NTN_FIXTURE")
	if fixturePath == "" {
		fail(3, "FAKE_NTN_FIXTURE is not set")
	}
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		fail(3, "reading the fixture: "+err.Error())
	}
	var fx fixture
	if err := json.Unmarshal(data, &fx); err != nil {
		fail(3, "parsing the fixture: "+err.Error())
	}
	if fx.Calls != "" {
		logCall(fx.Calls, os.Args[1:])
	}
	if fx.SleepMS > 0 {
		time.Sleep(time.Duration(fx.SleepMS) * time.Millisecond)
	}
	if fx.Stdout != "" {
		fmt.Print(fx.Stdout)
		return
	}
	if answer, ok := fx.Errors[apiPath]; ok {
		code := answer.Exit
		if code == 0 {
			code = 1
		}
		fail(code, answer.Stderr)
	}
	switch {
	case apiPath == "/v1/users/me":
		fmt.Printf(`{"bot":{"workspace_name":%q}}`, fx.Workspace)
	case apiPath == "/v1/ai/plugins":
		if body, ok := packsBody(&fx, cursorOf(os.Args[1:])); ok {
			os.Stdout.Write(body)
		} else {
			fmt.Print(defaultPacks)
		}
	case strings.HasPrefix(apiPath, "/v1/ai/plugins/"):
		writeBody(fx.Plugins, strings.TrimPrefix(apiPath, "/v1/ai/plugins/"))
	case strings.HasPrefix(apiPath, "/v1/ai/skills/"):
		writeBody(fx.Skills, strings.TrimPrefix(apiPath, "/v1/ai/skills/"))
	default:
		fail(1, "unknown path "+apiPath)
	}
}

// parseArgs reads `api <path> [exprs...] --notion-version <version>`.
func parseArgs(args []string) (apiPath, version string, err error) {
	if len(args) == 0 || args[0] != "api" {
		return "", "", fmt.Errorf("unsupported invocation: %s", strings.Join(args, " "))
	}
	for i := 1; i < len(args); i++ {
		switch {
		case args[i] == "--notion-version":
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("--notion-version needs a value")
			}
			version = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--"):
			return "", "", fmt.Errorf("unknown option %s", args[i])
		case apiPath == "":
			apiPath = args[i]
		}
	}
	if apiPath == "" || version == "" {
		return "", "", fmt.Errorf("incomplete invocation: %s", strings.Join(args, " "))
	}
	return apiPath, version, nil
}

// packsBody returns the packs answer of one cursor: the cursor-keyed fixture wins over the plain
// one, so a test can script pagination.
func packsBody(fx *fixture, cursor string) (json.RawMessage, bool) {
	if body, ok := fx.PacksByCursor[cursor]; ok {
		return body, true
	}
	if len(fx.Packs) > 0 {
		return fx.Packs, true
	}
	return nil, false
}

// cursorOf extracts the start_cursor value of a packs call, or "" for the first page.
func cursorOf(args []string) string {
	for _, arg := range args {
		if cursor, ok := strings.CutPrefix(arg, "start_cursor=="); ok {
			return cursor
		}
	}
	return ""
}

// writeBody answers one id-keyed path.
func writeBody(bodies map[string]json.RawMessage, escapedID string) {
	id, err := url.PathUnescape(escapedID)
	if err != nil {
		id = escapedID
	}
	body, ok := bodies[id]
	if !ok {
		fail(1, "no fixture for "+id)
	}
	os.Stdout.Write(body)
}

// logCall appends one argument list to the calls file.
func logCall(path string, args []string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fail(3, "opening the calls log: "+err.Error())
	}
	defer func() { _ = f.Close() }()
	if _, err := fmt.Fprintln(f, strings.Join(args, " ")); err != nil {
		fail(3, "writing the calls log: "+err.Error())
	}
}

// fail writes msg to stderr and exits with code.
func fail(code int, msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(code)
}
