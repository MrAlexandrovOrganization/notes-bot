// Package noteimage identifies local raster images embedded in an Obsidian note.
package noteimage

import (
	"net/url"
	"path"
	"regexp"
	"strings"
)

type Embed struct {
	Markup    string `json:"markup"`
	Reference string `json:"reference"`
}

var imageLink = regexp.MustCompile(`!\[\[([^\]\n|]+)(?:\|[^\]\n]*)?\]\]|!\[[^\]\n]*\]\((?:<([^>\n]+)>|((?:[^()\n]|\([^()\n]*\))+?))(?:\s+"[^"\n]*")?\)`)

func Parse(text string) []Embed {
	var result []Embed
	fence := ""
	for _, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "~~~") || strings.HasPrefix(trim, "```") {
			if fence == "" {
				fence = trim[:3]
			} else if strings.HasPrefix(trim, fence) {
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		for _, m := range imageLink.FindAllStringSubmatch(line, -1) {
			ref := m[1]
			if ref == "" {
				v := m[2]
				if v == "" {
					v = m[3]
				}
				var err error
				ref, err = url.PathUnescape(v)
				if err != nil {
					continue
				}
			}
			ref = strings.TrimSpace(ref)
			if strings.ContainsAny(ref, ":\\\x00") || strings.HasPrefix(ref, "/") {
				continue
			}
			switch strings.ToLower(path.Ext(ref)) {
			case ".jpg", ".jpeg", ".png", ".gif", ".webp":
				result = append(result, Embed{m[0], ref})
			}
		}
	}
	return result
}
