package e2b

import (
	"net/url"
	"testing"

	"github.com/agent-sandbox/agent-sandbox/pkg/api/e2b/api"
)

// The fleet under test: two sandboxes for the same caller, differing only in the
// metadata that identifies their owner - which is the case the unfiltered endpoint
// could not distinguish.
func fixture() []*api.Sandbox {
	return []*api.Sandbox{
		{
			SandboxID: "aaa",
			State:     api.Running,
			Metadata:  map[string]string{"workspace": "scratch/alpha", "name": "alpha"},
		},
		{
			SandboxID: "bbb",
			State:     api.Paused,
			Metadata:  map[string]string{"workspace": "scratch/beta", "name": "beta"},
		},
	}
}

func run(t *testing.T, rawQuery string) ([]*api.Sandbox, string) {
	t.Helper()
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatalf("bad test query %q: %v", rawQuery, err)
	}
	p, perr := parseListParams(q)
	if perr != "" {
		t.Fatalf("unexpected param error for %q: %s", rawQuery, perr)
	}
	var meta map[string]string
	if p.Metadata != nil {
		var ok bool
		meta, ok = parseMetadataFilter(*p.Metadata)
		if !ok {
			t.Fatalf("unexpected metadata parse failure for %q", rawQuery)
		}
	}
	return applyListFilters(fixture(), p, meta)
}

func TestNoFilterReturnsAll(t *testing.T) {
	got, _ := run(t, "")
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
}

// The headline case. Before the fix this returned 2.
func TestMetadataSelectsOne(t *testing.T) {
	got, _ := run(t, "metadata=workspace%3Dscratch%252Falpha")
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].SandboxID != "aaa" {
		t.Fatalf("want aaa, got %s", got[0].SandboxID)
	}
}

// The dangerous case: a filter matching nothing must return nothing, never the
// unfiltered set.
func TestMetadataNoMatchReturnsEmpty(t *testing.T) {
	got, _ := run(t, "metadata=workspace%3DNO_SUCH_WORKSPACE")
	if len(got) != 0 {
		t.Fatalf("want 0, got %d", len(got))
	}
}

func TestMetadataUnknownKeyReturnsEmpty(t *testing.T) {
	got, _ := run(t, "metadata=no_such_key%3Dwhatever")
	if len(got) != 0 {
		t.Fatalf("want 0, got %d", len(got))
	}
}

// Subset semantics: extra keys on the sandbox must not prevent a match, and all
// requested pairs must hold.
func TestMetadataMultiPair(t *testing.T) {
	if got, _ := run(t, "metadata=workspace%3Dscratch%252Falpha%26name%3Dalpha"); len(got) != 1 {
		t.Fatalf("both pairs match: want 1, got %d", len(got))
	}
	if got, _ := run(t, "metadata=workspace%3Dscratch%252Falpha%26name%3Dbeta"); len(got) != 0 {
		t.Fatalf("one pair contradicts: want 0, got %d", len(got))
	}
}

func TestStateFilter(t *testing.T) {
	if got, _ := run(t, "state=paused"); len(got) != 1 || got[0].SandboxID != "bbb" {
		t.Fatalf("state=paused should select bbb, got %v", got)
	}
	if got, _ := run(t, "state=running,paused"); len(got) != 2 {
		t.Fatalf("comma form should select both, got %d", len(got))
	}
	if got, _ := run(t, "state=running&state=paused"); len(got) != 2 {
		t.Fatalf("repeated form should select both, got %d", len(got))
	}
}

func TestInvalidValuesAreRejectedNotIgnored(t *testing.T) {
	if _, perr := parseListParams(url.Values{"state": {"nonsense"}}); perr == "" {
		t.Fatal("invalid state must be an error, not silently ignored")
	}
	if _, perr := parseListParams(url.Values{"limit": {"0"}}); perr == "" {
		t.Fatal("limit=0 must be an error")
	}
	if _, perr := parseListParams(url.Values{"limit": {"abc"}}); perr == "" {
		t.Fatal("non-numeric limit must be an error")
	}
	if _, perr := parseListParams(url.Values{"nextToken": {"not-a-number"}}); perr == "" {
		t.Fatal("garbled nextToken must be an error, not silently reset to page 1")
	}
	if _, perr := parseListParams(url.Values{"nextToken": {"-1"}}); perr == "" {
		t.Fatal("negative nextToken must be an error")
	}
	if _, ok := parseMetadataFilter("this-is-not-key-value"); ok {
		t.Fatal("malformed metadata must be rejected, never treated as no filter")
	}
}

func TestLimitAndPaging(t *testing.T) {
	got, next := run(t, "limit=1")
	if len(got) != 1 {
		t.Fatalf("limit=1: want 1, got %d", len(got))
	}
	if next != "1" {
		t.Fatalf("want nextToken 1, got %q", next)
	}
	page2, next2 := run(t, "limit=1&nextToken="+next)
	if len(page2) != 1 || page2[0].SandboxID == got[0].SandboxID {
		t.Fatalf("second page must be the other sandbox, got %v", page2)
	}
	if next2 != "" {
		t.Fatalf("last page must have no nextToken, got %q", next2)
	}
}

// limit must not be able to hide rows a filter selected.
func TestLimitAppliesAfterFilter(t *testing.T) {
	got, _ := run(t, "metadata=workspace%3Dscratch%252Fbeta&limit=5")
	if len(got) != 1 || got[0].SandboxID != "bbb" {
		t.Fatalf("want just bbb, got %v", got)
	}
}
