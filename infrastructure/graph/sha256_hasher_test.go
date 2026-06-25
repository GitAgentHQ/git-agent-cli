package graph

import (
	"testing"

	domaingraph "github.com/gitagenthq/git-agent/domain/graph"
)

func sampleEvent() domaingraph.EventRecord {
	exit := 0
	return domaingraph.EventRecord{
		Seq:        1,
		EventID:    "evt-1",
		RecordedAt: 1_700_000_000,
		Source:     domaingraph.EventSourceClaudeCode,
		InstanceID: "agent-1",
		Kind:       domaingraph.EventKindTool,
		ToolName:   "Edit",
		ExitCode:   &exit,
		PayloadRaw: []byte(`{"tool_input":{"file_path":"a.go","old_string":"x","new_string":"y"}}`),
	}
}

func TestSHA256Hasher_Deterministic(t *testing.T) {
	h := NewSHA256Hasher()
	e := sampleEvent()
	prev := domaingraph.GenesisHash

	first := h.Hash(prev, e)
	second := h.Hash(prev, e)

	if first != second {
		t.Fatalf("hash not deterministic: %q != %q", first, second)
	}
	if len(first) != 64 {
		t.Fatalf("expected 64-char hex hash, got %d chars: %q", len(first), first)
	}
}

func TestSHA256Hasher_FieldChangeChangesHash(t *testing.T) {
	h := NewSHA256Hasher()
	prev := domaingraph.GenesisHash
	base := h.Hash(prev, sampleEvent())

	otherExit := 1
	mutations := map[string]func(e *domaingraph.EventRecord){
		"seq":         func(e *domaingraph.EventRecord) { e.Seq = 2 },
		"recorded_at": func(e *domaingraph.EventRecord) { e.RecordedAt = 1_700_000_001 },
		"source":      func(e *domaingraph.EventRecord) { e.Source = domaingraph.EventSourceCursor },
		"instance_id": func(e *domaingraph.EventRecord) { e.InstanceID = "agent-2" },
		"kind":        func(e *domaingraph.EventRecord) { e.Kind = domaingraph.EventKindOutcome },
		"tool_name":   func(e *domaingraph.EventRecord) { e.ToolName = "Write" },
		"exit_code":   func(e *domaingraph.EventRecord) { e.ExitCode = &otherExit },
		"payload_raw": func(e *domaingraph.EventRecord) { e.PayloadRaw = []byte("different") },
	}

	for name, mutate := range mutations {
		e := sampleEvent()
		mutate(&e)
		if got := h.Hash(prev, e); got == base {
			t.Errorf("mutating %s did not change the hash", name)
		}
	}

	// prev_hash is part of the chain link, so a different prev must also change
	// the output for the same record.
	if got := h.Hash("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", sampleEvent()); got == base {
		t.Error("changing prev_hash did not change the hash")
	}

	// ThisHash/PrevHash stored on the record are excluded from CanonicalForm, so
	// setting them must not affect Hash's output for a fixed prevHash argument.
	e := sampleEvent()
	e.PrevHash = "deadbeef"
	e.ThisHash = "cafef00d"
	if got := h.Hash(prev, e); got != base {
		t.Errorf("stored PrevHash/ThisHash must not affect Hash output: got %q want %q", got, base)
	}
}
