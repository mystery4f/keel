package markdown

import (
	"strings"
	"testing"
)

const rangesDoc = "---\ntitle: demo\n---\n\n# Heading\n\npara one\n\n- a\n- b\n\n  nested\n\n```go\nfunc x() {}\n\n// blank inside the fence\n```\n\n| a | b |\n| - | - |\n| 1 | 2 |\n\n> quote\n\n---\n\ntail\n"

func rangeText(src string, r SourceRange) string { return src[r.Start:r.End] }

func findRange(t *testing.T, src, marker string) SourceRange {
	t.Helper()
	var found []SourceRange
	for _, r := range New(src).SourceRanges() {
		if strings.Contains(rangeText(src, r), marker) {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%q appears in %d ranges: %+v", marker, len(found), found)
	}
	return found[0]
}

func TestSourceRangesNameAndCoverBlocks(t *testing.T) {
	src := rangesDoc
	for _, tc := range []struct {
		marker, kind string
	}{
		{"title: demo", "front-matter"},
		{"# Heading", "heading"},
		{"para one", "paragraph"},
		{"- b", "list"},
		{"func x()", "code"},
		{"| 1 | 2 |", "table"},
		{"> quote", "quote"},
		{"tail", "paragraph"},
	} {
		if r := findRange(t, src, tc.marker); r.Kind != tc.kind {
			t.Errorf("%q: kind = %s, want %s", tc.marker, r.Kind, tc.kind)
		}
	}
	// Thematic break: goldmark keeps no lines for it, so the span comes from
	// the neighbouring blocks.
	// Thematic break: goldmark keeps no lines for it, so the span comes from
	// the neighbouring blocks.
	var rules []SourceRange
	for _, r := range New(src).SourceRanges() {
		if r.Kind == "rule" {
			rules = append(rules, r)
		}
	}
	if len(rules) != 1 || !strings.Contains(rangeText(src, rules[0]), "---") {
		t.Errorf("rule ranges = %+v", rules)
	}
}

// Every byte that is not blank must belong to a range: the ranges together with
// the blank lines split removes make up the whole source.
func TestSourceRangesCoverEveryNonBlankByte(t *testing.T) {
	src := rangesDoc
	covered := make([]bool, len(src))
	for _, r := range New(src).SourceRanges() {
		if r.Start < 0 || r.End > len(src) || r.Start >= r.End {
			t.Fatalf("invalid range %+v in %d bytes", r, len(src))
		}
		for i := r.Start; i < r.End; i++ {
			covered[i] = true
		}
	}
	for i := 0; i < len(src); i++ {
		if covered[i] || src[i] == '\n' || src[i] == ' ' {
			continue
		}
		t.Fatalf("byte %d (%q) in no range:\n%s", i, src[i], rangesDoc)
	}
}

// The ranges tile the source: each starts where the one above ended, so an
// editor can show every byte exactly once.
func TestSourceRangesTileTheSource(t *testing.T) {
	ranges := New(rangesDoc).SourceRanges()
	if len(ranges) == 0 || ranges[0].Start != 0 {
		t.Fatalf("ranges %+v", ranges)
	}
	for i := 1; i < len(ranges); i++ {
		if ranges[i].Start != ranges[i-1].End {
			t.Fatalf("range %d starts at %d, but %d ends at %d", i, ranges[i].Start, i-1, ranges[i-1].End)
		}
	}
}

func TestSourceRangesAppend(t *testing.T) {
	d := New("# Title\n\n")
	head := d.SourceRanges()
	if len(head) != 1 || head[0].Kind != "heading" {
		t.Fatalf("ranges %+v", head)
	}
	d.Append("first paragraph\n\n")
	d.Append("second paragraph\n\n")
	got := d.SourceRanges()
	if len(got) != 3 {
		t.Fatalf("ranges %+v", got)
	}
	if got[0].Kind != head[0].Kind || got[0].Start != head[0].Start {
		t.Errorf("heading span moved: %+v then %+v", head[0], got[0])
	}
	if text := rangeText(d.Source(), got[2]); text != "second paragraph" {
		t.Errorf("last range = %q", text)
	}
}

// Text inserted before a chunk moves it without reparsing: the spans shift.
func TestSourceRangesShiftWithPrefix(t *testing.T) {
	d := New("# Title\n\ntail\n")
	before := d.SourceRanges()
	d.SetSource("intro paragraph\n\n" + d.Source())
	after := d.SourceRanges()
	delta := len("intro paragraph\n\n")
	if len(after) != len(before)+1 {
		t.Fatalf("ranges %+v then %+v", before, after)
	}
	for i := range before {
		want := SourceRange{Kind: before[i].Kind, Start: before[i].Start + delta, End: before[i].End + delta}
		if after[i+1] != want {
			t.Errorf("range %d = %+v, want %+v", i, after[i+1], want)
		}
	}
}

func TestSourceRangesStreamingStayInSource(t *testing.T) {
	d := New("")
	d.SetStreaming(true)
	d.Append("# Title\n\npara with **unclosed bold\n\n```go\nfunc x() {")
	src := d.Source()
	for _, r := range d.SourceRanges() {
		if r.End > len(src) {
			t.Fatalf("range %+v beyond %d bytes", r, len(src))
		}
	}
	d.SetStreaming(false)
	if got := rangeText(d.Source(), d.SourceRanges()[2]); !strings.Contains(got, "```go") {
		t.Errorf("code range = %q", got)
	}
}

// References and footnotes parse the whole document in one context; the ranges
// must still be absolute.
func TestSourceRangesContextual(t *testing.T) {
	src := "see [link][a] and a note[^1]\n\n[a]: https://example.com\n\n[^1]: the note\n"
	ranges := New(src).SourceRanges()
	if len(ranges) == 0 {
		t.Fatal("no ranges")
	}
	for _, r := range ranges {
		if r.Start < 0 || r.End > len(src) || r.Start >= r.End {
			t.Fatalf("invalid range %+v", r)
		}
	}
	if text := rangeText(src, ranges[0]); !strings.HasPrefix(text, "see [link][a] and a note[^1]") {
		t.Errorf("first range = %q", text)
	}
}
