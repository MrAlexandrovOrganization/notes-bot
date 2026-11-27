package views

import (
	"bytes"
	"net/url"
	"path"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"notes-bot/internal/noteimage"
)

const NoteImagePath = "/notes/image"

var noteMarkdownParser = goldmark.New(
	goldmark.WithParserOptions(parser.WithInlineParsers(util.Prioritized(obsidianImageParser{}, 190))),
	goldmark.WithRendererOptions(html.WithHardWraps()),
)

// Obsidian embeds use the existing vault reference rules. Parsing them as
// inline nodes leaves escaped markup, code spans and fenced blocks literal.
type obsidianImageParser struct{}

func (obsidianImageParser) Trigger() []byte { return []byte{'!'} }

func (obsidianImageParser) Parse(_ ast.Node, reader text.Reader, _ parser.Context) ast.Node {
	line, _ := reader.PeekLine()
	rest, ok := bytes.CutPrefix(line, []byte("![["))
	if !ok {
		return nil
	}
	embed, _, ok := bytes.Cut(rest, []byte("]]"))
	if !ok {
		return nil
	}
	length := len(embed) + len("![[") + len("]]")
	images := noteimage.Parse(string(line[:length]))
	if len(images) != 1 || len(images[0].Markup) != length {
		return nil
	}
	ref := images[0].Reference
	link := ast.NewLink()
	destination := url.URL{Path: ref}
	link.Destination = []byte(destination.EscapedPath())
	link.AppendChild(link, ast.NewString([]byte(path.Base(ref))))
	reader.Advance(length)
	return ast.NewImage(link)
}

func renderNoteMarkdown(content, note string) (string, error) {
	source := []byte(content)
	doc := noteMarkdownParser.Parser().Parse(text.NewReader(source))
	var remove []ast.Node
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := node.(type) {
		case *ast.Image:
			ref, err := url.PathUnescape(string(util.URLEscape(n.Destination, true)))
			if err != nil || !localRasterReference(ref) {
				remove = append(remove, n)
				break
			}
			query := url.Values{"note": {note}, "reference": {ref}}
			n.Destination = []byte(NoteImagePath + "?" + query.Encode())
			n.SetAttributeString("loading", "lazy")
			n.SetAttributeString("decoding", "async")
		case *ast.Link:
			if !safeNoteLink(util.URLEscape(n.Destination, true)) {
				remove = append(remove, n)
			}
		case *ast.AutoLink:
			if n.AutoLinkType != ast.AutoLinkEmail && !safeNoteLink(util.URLEscape(n.URL(source), false)) {
				remove = append(remove, n)
			}
		}
		return ast.WalkContinue, nil
	})
	// Work from the innermost nodes out so descriptions keep their formatting.
	for i := len(remove) - 1; i >= 0; i-- {
		node := remove[i]
		parent := node.Parent()
		if link, ok := node.(*ast.AutoLink); ok {
			parent.InsertBefore(parent, node, ast.NewString(link.Label(source)))
		}
		for child := node.FirstChild(); child != nil; child = node.FirstChild() {
			node.RemoveChild(node, child)
			parent.InsertBefore(parent, node, child)
		}
		parent.RemoveChild(parent, node)
	}

	var rendered bytes.Buffer
	if err := noteMarkdownParser.Renderer().Render(&rendered, source, doc); err != nil {
		return "", err
	}
	return rendered.String(), nil
}

func localRasterReference(ref string) bool {
	return noteimage.IsRaster(ref) && !strings.ContainsAny(ref, ":\\\x00\r\n") && !strings.HasPrefix(ref, "/")
}

func safeNoteLink(destination []byte) bool {
	link, err := url.Parse(string(destination))
	if err != nil {
		return false
	}
	switch strings.ToLower(link.Scheme) {
	case "", "http", "https", "mailto":
		return true
	}
	return false
}
