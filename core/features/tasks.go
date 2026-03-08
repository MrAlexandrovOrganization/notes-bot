package features

import (
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

type Task struct {
	Text       string
	Completed  bool
	Index      int
	LineNumber int
}

func ParseTasks(content string) []Task {
	logger.Debug("ParseTasks")

	tasks := []Task{}

	parts := strings.Split(content, "---")
	if len(parts) < 4 {
		logger.Warn("invalid format, need at least 3 '---' delimiters for tasks section")
		return tasks
	}

	tasksSection := parts[2]
	lines := strings.Split(tasksSection, "\n")

	taskIndex := 0
	lineOffset := strings.Count(strings.Join(parts[:2], "---")+"---", "\n") + 1

	for i, line := range lines {
		stripped := strings.TrimSpace(line)

		taskText := ""
		completed := false
		if after, ok := strings.CutPrefix(stripped, "- [ ]"); ok {
			taskText = strings.TrimSpace(after)
		} else if after, ok := strings.CutPrefix(stripped, "- [x]"); ok {
			taskText = strings.TrimSpace(after)
			completed = true
		} else if after, ok := strings.CutPrefix(stripped, "- [X]"); ok {
			taskText = strings.TrimSpace(after)
			completed = true
		} else {
			continue
		}
		if idx := strings.Index(taskText, "[completion::"); idx != -1 {
			taskText = strings.TrimSpace(taskText[:idx])
		}

		tasks = append(tasks, Task{
			Text:       taskText,
			Completed:  completed,
			Index:      taskIndex,
			LineNumber: lineOffset + i,
		})

		taskIndex++
	}

	logger.Info("parsed tasks from content", zap.Int("amount", len(tasks)))

	return tasks
}

func ToggleTaskContent(content string, taskIndex int) (string, error) {
	tasks := ParseTasks(content)

	if taskIndex < 0 || taskIndex >= len(tasks) {
		return "", fmt.Errorf("invalid task index: %d (total tasks: %d)", taskIndex, len(tasks))
	}

	lineIdx := tasks[taskIndex].LineNumber - 1
	lines := strings.Split(content, "\n")

	if lineIdx < 0 || lineIdx >= len(lines) {
		return "", fmt.Errorf("invalid line number: %d", lineIdx+1)
	}

	line := lines[lineIdx]

	if strings.Contains(line, "- [ ]") {
		if idx := strings.Index(line, "[completion::"); idx != -1 {
			end := strings.Index(line[idx:], "]") + idx + 1
			line = strings.TrimRight(line[:idx], " ") + line[end:]
		}
		line = strings.Replace(line, "- [ ]", "- [x]", 1)
		line = strings.TrimRight(line, " ") + fmt.Sprintf("  [completion:: %s]", time.Now().Format("2006-01-02"))
	} else if strings.Contains(line, "- [x]") || strings.Contains(line, "- [X]") {
		line = strings.Replace(line, "- [x]", "- [ ]", 1)
		line = strings.Replace(line, "- [X]", "- [ ]", 1)
		if idx := strings.Index(line, "[completion::"); idx != -1 {
			end := strings.Index(line[idx:], "]") + idx + 1
			line = strings.TrimRight(line[:idx], " ") + line[end:]
		}
	} else {
		return "", fmt.Errorf("line %d does not contain a valid task", lineIdx+1)
	}

	lines[lineIdx] = line
	return strings.Join(lines, "\n"), nil
}

func AddTaskContent(content string, taskText string) (string, error) {
	parts := strings.Split(content, "---")
	if len(parts) < 4 {
		return "", fmt.Errorf("invalid format: need at least 3 '---' delimiters")
	}

	lines := strings.Split(parts[2], "\n")

	lastTaskIdx := -1
	for i, line := range lines {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "- [ ]") || strings.HasPrefix(stripped, "- [x]") || strings.HasPrefix(stripped, "- [X]") {
			lastTaskIdx = i
		}
	}

	newTask := "- [ ] " + taskText

	if lastTaskIdx >= 0 {
		lines = append(lines[:lastTaskIdx+1], append([]string{newTask}, lines[lastTaskIdx+1:]...)...)
	} else {
		if len(lines) > 0 && lines[0] == "" {
			lines = append([]string{lines[0], newTask}, lines[1:]...)
		} else {
			lines = append([]string{newTask}, lines...)
		}
	}

	parts[2] = strings.Join(lines, "\n")
	return strings.Join(parts, "---"), nil
}
