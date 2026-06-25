// Package redact removes secrets from agent hook payloads before they are
// stored in the Event Log. It is a pure, hot-path-safe infrastructure concern:
// the path denylist and the token-regex set are compiled once and applied
// without shelling out, DB, or network access.
package redact

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
)

// maxPayloadBytes caps the stored payload. Anything larger is truncated and
// flagged; the chain hash then covers exactly the stored (truncated) bytes.
const maxPayloadBytes = 512 * 1024

// maxFieldBytes caps a single JSON string value. Oversized values are replaced
// in place by a Truncation Marker so the stored payload stays valid JSON (a tool
// Event whose huge output would otherwise be sliced into unparseable bytes still
// yields its file/command structure on Replay).
const maxFieldBytes = 64 * 1024

// Result is the outcome of redacting a payload.
type Result struct {
	Bytes     []byte // post-redaction payload bytes (the hashed unit)
	OrigSize  int64  // size before any truncation
	Truncated bool
}

// Redactor removes secrets from a payload before storage.
type Redactor interface {
	// Redact applies the path denylist, the compiled token-regex set, and the
	// size cap, returning the bytes to store plus size/truncation metadata. It is
	// pure: no DB, git, or network access.
	Redact(payload []byte) Result
}

// tokenRule is one compiled inline-secret detector.
type tokenRule struct {
	id    string
	gate  string // cheap substring pre-filter; empty means always scan
	regex *regexp.Regexp
	repl  string // replacement template ($1 groups); empty => typed placeholder
}

// tokenRules is the compiled-once set of inline-secret detectors (Layer B). It
// covers the common credential shapes an agent hook payload can carry: cloud
// keys, VCS tokens, generic API keys, JWT/bearer headers, and URL userinfo.
var tokenRules = []tokenRule{
	{id: "aws-access-token", gate: "", regex: regexp.MustCompile(`(AKIA|ASIA)[A-Z0-9]{16}`)},
	{id: "github-token", gate: "gh", regex: regexp.MustCompile(`gh[posru]_[0-9A-Za-z]{36,255}`)},
	{id: "github-pat", gate: "github_pat_", regex: regexp.MustCompile(`github_pat_[0-9A-Za-z_]{22,255}`)},
	{id: "slack-token", gate: "xox", regex: regexp.MustCompile(`xox[baprs]-[0-9A-Za-z-]{10,}`)},
	{id: "openai-key", gate: "sk-", regex: regexp.MustCompile(`sk-(?:proj-|ant-)?[0-9A-Za-z_-]{20,}`)},
	{id: "stripe-key", gate: "sk_", regex: regexp.MustCompile(`sk_(?:live|test)_[0-9A-Za-z]{16,}`)},
	{id: "google-api-key", gate: "AIza", regex: regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`)},
	{id: "jwt", gate: "eyJ", regex: regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)},
	{id: "bearer-token", gate: "earer", regex: regexp.MustCompile(`(?i)(bearer\s+)[0-9A-Za-z._\-+/=]{16,}`), repl: "${1}[REDACTED:bearer-token]"},
	{id: "url-credentials", gate: "://", regex: regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://[^:/\s@\[\]]+:)[^@/\s\[\]]+(@)`), repl: "${1}[REDACTED:url-credentials]${2}"},
	{id: "private-key", gate: "PRIVATE KEY", regex: regexp.MustCompile(`-----BEGIN (?:[A-Z ]+ )?PRIVATE KEY-----[A-Za-z0-9+/=\s\\]*(?:-----END (?:[A-Z ]+ )?PRIVATE KEY-----)?`)},
}

// sensitivePatterns is the path denylist (Layer A). A field whose value looks
// like a path to one of these has its sibling content replaced by a Redaction
// Digest rather than being stored verbatim.
var sensitivePatterns = []string{
	".env", ".env.*", "*.pem", "*.key", "*.p12", "*.pfx",
	"id_rsa", "id_ed25519", "*.keystore", "credentials",
	".npmrc", ".netrc", "*.tfvars",
}

// jsonRedactor is the default Redactor: it understands the structured
// tool_input/tool_response JSON shape for the path denylist and falls back to a
// raw token scan over the whole payload.
type jsonRedactor struct{}

// NewRedactor builds a Redactor with the path denylist, the once-compiled token
// regex set, and the size cap.
func NewRedactor() Redactor {
	return jsonRedactor{}
}

// Redact runs Layer A (path denylist on structured fields), Layer B (inline
// token regex over the result), then the size cap.
func (jsonRedactor) Redact(payload []byte) Result {
	orig := int64(len(payload))

	redacted := redactSensitivePaths(payload)
	redacted = redactTokens(redacted)

	truncated := false
	// Prefer field-level truncation so the stored payload stays valid JSON; fall
	// back to a raw slice only when the payload is not parseable JSON.
	if int64(len(redacted)) > maxPayloadBytes {
		if capped, ok := truncateJSONFields(redacted); ok && int64(len(capped)) <= maxPayloadBytes {
			redacted = capped
			truncated = true
		}
	}
	if int64(len(redacted)) > maxPayloadBytes {
		redacted = redacted[:maxPayloadBytes]
		truncated = true
	}

	return Result{Bytes: redacted, OrigSize: orig, Truncated: truncated}
}

// redactSensitivePaths inspects the payload as JSON and, wherever an object
// carries a denylisted file_path, replaces that object's
// content/old_string/new_string values with a Redaction Digest. It recurses into
// every nested object so a secret file's body is caught regardless of nesting
// depth (e.g. a tool_response.file.content shape), and returns the input
// unchanged when it is not a JSON object (the token scan still applies).
func redactSensitivePaths(payload []byte) []byte {
	newRaw, changed := redactRawObject(payload)
	if !changed {
		return payload
	}
	return newRaw
}

// redactRawObject parses raw as a JSON object, redacts its content fields when
// its own file_path is denylisted, recurses into nested object values, and
// re-encodes only when something changed (untouched subtrees keep their verbatim
// bytes).
func redactRawObject(raw json.RawMessage) (json.RawMessage, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw, false
	}

	changed := false
	if newObj, did := redactSensitiveFields(obj); did {
		obj = newObj
		changed = true
	}
	for k, v := range obj {
		if newV, did := redactRawObject(v); did {
			obj[k] = newV
			changed = true
		}
	}

	if !changed {
		return raw, false
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return raw, false
	}
	return out, true
}

// contentFields are the value fields whose contents are replaced by a Redaction
// Digest when the object's file_path is denylisted.
var contentFields = []string{"content", "old_string", "new_string"}

// redactSensitiveFields replaces content fields with a Redaction Digest when the
// object's file_path value matches the denylist.
func redactSensitiveFields(obj map[string]json.RawMessage) (map[string]json.RawMessage, bool) {
	rawPath, ok := obj["file_path"]
	if !ok {
		return obj, false
	}
	var path string
	if err := json.Unmarshal(rawPath, &path); err != nil {
		return obj, false
	}
	if !isSensitivePath(path) {
		return obj, false
	}

	did := false
	for _, field := range contentFields {
		rawVal, ok := obj[field]
		if !ok {
			continue
		}
		var val string
		if err := json.Unmarshal(rawVal, &val); err != nil {
			continue
		}
		digest, err := json.Marshal(redactionDigest([]byte(val)))
		if err != nil {
			continue
		}
		obj[field] = digest
		did = true
	}
	return obj, did
}

// isSensitivePath reports whether path matches the denylist by base name.
func isSensitivePath(path string) bool {
	base := filepath.Base(path)
	for _, pat := range sensitivePatterns {
		if ok, _ := filepath.Match(pat, base); ok {
			return true
		}
	}
	return false
}

// redactTokens replaces every inline-secret match with its Typed Placeholder (or
// the rule's group-preserving replacement template when one is set).
func redactTokens(payload []byte) []byte {
	out := payload
	for _, rule := range tokenRules {
		if rule.gate != "" && !bytes.Contains(out, []byte(rule.gate)) {
			continue
		}
		if rule.repl != "" {
			out = rule.regex.ReplaceAll(out, []byte(rule.repl))
			continue
		}
		out = rule.regex.ReplaceAllLiteral(out, []byte(typedPlaceholder(rule.id)))
	}
	return out
}

// truncateJSONFields replaces every JSON string value longer than maxFieldBytes
// with a Truncation Marker, keeping the payload valid JSON. It returns ok=false
// when payload is not parseable JSON or nothing was oversized.
func truncateJSONFields(payload []byte) ([]byte, bool) {
	var root any
	if err := json.Unmarshal(payload, &root); err != nil {
		return nil, false
	}
	truncated := false
	root = truncateJSONValue(root, &truncated)
	if !truncated {
		return nil, false
	}
	out, err := json.Marshal(root)
	if err != nil {
		return nil, false
	}
	return out, true
}

// truncateJSONValue recursively caps oversized string values in a decoded JSON
// tree, recording whether any value was replaced.
func truncateJSONValue(v any, truncated *bool) any {
	switch node := v.(type) {
	case string:
		if len(node) > maxFieldBytes {
			*truncated = true
			return fmt.Sprintf("[REDACTED-TRUNCATED:original-len=%d]", len(node))
		}
		return node
	case map[string]any:
		for k, child := range node {
			node[k] = truncateJSONValue(child, truncated)
		}
		return node
	case []any:
		for i, child := range node {
			node[i] = truncateJSONValue(child, truncated)
		}
		return node
	default:
		return v
	}
}

// redactionDigest formats a Redaction Digest (sha256 + length) stored in place
// of denylisted-path content.
func redactionDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return fmt.Sprintf("[REDACTED-DIGEST:sha256:%x:len=%d]", sum, len(content))
}

// typedPlaceholder formats "[REDACTED:<rule-id>]" for a token match, preserving
// the kind for forensics without leaking the value.
func typedPlaceholder(ruleID string) string {
	return "[REDACTED:" + ruleID + "]"
}
