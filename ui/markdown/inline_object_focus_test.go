package markdown

import (
	"fmt"
	"testing"

	"github.com/dyike/keel/gio/io/input"
	"github.com/yuin/goldmark/ast"

	"github.com/dyike/keel/ui/el"
	"github.com/dyike/keel/ui/internal/uitest"
)

// TestInlineObjectClickFocusType locks the inline-editing chain: an InlineObject
// embedding an el input receives the press, keeps keyboard focus against the
// document selection, and typed text lands in the bound value.
func TestInlineObjectClickFocusType(t *testing.T) {
	src := "段首 [标签](https://example.com) 段尾"
	var val string
	d := New(src).Plugins(Plugin{
		Local: true,
		Inlines: map[ast.NodeKind]func(ast.Node, []byte) InlineObject{
			ast.KindLink: func(n ast.Node, source []byte) InlineObject {
				return InlineObject{
					Text: string(n.Text(source)),
					Widget: el.Embed(el.ViewFunc(func(cx *el.Context) el.Element {
						return el.Input().ID("inp").Bind(&val)
					})),
				}
			},
		},
	})
	h := uitest.New(el.Root(docView{d}))
	h.Frame()

	// Locate the embedded editor from the semantic tree, by class, not by ref.
	var cx, cy float32
	found := false
	walk(h.Router.AppendSemantics(nil)[0], func(n input.SemanticNode) {
		if found || fmt.Sprint(n.Desc.Class) != "Editor" {
			return
		}
		b := n.Desc.Bounds
		cx = float32(b.Min.X+b.Max.X) / 2
		cy = float32(b.Min.Y+b.Max.Y) / 2
		found = true
	})
	if !found {
		t.Fatal("no editor in the semantic tree")
	}

	h.Click(cx, cy)
	h.Type("Z")
	h.Type("Y")
	if val != "YZ" {
		t.Fatalf("typed text did not reach the input: val=%q", val)
	}

	// The same press must not have started a document selection: the selection
	// collapsed and OnSpanClick never fires for a press inside the object.
	// Re-locate the editor first: typing may have moved it.
	clicked := 0
	d.OnSpanClick(func(block, span int) { clicked++ })
	walk(h.Router.AppendSemantics(nil)[0], func(n input.SemanticNode) {
		if fmt.Sprint(n.Desc.Class) != "Editor" {
			return
		}
		b := n.Desc.Bounds
		t.Logf("editor node %v label=%q", b, n.Desc.Label)
		cx = float32(b.Min.X+b.Max.X) / 2
		cy = float32(b.Min.Y+b.Max.Y) / 2
	})
	h.Click(cx, cy)
	h.Frame()
	if clicked != 0 {
		t.Fatalf("OnSpanClick fired for a press inside the object: block,span reported %d times", clicked)
	}
}
