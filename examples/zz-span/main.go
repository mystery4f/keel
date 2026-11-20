// Command zz-span reproduces an editable span in a real window: a markdown
// document whose paragraph embeds an el.Input through an InlineObject,
// surrounded by other blocks, plus a plain el.Input outside the document for
// comparison. Drive it with keel-mcp: click at fractions of each input, type,
// and read the values from the log.
package main

import (
	"flag"
	"log"

	"github.com/dyike/keel/ui/el"
	"github.com/dyike/keel/ui/markdown"
	"github.com/dyike/keel/ui/theme"
	"github.com/dyike/keel/ui/window"
	"github.com/yuin/goldmark/ast"
)

type app struct {
	value string
	plain string
	doc   *markdown.Doc
}

func (a *app) Render(cx *el.Context) el.Element {
	return el.Div().P(24).Gap(16).Child(
		a.doc.Render(cx),
		el.Div().H(el.Dp(1)).Bg(theme.Border),
		el.Input().ID("plain").Name("plain").Bind(&a.plain).OnChange(func(v string) { log.Printf("PLAINCHANGE %q", v) }),
	)
}

func main() {
	flag.Parse()
	a := &app{value: "0123456789", plain: "0123456789"}
	object := markdown.InlineObject{Text: "0123456789", Widget: el.Embed(el.ViewFunc(func(cx *el.Context) el.Element {
		return el.Input().ID("span").Name("span").Bind(&a.value).OnChange(func(v string) { log.Printf("SPANCHANGE %q", v) })
	}))}
	src := "# 标题 Heading\n\nSTART *span* END filler text to widen the line.\n\n- 列表项 one\n- 列表项 two\n\n后面还有一段普通文本，用来把版面撑开一点。\n"
	a.doc = markdown.New(src).Plugins(markdown.Plugin{
		Inlines: map[ast.NodeKind]func(ast.Node, []byte) markdown.InlineObject{
			ast.KindEmphasis: func(n ast.Node, b []byte) markdown.InlineObject {
				if string(n.Text(b)) == "span" {
					return object
				}
				return markdown.InlineObject{}
			},
		},
	})
	content := el.Root(a)
	window.Open(window.Options{Title: "zz-span", Width: 800, Height: 600, Content: content})
	window.Main()
}
