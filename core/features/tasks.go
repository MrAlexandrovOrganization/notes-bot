package features

import (
	"context"
	"fmt"
	"notes-bot/internal/telemetry"
	"strings"
	"time"

	"go.uber.org/zap"
)

type TaskState int

const (
	TaskStatePending TaskState = iota
	TaskStateCompleted
	TaskStateRejected
)

type Task struct {
	Text       string
	State      TaskState
	Index      int
	LineNumber int
}

func (t Task) Completed() bool {
	return t.State == TaskStateCompleted
}

func (t Task) Rejected() bool {
	return t.State == TaskStateRejected
}

func (t Task) Pending() bool {
	return t.State == TaskStatePending
}

func ParseTasks(ctx context.Context, content string) []Task {
	_, span := telemetry.StartSpan(ctx)
	defer span.End()

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
		state := TaskStatePending
		if after, ok := strings.CutPrefix(stripped, "- [?]"); ok {
			taskText = strings.TrimSpace(after)
			state = TaskStatePending
		} else if after, ok := strings.CutPrefix(stripped, "- [ ]"); ok {
			taskText = strings.TrimSpace(after)
			state = TaskStatePending
		} else if after, ok := strings.CutPrefix(stripped, "- [x]"); ok {
			taskText = strings.TrimSpace(after)
			state = TaskStateCompleted
		} else if after, ok := strings.CutPrefix(stripped, "- [X]"); ok {
			taskText = strings.TrimSpace(after)
			state = TaskStateCompleted
		} else if after, ok := strings.CutPrefix(stripped, "- [-]"); ok {
			taskText = strings.TrimSpace(after)
			state = TaskStateRejected
		} else {
			continue
		}
		if idx := strings.Index(taskText, "[completion::"); idx != -1 {
			taskText = strings.TrimSpace(taskText[:idx])
		}

		tasks = append(tasks, Task{
			Text:       taskText,
			State:      state,
			Index:      taskIndex,
			LineNumber: lineOffset + i,
		})

		taskIndex++
	}

	logger.Info("parsed tasks from content", zap.Int("amount", len(tasks)))

	return tasks
}

func SetTaskStatusContent(ctx context.Context, content string, taskIndex int, newState TaskState) (string, error) {
	_, span := telemetry.StartSpan(ctx)
	defer span.End()

	tasks := ParseTasks(ctx, content)

	if taskIndex < 0 || taskIndex >= len(tasks) {
		return "", fmt.Errorf("invalid task index: %d (total tasks: %d)", taskIndex, len(tasks))
	}

	lineIdx := tasks[taskIndex].LineNumber - 1
	lines := strings.Split(content, "\n")

	if lineIdx < 0 || lineIdx >= len(lines) {
		return "", fmt.Errorf("invalid line number: %d", lineIdx+1)
	}

	line := lines[lineIdx]

	var newLine string
	switch newState {
	case TaskStatePending:
		if strings.Contains(line, "- [x]") || strings.Contains(line, "- [X]") {
			newLine = strings.Replace(line, "- [x]", "- [?]", 1)
			newLine = strings.Replace(newLine, "- [X]", "- [?]", 1)
		} else if strings.Contains(line, "- [-]") {
			newLine = strings.Replace(line, "- [-]", "- [?]", 1)
		} else if strings.Contains(line, "- [ ]") {
			newLine = strings.Replace(line, "- [ ]", "- [?]", 1)
		} else {
			return "", fmt.Errorf("line %d does not contain a valid task", lineIdx+1)
		}
		if idx := strings.Index(newLine, "[completion::"); idx != -1 {
			end := strings.Index(newLine[idx:], "]") + idx + 1
			newLine = strings.TrimRight(newLine[:idx], " ") + newLine[end:]
		}
	case TaskStateCompleted:
		if strings.Contains(line, "- [?]") || strings.Contains(line, "- [ ]") {
			if idx := strings.Index(line, "[completion::"); idx != -1 {
				end := strings.Index(line[idx:], "]") + idx + 1
				line = strings.TrimRight(line[:idx], " ") + line[end:]
			}
			newLine = strings.Replace(line, "- [?]", "- [x]", 1)
			newLine = strings.Replace(newLine, "- [ ]", "- [x]", 1)
		} else if strings.Contains(line, "- [-]") {
			newLine = strings.Replace(line, "- [-]", "- [x]", 1)
		} else if strings.Contains(line, "- [x]") || strings.Contains(line, "- [X]") {
			return "", fmt.Errorf("task already completed")
		} else {
			return "", fmt.Errorf("line %d does not contain a valid task", lineIdx+1)
		}
		newLine = strings.TrimRight(newLine, " ") + fmt.Sprintf("  [completion:: %s]", time.Now().Format("2006-01-02"))
	case TaskStateRejected:
		if strings.Contains(line, "- [?]") || strings.Contains(line, "- [ ]") {
			newLine = strings.Replace(line, "- [?]", "- [-]", 1)
			newLine = strings.Replace(newLine, "- [ ]", "- [-]", 1)
		} else if strings.Contains(line, "- [x]") || strings.Contains(line, "- [X]") {
			newLine = strings.Replace(line, "- [x]", "- [-]", 1)
			newLine = strings.Replace(newLine, "- [X]", "- [-]", 1)
		} else if strings.Contains(line, "- [-]") {
			return "", fmt.Errorf("task already rejected")
		} else {
			return "", fmt.Errorf("line %d does not contain a valid task", lineIdx+1)
		}
		if idx := strings.Index(newLine, "[completion::"); idx != -1 {
			end := strings.Index(newLine[idx:], "]") + idx + 1
			newLine = strings.TrimRight(newLine[:idx], " ") + newLine[end:]
		}
	default:
		return "", fmt.Errorf("invalid task state: %d", newState)
	}

	lines[lineIdx] = newLine
	return strings.Join(lines, "\n"), nil
}

func AddTaskContent(ctx context.Context, content string, taskText string) (string, error) {
	_, span := telemetry.StartSpan(ctx)
	defer span.End()

	parts := strings.Split(content, "---")
	if len(parts) < 4 {
		return "", fmt.Errorf("invalid format: need at least 3 '---' delimiters")
	}

	lines := strings.Split(parts[2], "\n")

	lastTaskIdx := -1
	for i, line := range lines {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "- [?]") || strings.HasPrefix(stripped, "- [ ]") || strings.HasPrefix(stripped, "- [x]") || strings.HasPrefix(stripped, "- [X]") || strings.HasPrefix(stripped, "- [-]") {
			lastTaskIdx = i
		}
	}

	newTask := "- [?] " + taskText

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
