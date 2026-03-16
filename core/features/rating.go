package features

import (
	"strconv"
	"strings"

	"go.uber.org/zap"
)

func UpdateRatingImpl(content string, rating int) (string, bool) {
	parts := strings.Split(content, "---")
	if len(parts) < 3 {
		logger.Error("invalid frontmatter format in note")
		return "", false
	}

	frontmatter := parts[1]
	lines := strings.Split(frontmatter, "\n")

	ratingFound := false
	updatedLines := make([]string, 0, len(lines))

	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "Оценка:") {
			ratingFound = true
			updatedLines = append(updatedLines, "Оценка: "+strconv.Itoa(rating))
		} else {
			updatedLines = append(updatedLines, line)
		}
	}

	if !ratingFound {
		if len(updatedLines) > 0 && updatedLines[len(updatedLines)-1] == "" {
			updatedLines = append(updatedLines[:len(updatedLines)-1], "Оценка: "+strconv.Itoa(rating), "")
		} else {
			updatedLines = append(updatedLines, "Оценка: "+strconv.Itoa(rating))
		}
	}

	parts[1] = strings.Join(updatedLines, "\n")
	return strings.Join(parts, "---"), true
}

func GetRatingImpl(content string) *int {
	parts := strings.Split(content, "---")
	if len(parts) < 3 {
		logger.Error("invalid frontmatter format in note")
		return nil
	}

	frontmatter := parts[1]
	lines := strings.SplitSeq(frontmatter, "\n")

	for line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "Оценка:") {
			continue
		}
		ratingStr := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
		rating, err := strconv.Atoi(ratingStr)
		if err != nil {
			logger.Warn("invalid rating value", zap.String("value", ratingStr))
			return nil
		}
		return &rating
	}

	return nil
}
