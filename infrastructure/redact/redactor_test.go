package redact

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactor_TokenPlaceholders(t *testing.T) {
	r := NewRedactor()

	tests := []struct {
		name    string
		secret  string
		ruleID  string
		context string
	}{
		{
			name:    "aws access key",
			secret:  "AKIAIOSFODNN7EXAMPLE",
			ruleID:  "aws-access-token",
			context: `{"command":"aws s3 ls --key %s"}`,
		},
		{
			name:    "github personal token",
			secret:  "ghp_" + strings.Repeat("a", 36),
			ruleID:  "github-token",
			context: `{"command":"git remote set-url origin https://%s@github.com/x/y"}`,
		},
		{
			name:    "github server token",
			secret:  "ghs_" + strings.Repeat("b", 36),
			ruleID:  "github-token",
			context: `{"command":"echo %s"}`,
		},
		{
			name:    "slack token",
			secret:  "xox"+"b-"+strings.Repeat("z",24),
			ruleID:  "slack-token",
			context: `{"command":"curl -H 'Authorization: Bearer %s'"}`,
		},
		{
			name:    "openai api key",
			secret:  "sk-proj-" + strings.Repeat("A", 40),
			ruleID:  "openai-key",
			context: `{"tool_input":{"command":"export OPENAI_API_KEY=%s"}}`,
		},
		{
			name:    "anthropic api key",
			secret:  "sk-ant-" + strings.Repeat("C", 40),
			ruleID:  "openai-key",
			context: `{"command":"ANTHROPIC_API_KEY=%s claude"}`,
		},
		{
			name:    "stripe secret key",
			secret:  "sk_live_" + strings.Repeat("d", 24),
			ruleID:  "stripe-key",
			context: `{"command":"curl -u %s: https://api.stripe.com"}`,
		},
		{
			name:    "google api key",
			secret:  "AIza" + strings.Repeat("e", 35),
			ruleID:  "google-api-key",
			context: `{"command":"curl 'https://maps.googleapis.com/?key=%s'"}`,
		},
		{
			name:    "jwt",
			secret:  "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV",
			ruleID:  "jwt",
			context: `{"command":"curl -H 'Authorization: Bearer %s'"}`,
		},
		{
			name:    "bearer token",
			secret:  strings.Repeat("Z", 40),
			ruleID:  "bearer-token",
			context: `{"command":"curl -H 'Authorization: Bearer %s'"}`,
		},
		{
			name:    "url credentials",
			secret:  "hunter2secret",
			ruleID:  "url-credentials",
			context: `{"command":"psql postgres://admin:%s@db.internal/prod"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := []byte(strings.Replace(tt.context, "%s", tt.secret, 1))
			res := r.Redact(payload)

			if bytes.Contains(res.Bytes, []byte(tt.secret)) {
				t.Fatalf("raw secret %q survived redaction: %q", tt.secret, res.Bytes)
			}
			placeholder := "[REDACTED:" + tt.ruleID + "]"
			if !bytes.Contains(res.Bytes, []byte(placeholder)) {
				t.Errorf("expected placeholder %q in output, got %q", placeholder, res.Bytes)
			}
		})
	}
}

func TestRedactor_SensitivePathDigest(t *testing.T) {
	r := NewRedactor()

	secretContent := "DB_PASSWORD=hunter2\nAPI_KEY=sk-live-abcdef"
	payload := []byte(`{"file_path":".env","content":` + quote(secretContent) + `}`)

	res := r.Redact(payload)

	if bytes.Contains(res.Bytes, []byte("hunter2")) {
		t.Fatalf("sensitive .env content survived redaction: %q", res.Bytes)
	}
	if bytes.Contains(res.Bytes, []byte("API_KEY=sk-live-abcdef")) {
		t.Fatalf("sensitive .env line survived redaction: %q", res.Bytes)
	}
	if !bytes.Contains(res.Bytes, []byte("[REDACTED-DIGEST:sha256:")) {
		t.Errorf("expected a Redaction Digest in output, got %q", res.Bytes)
	}
}

func TestRedactor_SensitivePathGlobs(t *testing.T) {
	r := NewRedactor()

	cases := []string{
		"server.pem",
		"deploy.key",
		"id_rsa",
		"secrets/credentials",
		".npmrc",
		"prod.tfvars",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			marker := "SECRETMARKER_" + strings.ReplaceAll(path, ".", "_")
			payload := []byte(`{"file_path":` + quote(path) + `,"content":` + quote(marker) + `}`)
			res := r.Redact(payload)
			if bytes.Contains(res.Bytes, []byte(marker)) {
				t.Errorf("content of sensitive path %q survived redaction: %q", path, res.Bytes)
			}
		})
	}
}

func TestRedactor_NonSensitivePathPassesThrough(t *testing.T) {
	r := NewRedactor()

	payload := []byte(`{"file_path":"src/main.go","content":"package main"}`)
	res := r.Redact(payload)

	if !bytes.Contains(res.Bytes, []byte("package main")) {
		t.Errorf("non-sensitive content was redacted: %q", res.Bytes)
	}
	if res.Truncated {
		t.Error("small non-sensitive payload should not be truncated")
	}
}

func TestRedactor_OversizedTruncation(t *testing.T) {
	r := NewRedactor()

	big := append([]byte(`{"content":"`), bytes.Repeat([]byte("x"), maxPayloadBytes+4096)...)
	big = append(big, []byte(`"}`)...)
	orig := int64(len(big))

	res := r.Redact(big)

	if !res.Truncated {
		t.Error("expected truncation flag set for oversized payload")
	}
	if res.OrigSize != orig {
		t.Errorf("OrigSize = %d, want %d (original size before truncation)", res.OrigSize, orig)
	}
	if int64(len(res.Bytes)) > maxPayloadBytes {
		t.Errorf("stored bytes = %d, exceed cap %d", len(res.Bytes), maxPayloadBytes)
	}
	if int64(len(res.Bytes)) >= orig {
		t.Errorf("stored bytes = %d not smaller than original %d", len(res.Bytes), orig)
	}
}

func TestRedactor_NestedSensitivePathDigest(t *testing.T) {
	r := NewRedactor()

	// file_path + content nested under tool_response.file, not flat siblings of
	// the root — the denylist must recurse to reach it.
	payload := []byte(`{"tool_response":{"file":{"file_path":".env","content":"SECRET=hunter2"}}}`)
	res := r.Redact(payload)

	if bytes.Contains(res.Bytes, []byte("hunter2")) {
		t.Errorf("nested sensitive content survived redaction: %q", res.Bytes)
	}
	if !bytes.Contains(res.Bytes, []byte("[REDACTED-DIGEST:sha256:")) {
		t.Errorf("expected a Redaction Digest in output, got %q", res.Bytes)
	}
}

func TestRedactor_PrivateKeyWithoutEndMarker(t *testing.T) {
	r := NewRedactor()

	// A key pasted/streamed without its matching END footer must still be redacted.
	body := "MIIEpAIBAAKCAQEA" + strings.Repeat("a", 200)
	payload := []byte(`{"content":"-----BEGIN RSA PRIVATE KEY-----\n` + body + `"}`)
	res := r.Redact(payload)

	if bytes.Contains(res.Bytes, []byte(body)) {
		t.Errorf("private key body without END marker survived redaction: %q", res.Bytes)
	}
}

func TestRedactor_TruncationKeepsValidJSON(t *testing.T) {
	r := NewRedactor()

	bigOutput := strings.Repeat("x", maxPayloadBytes+4096)
	payload := []byte(`{"tool_input":{"command":"go test ./..."},"tool_response":{"stdout":` + quote(bigOutput) + `}}`)
	res := r.Redact(payload)

	if !res.Truncated {
		t.Fatal("expected truncation flag for oversized payload")
	}
	if int64(len(res.Bytes)) > maxPayloadBytes {
		t.Errorf("stored bytes = %d exceed cap %d", len(res.Bytes), maxPayloadBytes)
	}
	var parsed struct {
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	if err := json.Unmarshal(res.Bytes, &parsed); err != nil {
		t.Fatalf("truncated payload is not valid JSON: %v\n%q", err, res.Bytes)
	}
	if parsed.ToolInput.Command != "go test ./..." {
		t.Errorf("command lost after field truncation: %q", parsed.ToolInput.Command)
	}
}

// quote returns the JSON string literal for s.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range s {
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
