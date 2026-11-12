package taskidentity

import (
	"regexp"
	"strconv"
	"strings"
)

const TaskMarkerPrefix = "<!-- notes-bot:task:"

var taskMarker = regexp.MustCompile(`\s+<!-- notes-bot:task:([0-9a-f-]{36}):([0-9]+):([0-9a-f-]{36}|-) -->$`)

// TaskIdentity is internal metadata; it never becomes the displayed task title.
func TaskIdentity(line string) (id string, revision uint64, eventID string) {
	m := taskMarker.FindStringSubmatch(line)
	if m == nil {
		return "", 0, ""
	}
	revision, _ = strconv.ParseUint(m[2], 10, 64)
	eventID = m[3]
	if eventID == "-" {
		eventID = ""
	}
	return m[1], revision, eventID
}
func StripTaskIdentity(line string) string { return taskMarker.ReplaceAllString(line, "") }
func WithTaskIdentity(line, id string, revision uint64, eventID string) string {
	if eventID == "" {
		eventID = "-"
	}
	return strings.TrimRight(StripTaskIdentity(line), " \t") + " " + TaskMarkerPrefix + id + ":" + strconv.FormatUint(revision, 10) + ":" + eventID + " -->"
}

// Visible removes internal identity stamps from user-facing note content.
func Visible(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = StripTaskIdentity(line)
	}
	return strings.Join(lines, "\n")
}
