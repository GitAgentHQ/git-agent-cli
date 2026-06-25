package graph

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"

	"github.com/gitagenthq/git-agent/domain/graph"
)

// chainVersionV1 freezes the canonical-form layout for v1. A future format
// change uses a new version byte and never mutates v1, so historical rows always
// re-verify.
const chainVersionV1 byte = 1

// exitCodeSentinel encodes a nil ExitCode in the fixed-width exit-code slot so
// "no exit code" hashes distinctly from any real exit value.
const exitCodeSentinel int64 = -1 << 31

// SHA256Hasher computes the Event chain's this_hash from prev_hash and the
// record's canonical form. It implements graph.EventHasher.
type SHA256Hasher struct{}

// Compile-time check that SHA256Hasher satisfies graph.EventHasher.
var _ graph.EventHasher = (*SHA256Hasher)(nil)

// NewSHA256Hasher returns a new SHA256Hasher (peer of NewUUIDSessionIDGenerator).
func NewSHA256Hasher() *SHA256Hasher {
	return &SHA256Hasher{}
}

// Hash returns this_hash = SHA256( prev_hash ‖ "\n" ‖ CanonicalForm(e) ) as a
// 64-char hex string. The canonical form is fixed-order and length-prefixed so
// there is no JSON ordering or whitespace ambiguity, and the variable payload is
// covered by hashing its exact stored bytes (never re-serialized).
func (h *SHA256Hasher) Hash(prevHash string, e graph.EventRecord) string {
	var buf bytes.Buffer
	buf.WriteString(prevHash)
	buf.WriteByte('\n')

	buf.WriteByte(chainVersionV1)
	writeUint64(&buf, uint64(e.Seq))
	writeUint64(&buf, uint64(e.RecordedAt))
	writeLenPrefixed(&buf, []byte(e.Source))
	writeLenPrefixed(&buf, []byte(e.InstanceID))
	writeLenPrefixed(&buf, []byte(e.Kind))
	writeLenPrefixed(&buf, []byte(e.ToolName))
	writeInt64(&buf, exitCodeValue(e.ExitCode))

	payloadDigest := sha256.Sum256(e.PayloadRaw)
	buf.Write(payloadDigest[:])

	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:])
}

func exitCodeValue(code *int) int64 {
	if code == nil {
		return exitCodeSentinel
	}
	return int64(*code)
}

func writeUint64(buf *bytes.Buffer, v uint64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	buf.Write(b[:])
}

func writeInt64(buf *bytes.Buffer, v int64) {
	writeUint64(buf, uint64(v))
}

func writeLenPrefixed(buf *bytes.Buffer, b []byte) {
	writeUint64(buf, uint64(len(b)))
	buf.Write(b)
}
