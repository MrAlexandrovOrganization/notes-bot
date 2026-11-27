// Package journalreport builds a read-only projection of the Obsidian vault.
package journalreport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	dailyLayout = "02-Jan-2006"
	isoDate     = "2006-01-02"
	peopleDir   = "Tags/People/"
)

var (
	delimiter   = regexp.MustCompile("(?m)^---[ \t\r]*$")
	rating      = regexp.MustCompile("(?m)^Оценка:[ \t]*([^\r\n]*)")
	taskLine    = regexp.MustCompile(`^[-*+]\s+\[([ xX?\-])\]\s*(.*)$`)
	gymTitle    = regexp.MustCompile(`(?i)^зал[\s.!✅]*$`)
	completion  = regexp.MustCompile(`\[completion::\s*(\d{4}-\d{2}-\d{2})\s*\]`)
	htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	obComment   = regexp.MustCompile(`(?s)%%.*?%%`)
	inlineCode  = regexp.MustCompile("`[^`\n]*`")
	wikiLink    = regexp.MustCompile(`!?\[\[([^\]\n]+)\]\]`)
	mdLink      = regexp.MustCompile(`!?\[([^\]\n]*)\]\(<?([^>\r\n)]+)>?\)`)
)

type Day struct {
	Date       time.Time
	Rating     *int
	Characters int
	Links      int
}

type Person struct {
	ID   string
	Name string
}

type Meeting struct {
	Date   time.Time
	First  Person
	Second Person
}

type Snapshot struct {
	Days     []Day
	GymDays  []time.Time
	Meetings []Meeting
}

type noteIndex struct {
	byPath map[string]string
	byName map[string][]string
}

func Scan(ctx context.Context, vault string) (Snapshot, error) {
	root, err := os.OpenRoot(vault)
	if err != nil {
		return Snapshot{}, err
	}
	defer root.Close()
	return scan(ctx, root.FS())
}

func scan(ctx context.Context, vault fs.FS) (Snapshot, error) {
	var files []string
	index := noteIndex{byPath: make(map[string]string), byName: make(map[string][]string)}
	dates := make(map[string]time.Time)
	err := fs.WalkDir(vault, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if name != "." && (strings.HasPrefix(entry.Name(), ".") || name == "Trash" || name == "Templates") {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || strings.HasPrefix(entry.Name(), ".") || !strings.EqualFold(path.Ext(name), ".md") {
			return nil
		}
		files = append(files, name)
		index.byPath[strings.ToLower(withoutExtension(name))] = name
		base := strings.ToLower(withoutExtension(entry.Name()))
		index.byName[base] = append(index.byName[base], name)
		if path.Dir(name) == "Daily" {
			if day, parseErr := time.Parse(dailyLayout, withoutExtension(entry.Name())); parseErr == nil {
				dates[name] = day
			}
		}
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}
	if len(dates) == 0 {
		return Snapshot{}, errors.New("no daily notes found")
	}

	var result Snapshot
	related := make(map[string]map[string]struct{}, len(dates))
	for name := range dates {
		related[name] = make(map[string]struct{})
	}
	gymDays := make(map[time.Time]struct{})
	for _, name := range files {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		data, err := fs.ReadFile(vault, name)
		if err != nil {
			return Snapshot{}, err
		}
		header, body := splitNote(string(data))
		body = withoutCodeAndComments(body)
		targets := index.links(name, header+"\n"+body)
		for target := range targets {
			if set, ok := related[name]; ok {
				set[target] = struct{}{}
			}
			if set, ok := related[target]; ok {
				set[name] = struct{}{}
			}
		}
		date, daily := dates[name]
		if !daily {
			continue
		}
		result.Days = append(result.Days, Day{
			Date: date, Rating: readRating(header), Characters: diaryCharacters(body),
		})
		findGymDays(body, date, gymDays)
		result.Meetings = append(result.Meetings, findMeetings(date, targets)...)
	}
	days := make(map[time.Time]int, len(dates))
	for name, date := range dates {
		days[date] = len(related[name])
	}
	for i := range result.Days {
		result.Days[i].Links = days[result.Days[i].Date]
	}
	for date := range gymDays {
		result.GymDays = append(result.GymDays, date)
	}
	return result, nil
}

func withoutExtension(name string) string {
	if strings.EqualFold(path.Ext(name), ".md") {
		return name[:len(name)-3]
	}
	return name
}

func splitNote(content string) (header, body string) {
	content = strings.TrimPrefix(content, "\ufeff")
	bounds := delimiter.FindAllStringIndex(content, 2)
	if len(bounds) == 2 && bounds[0][0] == 0 {
		return content[bounds[0][1]+1 : bounds[1][0]], strings.TrimPrefix(content[bounds[1][1]:], "\n")
	}
	return "", content
}

func readRating(header string) *int {
	match := rating.FindStringSubmatch(header)
	if match == nil {
		return nil
	}
	value, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(match[1], " #", 2)[0]))
	if err != nil || value < 0 || value > 10 {
		return nil
	}
	return &value
}

func withoutCodeAndComments(body string) string {
	body = htmlComment.ReplaceAllString(body, "")
	body = obComment.ReplaceAllString(body, "")
	var clean strings.Builder
	var fence byte
	var width int
	for line := range strings.Lines(body) {
		start := strings.TrimSpace(line)
		if start != "" && (start[0] == '`' || start[0] == '~') {
			marker := start[0]
			after := strings.TrimLeft(start, string(marker))
			length := len(start) - len(after)
			if fence == 0 && length >= 3 {
				fence, width = marker, length
				continue
			}
			if fence == marker && length >= width && after == "" {
				fence = 0
				continue
			}
		}
		if fence == 0 {
			clean.WriteString(inlineCode.ReplaceAllString(line, ""))
		}
	}
	return clean.String()
}

func diaryCharacters(body string) int {
	total := 0
	for line := range strings.Lines(body) {
		line = strings.TrimSpace(line)
		if line == "---" || taskLine.MatchString(line) {
			continue
		}
		line = wikiLink.ReplaceAllStringFunc(line, func(link string) string {
			if strings.HasPrefix(link, "!") {
				return ""
			}
			target := strings.TrimSuffix(strings.TrimPrefix(link, "[["), "]]")
			if _, label, ok := strings.Cut(target, "|"); ok {
				return label
			}
			return path.Base(target)
		})
		line = mdLink.ReplaceAllStringFunc(line, func(link string) string {
			if strings.HasPrefix(link, "!") {
				return ""
			}
			return mdLink.FindStringSubmatch(link)[1]
		})
		total += utf8.RuneCountInString(strings.TrimSpace(line))
	}
	return total
}

func findGymDays(body string, date time.Time, days map[time.Time]struct{}) {
	for line := range strings.Lines(body) {
		task := taskLine.FindStringSubmatch(strings.TrimSpace(line))
		if task == nil || !strings.EqualFold(task[1], "x") {
			continue
		}
		title, _, _ := strings.Cut(task[2], "[completion::")
		if !gymTitle.MatchString(strings.TrimSpace(title)) {
			continue
		}
		doneDate := date
		if stamp := completion.FindStringSubmatch(task[2]); stamp != nil {
			if parsed, err := time.Parse(isoDate, stamp[1]); err == nil {
				doneDate = parsed
			}
		}
		days[doneDate] = struct{}{}
	}
}

func (index noteIndex) links(source, content string) map[string]struct{} {
	found := make(map[string]struct{})
	for _, match := range wikiLink.FindAllStringSubmatch(content, -1) {
		index.addTarget(found, source, match[1], false)
	}
	for _, match := range mdLink.FindAllStringSubmatch(content, -1) {
		index.addTarget(found, source, match[2], true)
	}
	return found
}

func (index noteIndex) addTarget(found map[string]struct{}, source, raw string, relative bool) {
	raw, _, _ = strings.Cut(raw, "|")
	raw, _, _ = strings.Cut(raw, "#")
	target, err := url.PathUnescape(strings.TrimSpace(raw))
	if err != nil || target == "" || strings.Contains(target, ":") || strings.HasPrefix(target, "//") {
		return
	}
	target = strings.ToLower(withoutExtension(target))
	resolved := index.byPath[strings.TrimPrefix(path.Clean(target), "/")]
	if relative || strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../") {
		if local := index.byPath[path.Join(strings.ToLower(path.Dir(source)), target)]; local != "" {
			resolved = local
		}
	}
	if resolved == "" && !strings.Contains(target, "/") {
		if candidates := index.byName[target]; len(candidates) == 1 {
			resolved = candidates[0]
		}
	}
	if resolved != "" && resolved != source {
		found[resolved] = struct{}{}
	}
}

func findMeetings(date time.Time, targets map[string]struct{}) []Meeting {
	var paths []string
	for target := range targets {
		if strings.HasPrefix(target, peopleDir) {
			paths = append(paths, target)
		}
	}
	slices.Sort(paths)
	people := make([]Person, len(paths))
	for i, target := range paths {
		id := sha256.Sum256([]byte(target))
		people[i] = Person{ID: hex.EncodeToString(id[:]), Name: withoutExtension(path.Base(target))}
	}
	var pairs []Meeting
	for i, first := range people {
		for _, second := range people[i+1:] {
			if first.ID > second.ID {
				pairs = append(pairs, Meeting{Date: date, First: second, Second: first})
			} else {
				pairs = append(pairs, Meeting{Date: date, First: first, Second: second})
			}
		}
	}
	return pairs
}
