package markdown

import (
	"bytes"
	"github.com/dyike/keel/ui/el"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	gmparser "github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// The intermediate form: what a chunk of Markdown shows, independent of how it
// is drawn. It is built once per chunk and reused across frames.

type blockKind uint8

const (
	paragraph blockKind = iota
	heading
	codeBlock
	quote
	list
	table
	rule
	footnoteList
	footnoteItem
	group       // several blocks from one HTML block, drawn one after another
	frontMatter // YAML metadata at the top; code holds it
)

type block struct {
	custom   el.View
	kind     blockKind
	anchor   string
	level    int       // heading
	spans    []span    // paragraph, heading
	srcl     []spanSrc // where each span's Markdown sits; parallel to spans
	lang     string    // codeBlock
	code     string
	children []block // quote
	ordered  bool    // list
	start    int
	items    []listItem
	tbl      *tableData

	srcStart, srcEnd int  // source bytes covered; zero when unknown
	srcInferred      bool // span taken from the neighbours, not from goldmark
	open             int  // first byte of the block's own source, markers included
	view             any  // drawing state that must survive frames (rich text, copy feedback)
}

type listItem struct {
	task   *bool // nil: not a task item
	blocks []block
}

type cellAlign uint8

const (
	alignLeft cellAlign = iota
	alignCenter
	alignRight
)

type tableData struct {
	align  []cellAlign
	header [][]span
	rows   [][][]span
}

type span struct {
	object                     *InlineObject
	text                       string
	bold, italic, code, strike bool
	link                       string
	math                       *mathExpr
	display                    bool
	anchor                     string
	superscript, subscript     bool
	underline, mark            bool // from HTML <u> and <mark>
	imageURL                   string
	imageAlt                   string
}

var parser = goldmark.New(goldmark.WithExtensions(extension.GFM, extension.Footnote), goldmark.WithParserOptions(gmparser.WithInlineParsers(util.Prioritized(mathInlineParser{}, 50)), gmparser.WithBlockParsers(util.Prioritized(mathBlockParser{}, 50)))).Parser()

// parse turns one chunk of Markdown into blocks at offset 0.
func parse(src string) []block { return parseAt(src, 0) }

// parseAt is parse for a chunk that starts at base, its byte offset inside
// Doc.Source; every span it reports is measured from there.
func parseAt(src string, base int) []block {
	source := []byte(src)
	doc := parser.Parse(text.NewReader(source))
	return blocks(doc, source, base, mathMacros{})
}

func blocks(parent ast.Node, src []byte, base int, macros mathMacros, plugins ...*pluginSet) []block {
	var out []block
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		if b, ok := convert(n, src, base, macros, plugins...); ok {
			out = append(out, b)
		}
	}
	// A leaf goldmark reports no lines for, such as a thematic break, takes the
	// lines between its neighbours: the gap left by the blocks around it.
	for i := range out {
		if out[i].srcEnd > out[i].srcStart {
			continue
		}
		start, end := 0, len(src)
		if i > 0 {
			start = nextLine(src, out[i-1].srcEnd-base)
		}
		for j := i + 1; j < len(out); j++ {
			if out[j].srcEnd > out[j].srcStart {
				end = out[j].open - base
				break
			}
		}
		if end > start {
			out[i].srcStart, out[i].srcEnd = base+start, base+end
			out[i].srcInferred = true
		}
	}
	return out
}

// nextLine returns the start of the first line after pos.
func nextLine(src []byte, pos int) int {
	if pos < 0 {
		pos = 0
	}
	if pos > len(src) {
		return len(src)
	}
	if i := bytes.IndexByte(src[pos:], '\n'); i >= 0 {
		return pos + i + 1
	}
	return len(src)
}

// lineStartAt returns the start of the line holding pos.
func lineStartAt(src []byte, pos int) int {
	if pos > len(src) {
		pos = len(src)
	}
	if pos < 0 {
		return 0
	}
	if i := bytes.LastIndexByte(src[:pos], '\n'); i >= 0 {
		return i + 1
	}
	return 0
}

func convert(n ast.Node, src []byte, base int, macros mathMacros, plugins ...*pluginSet) (block, bool) {
	b, ok := convertInner(n, src, base, macros, plugins...)
	if !ok {
		return block{}, false
	}
	if s, e := sourceSpan(n, b, base); e > s {
		b.srcStart, b.srcEnd = s, e
		b.open = base + lineStartAt(src, s-base)
		// A fenced code block and a display math block begin one line above
		// their content: the fence, the opening $$.
		switch n.(type) {
		case *ast.FencedCodeBlock, *displayMath:
			b.open = base + lineStartAt(src, max(0, s-base-1))
		}
	}
	return b, true
}

// sourceSpan reads the line segments goldmark kept for the node; they index the
// chunk source, so the chunk's own base turns them into Doc.Source offsets.
// A container carries no segments: its extent is its children's.
func sourceSpan(n ast.Node, b block, base int) (int, int) {
	if ls := n.Lines(); ls.Len() > 0 {
		return base + ls.At(0).Start, base + ls.At(ls.Len()-1).Stop
	}
	start, end, set := 0, 0, false
	add := func(s, e int) {
		if e <= s {
			return
		}
		if !set || s < start {
			start = s
		}
		if !set || e > end {
			end = e
		}
		set = true
	}
	for _, c := range b.children {
		add(c.srcStart, c.srcEnd)
	}
	for _, it := range b.items {
		for _, c := range it.blocks {
			add(c.srcStart, c.srcEnd)
		}
	}
	return start, end
}

func convertInner(n ast.Node, src []byte, base int, macros mathMacros, plugins ...*pluginSet) (block, bool) {
	if view := customBlock(plugins, n, src); view != nil {
		return block{custom: view}, true
	}
	switch n := n.(type) {
	case *east.FootnoteList:
		return block{kind: footnoteList, children: blocks(n, src, base, macros, plugins...)}, true
	case *east.Footnote:
		children := blocks(n, src, base, macros, plugins...)
		anchorFirst(children, footnoteID(n.Index))
		return block{kind: footnoteItem, level: n.Index, children: children}, true
	case *east.FootnoteBacklink:
		return block{kind: paragraph, spans: []span{backlinkSpan(n)}}, true
	case *displayMath:
		source := lines(n, src)
		s := span{text: "$$\n" + source, display: true}
		if n.closed {
			s.text += "\n$$"
			s.math = parseMathIn(source, macros)
		}
		return block{kind: paragraph, spans: []span{s}}, true
	case *ast.Paragraph, *ast.TextBlock:
		b := block{kind: paragraph}
		b.spans, b.srcl = inlineSpans(n, src, base, span{}, macros, plugins...)
		return b, true
	case *ast.Heading:
		b := block{kind: heading, level: n.Level}
		b.spans, b.srcl = inlineSpans(n, src, base, span{}, macros, plugins...)
		return b, true
	case *ast.FencedCodeBlock:
		lang := ""
		if n.Info != nil {
			lang = strings.Fields(string(n.Info.Segment.Value(src)) + " ")[0]
		}
		return block{kind: codeBlock, lang: lang, code: lines(n, src)}, true
	case *ast.CodeBlock:
		return block{kind: codeBlock, code: lines(n, src)}, true
	case *ast.Blockquote:
		return block{kind: quote, children: blocks(n, src, base, macros, plugins...)}, true
	case *ast.List:
		b := block{kind: list, ordered: n.IsOrdered(), start: n.Start}
		for it := n.FirstChild(); it != nil; it = it.NextSibling() {
			item := listItem{blocks: blocks(it, src, base, macros, plugins...)}
			item.task = taskState(it)
			b.items = append(b.items, item)
		}
		return b, true
	case *ast.ThematicBreak:
		return block{kind: rule}, true
	case *ast.HTMLBlock:
		// An HTML block may hold several blocks; the first goes here and the
		// rest follow it as a quote-less group.
		bs := htmlBlocks(lines(n, src) + closure(n, src))
		switch len(bs) {
		case 0:
			return block{}, false
		case 1:
			return bs[0], true
		}
		return block{kind: group, children: bs}, true
	case *east.Table:
		return block{kind: table, tbl: tableOf(n, src, macros, plugins...)}, true
	}
	return block{}, false
}

func lines(n ast.Node, src []byte) string {
	var b strings.Builder
	ls := n.Lines()
	for i := 0; i < ls.Len(); i++ {
		seg := ls.At(i)
		b.Write(seg.Value(src))
	}
	return strings.TrimRight(b.String(), "\n")
}

// taskState finds a GFM task checkbox at the start of a list item.
func taskState(item ast.Node) *bool {
	first := item.FirstChild()
	if first == nil {
		return nil
	}
	if cb, ok := first.FirstChild().(*east.TaskCheckBox); ok {
		v := cb.IsChecked
		return &v
	}
	return nil
}

func tableOf(t *east.Table, src []byte, macros mathMacros, plugins ...*pluginSet) *tableData {
	d := &tableData{}
	for _, a := range t.Alignments {
		switch a {
		case east.AlignCenter:
			d.align = append(d.align, alignCenter)
		case east.AlignRight:
			d.align = append(d.align, alignRight)
		default:
			d.align = append(d.align, alignLeft)
		}
	}
	for r := t.FirstChild(); r != nil; r = r.NextSibling() {
		var cells [][]span
		for c := r.FirstChild(); c != nil; c = c.NextSibling() {
			cells = append(cells, inlines(c, src, span{}, macros, plugins...))
		}
		if _, ok := r.(*east.TableHeader); ok {
			d.header = cells
		} else {
			d.rows = append(d.rows, cells)
		}
	}
	return d
}

// inlines flattens inline content into styled spans, merging neighbours with
// the same style.
// inlines renders an inline container without collecting source positions.
func inlines(n ast.Node, src []byte, style span, macros mathMacros, plugins ...*pluginSet) []span {
	spans, _ := inlineSpans(n, src, 0, style, macros, plugins...)
	return spans
}

func endsCJK(s string) bool {
	r, _ := utf8.DecodeLastRuneInString(s)
	return unicode.Is(unicode.Han, r) || unicode.In(r, unicode.Hiragana, unicode.Katakana, unicode.Hangul) || (r >= 0x3000 && r <= 0x303f) || (r >= 0xff00 && r <= 0xffef)
}

func plain(spans []span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.text)
	}
	return b.String()
}
