package redact

import (
	"bytes"
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
			name:    "slack token",
			secret:  "xox"+"b-"+strings.Repeat("z",24),
			ruleID:  "slack-token",
			context: `{"command":"curl -H 'Authorization: Bearer %s'"}`,
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
