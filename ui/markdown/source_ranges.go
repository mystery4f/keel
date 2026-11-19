package markdown

import (
	"sort"
	"strings"
)

// SourceRange is one top-level rendered block and the bytes of Source it came
// from: Source()[Start:End]. The offsets are bytes, not runes, and both edges
// sit on line boundaries.
//
// The ranges are listed in source order and tile the source: each one starts
// where the one before it ended, so every byte is shown exactly once. Text that
// draws nothing, such as a link reference definition, belongs to the block
// above it.
//
// A footnote list is drawn as an appendix, away from the source lines it comes
// from, so it gets no range: those lines stay with the block above them.
type SourceRange struct {
	// Kind names the block: "paragraph", "heading", "code", "quote", "list",
	// "table", "rule", "front-matter", "footnote-list", "footnote-item",
	// "group" or "custom".
	Kind string
	// Start and End are byte offsets into Source; Start < End.
	Start, End int
}

// SourceRanges reports where each top-level block sits in Source.
//
// A block keeps its span across frames and across Append when the text before
// it did not change length; a chunk that only moved is shifted, not reparsed.
// The document is parsed in chunks split at blank lines, so one range may cover
// several visual blocks: a list is one range, and an HTML block is one "group".
// Blocks that show nothing, such as a chunk that is only blank lines, are left
// out.
//
// While Streaming is on, the tail of the document is parsed with unclosed
// syntax provisionally closed. Ranges stay inside Source, but the last one may
// still move as more text arrives; read them once streaming ends.
//
// The spans are what goldmark reported for its nodes, so a block whose markers
// goldmark does not treat as content (a setext heading's underline, a table's
// delimiter row) may extend beyond the text the block draws.
func (d *Doc) SourceRanges() []SourceRange {
	src := d.src
	docEnd := len(strings.TrimRight(src, "\n"))
	type entry struct {
		b block
	}
	var entries []entry
	for _, c := range d.chunks {
		for _, b := range c.blocks {
			if k := b.kind; k == footnoteList || k == footnoteItem {
				continue // drawn as an appendix, away from its source lines
			}
			entries = append(entries, entry{b: b})
		}
	}
	// The contextual parser splits the document by node, not by position, so
	// footnote lists can arrive last while sitting mid-document: sort by source.
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].b.srcStart < entries[j].b.srcStart })
	var out []SourceRange
	prev := 0
	for i, e := range entries {
		// A block's own source begins at its open offset, which carries the
		// markers: "# " for an ATX heading, the fence line above a fenced code
		// block's first content line.
		start := e.b.open
		// Ranges tile the source in order and never overlap: text between two
		// blocks belongs to the block above, and a block goldmark reports no
		// lines for keeps the lines its neighbours left free.
		if start < prev {
			start = prev
		}
		end := e.b.srcEnd // a line boundary already when inferred
		if !e.b.srcInferred {
			end = lineEndOf(src, e.b.srcEnd)
		}
		if i+1 < len(entries) {
			next := entries[i+1].b
			// The next block's own source starts here, markers included, so the
			// gap before it belongs to the block above. A block goldmark reports
			// no lines for has no open offset: its content line is the boundary.
			bound := next.open
			if next.srcInferred {
				bound = lineStartOf(src, next.srcStart)
			}
			end = max(end, bound)
		} else {
			end = docEnd
		}
		if end <= start || start >= len(src) {
			continue
		}
		out = append(out, SourceRange{Kind: e.b.sourceKind(), Start: start, End: end})
		prev = end
	}
	return out
}

// lineStartOf returns the start of the line holding pos, or 0 before the first
// newline.
func lineStartOf(src string, pos int) int {
	if pos > len(src) {
		pos = len(src)
	}
	if pos < 0 {
		return 0
	}
	if i := strings.LastIndexByte(src[:pos], '\n'); i >= 0 {
		return i + 1
	}
	return 0
}

func (b block) sourceKind() string {
	if b.custom != nil {
		return "custom"
	}
	switch b.kind {
	case heading:
		return "heading"
	case codeBlock:
		return "code"
	case quote:
		return "quote"
	case list:
		return "list"
	case table:
		return "table"
	case rule:
		return "rule"
	case footnoteList:
		return "footnote-list"
	case footnoteItem:
		return "footnote-item"
	case group:
		return "group"
	case frontMatter:
		return "front-matter"
	default:
		return "paragraph"
	}
}

// lineEndOf returns the end of the line holding pos, before its newline.
func lineEndOf(src string, pos int) int {
	if pos < 0 {
		return 0
	}
	if pos > len(src) {
		return len(src)
	}
	if i := strings.IndexByte(src[pos:], '\n'); i >= 0 {
		return pos + i
	}
	return len(src)
}
