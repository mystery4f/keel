package markdown

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/dyike/keel/ui/locale"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
)

// inlineSpans turns an inline container's children into styled spans and
// records, for each span, where its Markdown sits in src. src is the chunk's
// source and base its offset in the document, so the recorded ranges are
// document offsets, like block spans.
//
// The two results are parallel: spans[i] is drawn from src[sources[i]], so a
// rune index in a span leads back to its Markdown.
func inlineSpans(n ast.Node, src []byte, base int, style span, macros mathMacros, plugins ...*pluginSet) ([]span, []spanSrc) {
	var out []span
	var srcs []spanSrc
	cursor := 0 // end of the last recorded source, for nodes without a position

	add := func(s span, sr spanSrc) {
		if s.text == "" {
			return
		}
		if e := sr.end - base; e > cursor {
			cursor = e
		}
		sr.start += base
		sr.end += base
		if k := len(out) - 1; k >= 0 {
			last := &out[k]
			if last.object == nil && s.object == nil && last.imageURL == "" && s.imageURL == "" && last.anchor == "" && s.anchor == "" && last.superscript == s.superscript && last.subscript == s.subscript && last.underline == s.underline && last.mark == s.mark && last.math == nil && s.math == nil && last.bold == s.bold && last.italic == s.italic && last.code == s.code && last.strike == s.strike && last.link == s.link {
				last.text += s.text
				switch {
				case srcs[k].synthetic:
				case sr.synthetic:
					srcs[k].synthetic = true
				default:
					srcs[k].end = sr.end
				}
				return
			}
		}
		if sr.kind == "text" {
			sr.kind = spanKind(s)
		}
		out = append(out, s)
		srcs = append(srcs, sr)
	}
	// open widens the first span recorded after mark to the left, and the
	// last one to the right, so a container's own markers belong to it.
	open := func(mark int, left, right string) {
		if len(srcs) <= mark {
			return
		}
		first, last := &srcs[mark], &srcs[len(srcs)-1]
		if !first.synthetic && first.start >= base {
			first.start = base + markerLeft(src, first.start-base, left)
		}
		if !last.synthetic && last.end >= base {
			last.end = base + markerRight(src, last.end-base, right)
		}
	}
	var walk func(n ast.Node, st span)
	walk = func(n ast.Node, st span) {
		var tags htmlInline // inline HTML tags style their later siblings
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			if object := customInline(plugins, c, src); object != nil {
				s := st
				s.text, s.object = object.Text, object
				add(s, spanSrc{kind: "object", synthetic: true})
				continue
			}
			switch c := c.(type) {
			case *east.FootnoteLink:
				lit := "[^" + strconv.Itoa(c.Index) + "]"
				sr := spanSrc{kind: "footnote-ref", start: cursor, end: cursor}
				if p, ok := findLocal(src, cursor, lit); ok {
					sr.start, sr.end = p, p+len(lit)
				} else {
					sr.synthetic = true
				}
				add(span{text: "[" + strconv.Itoa(c.Index) + "]", link: footnoteID(c.Index), anchor: footnoteRefID(c.Index, c.RefIndex), superscript: true}, sr)
			case *east.FootnoteBacklink:
				add(backlinkSpan(c), spanSrc{kind: "footnote-ref", synthetic: true})
			case *mathInline:
				s := st
				s.text, s.math, s.display = c.raw, parseMathIn(c.source, macros), c.display
				sr := spanSrc{kind: "math", start: cursor, end: cursor}
				if p, ok := findLocal(src, cursor, c.raw); ok {
					sr.start, sr.end = p, p+len(c.raw)
				} else {
					sr.synthetic = true
				}
				add(s, sr)
			case *ast.Text:
				s := st
				s.text = string(c.Segment.Value(src))
				add(s, spanSrc{kind: "text", start: c.Segment.Start, end: c.Segment.Stop})
				switch {
				case c.HardLineBreak():
					add(span{text: "\n", bold: st.bold, italic: st.italic, link: st.link}, spanSrc{kind: "text", start: c.Segment.Stop, end: c.Segment.Stop + 1, synthetic: true})
				case c.SoftLineBreak():
					// A soft break is a space, except between CJK characters.
					if !endsCJK(s.text) {
						b := st
						b.text = " "
						add(b, spanSrc{kind: "text", start: c.Segment.Stop, end: c.Segment.Stop + 1, synthetic: true})
					}
				}
			case *ast.String:
				s := st
				s.text = string(c.Value)
				add(s, spanSrc{kind: "text", start: cursor, end: cursor + len(c.Value)})
			case *ast.CodeSpan:
				s := st
				s.code = true
				var b strings.Builder
				start, end := -1, -1
				for t := c.FirstChild(); t != nil; t = t.NextSibling() {
					if tt, ok := t.(*ast.Text); ok {
						b.Write(tt.Segment.Value(src))
						if start < 0 {
							start = tt.Segment.Start
						}
						end = tt.Segment.Stop
					}
				}
				s.text = b.String()
				if start < 0 {
					add(s, spanSrc{kind: "code", synthetic: true})
					break
				}
				add(s, spanSrc{kind: "code", start: markerLeft(src, start, "`"), end: markerRight(src, end, "`")})
			case *ast.Emphasis:
				s := st
				if c.Level >= 2 {
					s.bold = true
				} else {
					s.italic = true
				}
				mark := len(srcs)
				walk(c, s)
				open(mark, "*_", "*_")
			case *east.Strikethrough:
				s := st
				s.strike = true
				mark := len(srcs)
				walk(c, s)
				open(mark, "~", "~")
			case *ast.Link:
				s := st
				s.link = string(c.Destination)
				mark := len(srcs)
				walk(c, s)
				open(mark, "[", "")
				if len(srcs) > mark && !srcs[len(srcs)-1].synthetic {
					if e, ok := closeLink(src, srcs[len(srcs)-1].end-base); ok {
						srcs[len(srcs)-1].end = base + e
					}
				}
			case *ast.AutoLink:
				s := st
				s.link = string(c.URL(src))
				s.text = string(c.Label(src))
				mark := len(srcs)
				add(s, spanSrc{kind: "link", start: cursor, end: cursor})
				if len(srcs) > mark && !srcs[mark].synthetic {
					first := &srcs[mark]
					if p, ok := findLocal(src, cursor, string(c.Label(src))); ok {
						first.start = base + p
						first.end = base + p + len(c.Label(src))
						if p > 0 && src[p-1] == '<' && p+len(c.Label(src)) < len(src) && src[p+len(c.Label(src))] == '>' {
							first.start = base + p - 1
							first.end = base + p + len(c.Label(src)) + 1
						}
					}
				}
			case *ast.Image:
				s := st
				s.imageURL = string(c.Destination)
				var alt strings.Builder
				start, end := -1, -1
				ast.Walk(c, func(t ast.Node, entering bool) (ast.WalkStatus, error) {
					if tt, ok := t.(*ast.Text); entering && ok {
						alt.Write(tt.Segment.Value(src))
						if start < 0 {
							start = tt.Segment.Start
						}
						end = tt.Segment.Stop
					}
					return ast.WalkContinue, nil
				})
				s.imageAlt = alt.String()
				s.text = "[" + locale.Current().Name(locale.Current().Image, s.imageAlt) + "]"
				sr := spanSrc{kind: "image", synthetic: true}
				if start >= 0 {
					sr = spanSrc{kind: "image", start: markerLeft(src, start, "["), end: end}
					if sr.start > 0 && src[sr.start-1] == '!' {
						sr.start--
					}
					if e, ok := closeLink(src, sr.end); ok {
						sr.end = e
					}
				}
				add(s, sr)
			case *east.TaskCheckBox:
				// drawn as the list marker instead
			case *ast.RawHTML:
				var raw strings.Builder
				start, end := -1, -1
				for i := 0; i < c.Segments.Len(); i++ {
					seg := c.Segments.At(i)
					raw.Write(seg.Value(src))
					if start < 0 {
						start = seg.Start
					}
					end = seg.Stop
				}
				sr := spanSrc{kind: "html", synthetic: start < 0}
				if start >= 0 {
					sr.start, sr.end = start, end
				}
				next, insert, ok := tags.tag(raw.String(), st)
				if !ok { // not a known element, e.g. the <T> in List<T>
					s := st
					s.text = raw.String()
					add(s, sr)
					continue
				}
				add(insert, sr)
				st = next
			default:
				walk(c, st)
			}
		}
	}
	walk(n, style)
	return out, srcs
}

// findLocal locates lit in src at or after from.
func findLocal(src []byte, from int, lit string) (int, bool) {
	if lit == "" || from < 0 || from > len(src) {
		return 0, false
	}
	i := bytes.Index(src[from:], []byte(lit))
	if i < 0 {
		return 0, false
	}
	return from + i, true
}

// markerLeft walks back over a run of marker bytes ending at pos.
func markerLeft(src []byte, pos int, markers string) int {
	for pos > 0 && strings.IndexByte(markers, src[pos-1]) >= 0 {
		pos--
	}
	return pos
}

// markerRight walks forward over a run of marker bytes starting at pos.
func markerRight(src []byte, pos int, markers string) int {
	for pos < len(src) && strings.IndexByte(markers, src[pos]) >= 0 {
		pos++
	}
	return pos
}

// closeLink returns the end of a link's trailing "] (...)" or "][label]"
// starting at pos, which must hold the closing "]".
func closeLink(src []byte, pos int) (int, bool) {
	if pos < 0 || pos >= len(src) || src[pos] != ']' {
		return 0, false
	}
	switch {
	case pos+1 < len(src) && src[pos+1] == '(':
		return scanTo(src, pos+2, '(', ')')
	case pos+1 < len(src) && src[pos+1] == '[':
		return scanTo(src, pos+2, '[', ']')
	}
	return pos + 1, true
}

// scanTo scans from pos to the closing bracket, honouring nesting, escapes and
// staying on the same line.
func scanTo(src []byte, pos int, open, close byte) (int, bool) {
	depth := 1
	for i := pos; i < len(src); i++ {
		switch src[i] {
		case '\\':
			i++
		case '\n':
			return 0, false
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}
