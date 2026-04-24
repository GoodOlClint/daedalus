package core

import (
	"bytes"
	"regexp"
)

// patterns enumerates the high-entropy credential shapes Mnemosyne redacts
// on every run-record write per architecture.md §14 Secret Sanitization.
// The list is deliberately conservative; Phase 2 tunes it against observed
// content.
var patterns = []*regexp.Regexp{
	// GitHub personal + installation + fine-grained PATs.
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]{20,}`),
	// Slack xoxb/xoxp/xoxa tokens.
	regexp.MustCompile(`xox[abp]-[A-Za-z0-9-]+`),
	// Discord bot tokens (loose shape).
	regexp.MustCompile(`[A-Za-z0-9_-]{24}\.[A-Za-z0-9_-]{6}\.[A-Za-z0-9_-]{27,}`),
	// AWS access key id.
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	// Private key header.
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	// Bearer Authorization values.
	regexp.MustCompile(`Bearer\s+[A-Za-z0-9\-_.=]{16,}`),
	// Anthropic API keys.
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{20,}`),
	// Generic long base64-ish blobs (>= 40 chars, no spaces).
	regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`),
}

// redaction is the placeholder that replaces every matched secret.
const redaction = "<redacted>"

// Sanitize returns a copy of body with known secret shapes and the
// literal values in knownValues replaced by <redacted>. knownValues is
// typically the set of credential plaintexts that flowed through the pod
// (GITHUB_TOKEN, MCP_AUTH_TOKEN, etc.) — the caller derives the set from
// the envelope's InjectedCredentials and the Minos-minted bearer.
//
// The result is always valid UTF-8 bytes; JSON structure is preserved
// because the regex patterns only match string contents, not JSON syntax.
func Sanitize(body []byte, knownValues [][]byte) []byte {
	if len(body) == 0 {
		return body
	}
	out := make([]byte, len(body))
	copy(out, body)
	// Replace exact known values first — these have zero false positives
	// and are the highest-signal match available.
	for _, v := range knownValues {
		if len(v) < 4 {
			continue
		}
		out = bytes.ReplaceAll(out, v, []byte(redaction))
	}
	// Then pattern-based redaction.
	for _, p := range patterns {
		out = p.ReplaceAll(out, []byte(redaction))
	}
	return out
}
