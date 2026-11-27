package views

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoteMarkdownBlocksActiveContent(t *testing.T) {
	content := strings.Join([]string{
		"<script>alert(1)</script>",
		"",
		"<img src=https://invalid.example/raw.jpg onerror=alert(1)>",
		"",
		"![external](https://invalid.example/picture.png)",
		"![protocol relative](//invalid.example/picture.png)",
		"![inline data](data:image/png;base64,AAAA)",
		"![absolute path](/static/image.png)",
		"![encoded external](https%3A%2F%2Finvalid.example%2Fpicture.png)",
		"![[https://invalid.example/obsidian.jpg]]",
		"",
		"[direct](javascript:alert(1)) [encoded](javascript&#58;alert(1))",
		"[entity tab](java&#9;script:alert(1)) <javascript:alert(1)>",
		"[safe](https://example.invalid/?x=1&y=2)",
		"",
		"`![[literal.png]]` \\![[escaped.png]]",
		"```md",
		"![fenced image](local.png)",
		"![[fenced.png]]",
		"```",
	}, "\n")

	rendered, err := renderNoteMarkdown(content, "folder/Note.md")
	require.NoError(t, err)
	assert.NotContains(t, rendered, "<img")
	assert.NotContains(t, rendered, "<script")
	assert.NotContains(t, rendered, "onerror=")
	assert.Equal(t, 1, strings.Count(rendered, "<a "))
	assert.Contains(t, rendered, `href="https://example.invalid/?x=1&amp;y=2"`)
	assert.Contains(t, rendered, "external")
	assert.Contains(t, rendered, "<code>![[literal.png]]</code> ![[escaped.png]]")
	assert.Contains(t, rendered, "![fenced image](local.png)\n![[fenced.png]]")
}
