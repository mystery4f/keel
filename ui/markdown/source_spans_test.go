package markdown

import (
	"strings"
	"testing"

	"github.com/dyike/keel/ui/el"
	"github.com/dyike/keel/ui/internal/uitest"
)

// spanText returns the Markdown a span covers.
func spanText(src string, s SourceSpan) string { return src[s.Start:s.End] }

func spansOf(t *testing.T, src, blockMarker string) (string, []SourceSpan) {
	t.Helper()
	d := New(src)
	r := findRange(t, src, blockMarker)
	var index int
	for i, rr := range d.SourceRanges() {
		if rr == r {
			index = i
		}
	}
	return src[r.Start:r.End], d.SourceSpans(index)
}

func TestSourceSpansCoverMarkers(t *testing.T) {
	src := "plain **bold** *it* `code` [link](https://x) $a+b$ tail\n"
	_, spans := spansOf(t, src, "plain")
	want := map[string]string{
		"text":   "plain ",
		"strong": "**bold**",
		"emph":   "*it*",
		"code":   "`code`",
		"link":   "[link](https://x)",
		"math":   "$a+b$",
	}
	got := map[string]string{}
	for _, s := range spans {
		if _, seen := got[s.Kind]; seen && s.Kind == "text" {
			continue // plain text arrives in pieces
		}
		got[s.Kind] = spanText(src, s)
	}
	for kind, text := range want {
		if got[kind] != text {
			t.Errorf("%s covers %q, want %q (all: %+v)", kind, got[kind], text, spans)
		}
	}
	for _, s := range spans {
		if s.Synthetic {
			t.Errorf("%s reported synthetic: %+v", s.Kind, s)
		}
	}
}

func TestSourceSpansNest(t *testing.T) {
	src := "*a **b** c*\n"
	_, spans := spansOf(t, src, "*a")
	// The emphasis owns its markers: the first emph piece starts at the
	// opening marker and the last one ends at the closing marker.
	first, last, strong := -1, -1, ""
	for _, s := range spans {
		switch s.Kind {
		case "emph":
			if !s.Synthetic {
				if first < 0 || s.Start < first {
					first = s.Start
				}
				if s.End > last {
					last = s.End
				}
			}
		case "strong":
			strong = spanText(src, s)
		}
	}
	if first != 0 || last != len(src)-1 {
		t.Errorf("emph covers [%d,%d), want the whole span", first, last)
	}
	if strong != "**b**" {
		t.Errorf("strong covers %q, want \"**b**\"", strong)
	}
}

func TestSourceSpansFootnoteIsSyntheticOrLocated(t *testing.T) {
	src := "note[^1]\n\n[^1]: text\n"
	_, spans := spansOf(t, src, "note[^1]")
	var found bool
	for _, s := range spans {
		if s.Kind == "footnote-ref" {
			found = true
			if !s.Synthetic && spanText(src, s) != "[^1]" {
				t.Errorf("footnote spans %q", spanText(src, s))
			}
		}
	}
	if !found {
		t.Errorf("no footnote-ref span: %+v", spans)
	}
}

func TestSourceSpansHeading(t *testing.T) {
	src := "# Title **bold**\n\npara\n"
	_, spans := spansOf(t, src, "# Title")
	if len(spans) == 0 {
		t.Fatal("no spans for a heading")
	}
	if spans[0].Kind != "text" {
		t.Errorf("first span is %q", spans[0].Kind)
	}
}

func TestSourceSpansOnlyInlineBlocks(t *testing.T) {
	src := "```go\nx\n```\n\n| a |\n| - |\n| 1 |\n"
	d := New(src)
	for i, r := range d.SourceRanges() {
		if r.Kind == "code" || r.Kind == "table" {
			if s := d.SourceSpans(i); len(s) != 0 {
				t.Errorf("%s block reports %d inline spans", r.Kind, len(s))
			}
		}
	}
}

func TestSpanEditable(t *testing.T) {
	src := "plain **bold** `code` [link](https://x)\n"
	d := New(src)
	r := findRange(t, src, "plain")
	var index int
	for i, rr := range d.SourceRanges() {
		if rr == r {
			index = i
		}
	}
	editable := map[string]bool{}
	for k, s := range d.SourceSpans(index) {
		if strings.TrimSpace(spanText(src, s)) == "" {
			continue // a lone space is not editable content
		}
		editable[s.Kind] = d.SpanEditable(index, k)
	}
	for _, kind := range []string{"text", "strong", "code", "link"} {
		if !editable[kind] {
			t.Errorf("%s span is not editable", kind)
		}
	}
}

func TestSpanEditableRejectsDefinitionsAndCodeBlocks(t *testing.T) {
	src := "see [link][ref]\n\n[ref]: https://x\n"
	d := New(src)
	for i, r := range d.SourceRanges() {
		for k, s := range d.SourceSpans(i) {
			if strings.Contains(spanText(src, s), "[ref]") && s.Kind == "link" {
				if !d.SpanEditable(i, k) {
					t.Errorf("a link using a definition should be editable: %q", spanText(src, s))
				}
			}
		}
		if r.Kind == "code" {
			for k := range d.SourceSpans(i) {
				if d.SpanEditable(i, k) {
					t.Error("a code block span reports editable")
				}
			}
		}
	}
}

func TestSpanAtMapsRunesToSpans(t *testing.T) {
	r := &richText{
		runs:     []run{{text: "ab"}, {text: "cd"}},
		srcl:     []spanSrc{{kind: "text"}, {kind: "strong", synthetic: true}},
		textRuns: 2,
	}
	for _, tc := range []struct {
		rune  int
		span  int
		found bool
	}{
		{0, 0, true}, {1, 0, true}, {2, 1, false}, {3, 1, false}, {4, 0, false},
	} {
		span, ok := r.spanAt(tc.rune)
		if ok != tc.found || (ok && span != tc.span) {
			t.Errorf("spanAt(%d) = %d, %v; want %d, %v", tc.rune, span, ok, tc.span, tc.found)
		}
	}
}

// A click inside an inline span reports the block and the span.
func TestOnSpanClickReportsSpan(t *testing.T) {
	d := New("开头 **粗体** 结尾")
	block, span := -1, -1
	d.OnSpanClick(func(b, s int) { block, span = b, s })
	h := uitest.New(el.Root(docView{d}))
	r := d.chunks[0].blocks[0].view.(*richBlock)
	var x, y float32
	for _, p := range r.rt.pieces {
		if p.run < len(r.runs) && r.runs[p.run].text == "粗体" {
			x = float32(10 + p.rect.Min.X + 2)
			y = float32(10 + (p.rect.Min.Y+p.rect.Max.Y)/2)
		}
	}
	if x == 0 {
		t.Fatal("no piece drawn for the bold span")
	}
	h.Click(x, y)
	if block != 0 {
		t.Fatalf("click reported block %d, span %d", block, span)
	}
	if got := d.SourceSpans(block)[span].Kind; got != "strong" {
		t.Fatalf("click reported a %q span", got)
	}
	if !d.SpanEditable(block, span) {
		t.Error("the clicked span should be editable")
	}
}
