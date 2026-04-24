package config

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadmeEnvListMatchesStruct is the operator-doc drift guard.
//
// The README's "Configuration" section MUST mention every env var
// the Config struct parses, and MUST NOT mention env vars the
// struct does not parse. A mismatch causes operators to either
// configure variables the gateway ignores, or to miss variables
// they should have set — both have caused real incidents.
//
// The test reads every `env:"..."` tag off the Config struct via
// reflection, scans the README for `|` `VAR_NAME` `|` table cells
// inside the Configuration block, and asserts the two sets are
// equal.
func TestReadmeEnvListMatchesStruct(t *testing.T) {
	structVars := envTagsFromConfig(t)
	readmeVars := envVarsFromReadme(t)

	missingFromReadme := setDifference(structVars, readmeVars)
	extraInReadme := setDifference(readmeVars, structVars)

	assert.Empty(t, missingFromReadme,
		"README is missing env vars that the Config struct parses — operators will not know to set them: %v",
		missingFromReadme)
	assert.Empty(t, extraInReadme,
		"README mentions env vars the Config struct does not parse — operators will configure variables the gateway ignores: %v",
		extraInReadme)
}

// envTagsFromConfig returns the set of every `env:"..."` tag value
// declared on the Config struct, ignoring the special "-" sentinel
// (used for derived fields not parsed from env).
func envTagsFromConfig(t *testing.T) map[string]struct{} {
	t.Helper()
	out := map[string]struct{}{}
	cfg := Config{}
	tp := reflect.TypeOf(cfg)
	for i := 0; i < tp.NumField(); i++ {
		raw := tp.Field(i).Tag.Get("env")
		if raw == "" || raw == "-" {
			continue
		}
		// Tag forms: `NAME`, `NAME,required`. Take the head before
		// the first comma — that's the actual variable name.
		name := raw
		if comma := strings.IndexByte(raw, ','); comma >= 0 {
			name = raw[:comma]
		}
		out[name] = struct{}{}
	}

	return out
}

// envVarsFromReadme parses the README and returns every env var
// mentioned inside a Configuration table cell. The match looks for
// markdown table rows whose first column is a backticked uppercase
// token (e.g. `| `LOG_LEVEL` | ...`). Anything outside the
// "## Configuration" through next "## " section is ignored so the
// "Run" code block and the headline numbers tables don't pollute
// the comparison.
func envVarsFromReadme(t *testing.T) map[string]struct{} {
	t.Helper()
	path := readmePath(t)
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	body := string(data)
	configBlock := extractSection(body, "## Configuration")
	require.NotEmpty(t, configBlock, "README must have a `## Configuration` section")

	// Match the variable token in the first table column. The leading
	// `|` and surrounding whitespace are required so we only catch
	// table rows; backticks frame the var name; the closing `|`
	// after the cell ends the cell. The token itself is uppercase
	// letters, digits, and underscores (the env-var alphabet).
	re := regexp.MustCompile(`(?m)^\|\s*` + "`" + `([A-Z][A-Z0-9_]*)` + "`" + `\s*\|`)
	out := map[string]struct{}{}
	for _, m := range re.FindAllStringSubmatch(configBlock, -1) {
		out[m[1]] = struct{}{}
	}

	return out
}

// extractSection returns the slice of body starting at the first
// occurrence of header and ending at the next top-level "## "
// header (or end of file). Used to scope the regex match to the
// Configuration section so unrelated tables (Performance, Metrics)
// don't poison the var set.
func extractSection(body, header string) string {
	start := strings.Index(body, header)
	if start < 0 {
		return ""
	}
	rest := body[start+len(header):]
	if next := strings.Index(rest, "\n## "); next >= 0 {
		return rest[:next]
	}

	return rest
}

// readmePath resolves the README from the test working dir, which
// `go test` sets to the package dir (internal/config). Two levels up
// reaches the gateway-server root.
func readmePath(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	return filepath.Join(wd, "..", "..", "README.md")
}

// setDifference returns members of a not present in b, sorted for
// stable assertion failure output.
func setDifference(a, b map[string]struct{}) []string {
	out := make([]string, 0)
	for k := range a {
		if _, ok := b[k]; !ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)

	return out
}
