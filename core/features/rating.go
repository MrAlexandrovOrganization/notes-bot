package features

import (
	"context"
	"notes-bot/internal/telemetry"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

func UpdateRatingImpl(ctx context.Context, content string, rating int) (string, bool) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	parts := strings.Split(content, "---")
	if len(parts) < 3 {
		logger.Error("invalid frontmatter format in note")
		return "", false
	}

	// Handle both 2-delimiter and 3-delimiter (Obsidian) formats
	// parts[0] = "" (before first ---)
	// parts[1] = frontmatter
	// parts[2] = content (may contain trailing --- in Obsidian format)
	frontmatter := parts[1]
	var contentSuffix string

	// If there's a third delimiter, frontmatter might contain part of content
	if len(parts) >= 4 {
		// In Obsidian format: ---frontmatter---content---
		// parts[1] = "frontmatter\n" and parts[2] = "content\n---"
		// We need to extract just the frontmatter part and preserve the rest
		frontmatterLines := strings.Split(frontmatter, "\n")
		var cleanFrontmatter []string
		var contentStart []string
		inFrontmatter := true

		for _, line := range frontmatterLines {
			// Stop if we hit what looks like content (starts with - [ ] or - [x])
			if inFrontmatter && strings.HasPrefix(strings.TrimSpace(line), "- [") {
				inFrontmatter = false
			}
			if inFrontmatter {
				cleanFrontmatter = append(cleanFrontmatter, line)
			} else {
				contentStart = append(contentStart, line)
			}
		}
		frontmatter = strings.Join(cleanFrontmatter, "\n")
		// Reconstruct the content part that was in frontmatter
		if len(contentStart) > 0 {
			contentSuffix = strings.Join(contentStart, "\n")
			if !strings.HasPrefix(contentSuffix, "\n") {
				contentSuffix = "\n" + contentSuffix
			}
		}
	}

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

	parts[1] = strings.Join(updatedLines, "\n") + contentSuffix
	return strings.Join(parts, "---"), true
}

func GetRatingImpl(ctx context.Context, content string) *int {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	parts := strings.Split(content, "---")
	if len(parts) < 3 {
		logger.Error("invalid frontmatter format in note")
		return nil
	}

	// Handle both 2-delimiter and 3-delimiter (Obsidian) formats
	// parts[0] = "" (before first ---)
	// parts[1] = frontmatter
	// parts[2] = content (may contain trailing --- in Obsidian format)
	frontmatter := parts[1]
	// If there's a third delimiter, frontmatter might contain part of content
	// Find the actual end of frontmatter by looking for the second ---
	if len(parts) >= 4 {
		// In Obsidian format: ---frontmatter---content---
		// parts[1] = "frontmatter\n" and parts[2] = "content\n---"
		// We need to extract just the frontmatter part
		frontmatterLines := strings.Split(frontmatter, "\n")
		var cleanLines []string
		for _, line := range frontmatterLines {
			// Stop if we hit what looks like content (starts with - [ ] or - [x])
			if strings.HasPrefix(strings.TrimSpace(line), "- [") {
				break
			}
			cleanLines = append(cleanLines, line)
		}
		frontmatter = strings.Join(cleanLines, "\n")
	}

	lines := strings.SplitSeq(frontmatter, "\n")

	for line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "Оценка:") {
			continue
		}
		ratingStr := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
		// Empty rating is normal - note just doesn't have a rating yet
		if ratingStr == "" {
			return nil
		}
		rating, err := strconv.Atoi(ratingStr)
		if err != nil {
			logger.Warn("invalid rating value", zap.String("value", ratingStr))
			return nil
		}
		return &rating
	}

	return nil
}
