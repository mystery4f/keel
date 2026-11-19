package markdown

import (
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	gmparser "github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// spanSrc records where one rendered span's Markdown sits: a byte range of the
// document, markers included when they could be located.
type spanSrc struct {
	kind       string
	start, end int
	synthetic  bool // drawn but not traceable to source, e.g. a footnote marker
}

// SourceSpan is one inline span of a paragraph or heading, with the byte range
// of its Markdown in Doc.Source.
type SourceSpan struct {
	Kind       string // text, emph, strong, code, link, image, math, footnote-ref, html, object
	Start, End int
	Synthetic  bool // drawn without a matching source range, and not editable
}

// SourceSpans returns the inline spans of the i-th block of SourceRanges, in
// drawing order: the k-th span is drawn from
// Doc.Source()[spans[k].Start:spans[k].End]. Ranges and markers of nested
// spans may overlap: an emphasis and the text inside it cover the same bytes.
//
// Only blocks drawn from inline spans — paragraphs and headings — report any;
// code, tables, quotes and lists report none.
func (d *Doc) SourceSpans(i int) []SourceSpan {
	bs := d.sourceOrderedBlocks()
	if i < 0 || i >= len(bs) || len(bs[i].srcl) == 0 {
		return nil
	}
	srcl := bs[i].srcl
	out := make([]SourceSpan, len(srcl))
	for k, s := range srcl {
		out[k] = SourceSpan{Kind: s.kind, Start: s.start, End: s.end, Synthetic: s.synthetic}
	}
	return out
}

// SpanEditable reports whether the k-th inline span of block i can be edited
// on its own: its Markdown, parsed with the document's definitions, renders to
// the same text. Synthetic spans, spans crossing a line and blocks without
// inline spans are never editable.
func (d *Doc) SpanEditable(i, k int) bool {
	bs := d.sourceOrderedBlocks()
	if i < 0 || i >= len(bs) {
		return false
	}
	b := bs[i]
	if b.kind != "paragraph" && b.kind != "heading" {
		return false
	}
	if k < 0 || k >= len(b.srcl) || k >= len(b.spans) {
		return false
	}
	s := b.srcl[k]
	src := d.Source()
	if s.synthetic || s.end <= s.start || s.start < 0 || s.end > len(src) {
		return false
	}
	snippet := src[s.start:s.end]
	if strings.ContainsAny(snippet, "\n\r") {
		return false
	}
	candidate := snippet + "\n\n" + d.definitions()
	doc := parser.Parse(text.NewReader([]byte(candidate)))
	if doc == nil || doc.FirstChild() == nil {
		return false
	}
	first := doc.FirstChild()
	switch first.Kind() {
	case ast.KindParagraph, ast.KindTextBlock:
	default:
		return false
	}
	spans, _ := inlineSpans(first, []byte(candidate), 0, span{}, mathMacros{})
	// Trailing spaces are stripped by the parser, so compare trimmed text.
	return strings.TrimSpace(plain(spans)) == strings.TrimSpace(b.spans[k].text)
}

// OnSpanClick reports clicks on an inline span of a paragraph or heading. The
// block indexes SourceRanges and the span indexes SourceSpans(block). A span
// drawn without a source range reports Synthetic and is skipped here.
func (d *Doc) OnSpanClick(fn func(block, span int)) *Doc {
	d.onSpanClick = fn
	return d
}

// blockIndex returns the index of b in SourceRanges, or -1.
func (d *Doc) blockIndex(b *block) int {
	for i, e := range d.sourceOrderedBlocks() {
		if e.open == b.open && e.srcStart == b.srcStart {
			return i
		}
	}
	return -1
}

// definitions returns the document's link reference and footnote definitions,
// so one inline span can be parsed on its own.
func (d *Doc) definitions() string {
	if d.defsSrc == d.src {
		return d.defs
	}
	var b strings.Builder
	for _, line := range strings.Split(d.src, "\n") {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "[^") && strings.Contains(t, "]:") {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	pc := gmparser.NewContext()
	parser.Parse(text.NewReader([]byte(d.src)), gmparser.WithContext(pc))
	for _, ref := range pc.References() {
		b.WriteString("[" + string(ref.Label()) + "]: " + string(ref.Destination()))
		if t := ref.Title(); len(t) > 0 {
			b.WriteString(` "` + string(t) + `"`)
		}
		b.WriteString("\n")
	}
	d.defs, d.defsSrc = b.String(), d.src
	return d.defs
}

// spanKind names a span by how it is drawn.
func spanKind(s span) string {
	switch {
	case s.object != nil:
		return "object"
	case s.math != nil:
		return "math"
	case s.imageURL != "":
		return "image"
	case s.code:
		return "code"
	case s.link != "":
		return "link"
	case s.bold:
		return "strong"
	case s.italic:
		return "emph"
	case s.strike:
		return "strike"
	}
	return "text"
}

// spanAt maps a rune index of the drawn text to the span drawn there.
func (r *richText) spanAt(i int) (int, bool) {
	pos := 0
	for k := 0; k < r.textRuns && k < len(r.runs) && k < len(r.srcl); k++ {
		n := utf8.RuneCountInString(r.runs[k].text)
		if i < pos+n {
			return k, !r.srcl[k].synthetic
		}
		pos += n
	}
	return 0, false
}
