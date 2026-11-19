package lockfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf16"
)

// member is one key with its already-encoded JSON value.
type member struct {
	key   string
	value string
}

// encode renders the document the way JSON.stringify(doc, null, 2) does, plus a trailing newline.
func (f *File) encode() ([]byte, error) {
	skills := make([]member, 0, len(f.Skills))
	for _, name := range sortedKeys(f.Skills) {
		skills = append(skills, member{name, object(f.Skills[name].members())})
	}
	top := []member{{"version", fmt.Sprint(Version)}, {"skills", object(skills)}}
	for _, k := range sortedKeys(f.extra) {
		top = append(top, member{k, string(f.extra[k])})
	}
	var out bytes.Buffer
	if err := json.Indent(&out, []byte(object(top)), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// members lists the entry fields in the order upstream writes them. Optional fields are omitted
// when empty; computedHash is always written, because "" is a meaningful value.
func (e Entry) members() []member {
	var ms []member
	add := func(key, value string, always bool) {
		if value != "" || always {
			ms = append(ms, member{key, quote(value)})
		}
	}
	add("source", e.Source, true)
	add("sourceUrl", e.SourceURL, false)
	add("ref", e.Ref, false)
	add("commit", e.Commit, false)
	add("sourceType", e.SourceType, true)
	add("skillPath", e.SkillPath, false)
	add("computedHash", e.ComputedHash, true)
	add("skillName", e.SkillName, false)
	add("installMode", e.InstallMode, false)
	for _, k := range sortedKeys(e.extra) {
		ms = append(ms, member{k, string(e.extra[k])})
	}
	return ms
}

// object joins members into one compact JSON object.
func object(ms []member) string {
	var b strings.Builder
	b.WriteByte('{')
	for i, m := range ms {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(quote(m.key))
		b.WriteByte(':')
		b.WriteString(m.value)
	}
	b.WriteByte('}')
	return b.String()
}

// quote encodes a string as a JSON string, escaping the same characters as JSON.stringify: no
// HTML escaping, and control characters as \b \f \n \r \t or \u00XX.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// sortedKeys returns the keys of m ordered by UTF-16 code units, the order JavaScript sorts in.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return lessUTF16(keys[i], keys[j]) })
	return keys
}

// lessUTF16 compares strings by UTF-16 code units.
func lessUTF16(a, b string) bool {
	return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b))) < 0
}
