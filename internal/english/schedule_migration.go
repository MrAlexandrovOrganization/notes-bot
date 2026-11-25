package english

// Upgrade the JSON state from one schedule per selection. An existing queue
// keeps the side used to generate its tokens, including offline presentations.
func (s *session) upgradeSchedule() {
	if s.SharedSchedule {
		return
	}
	s.Prompts = savedPrompts(s.Queue, func(id string) directionDue {
		p := s.Progress[id]
		return directionDue{p.side("en").Due, p.side("ru").Due}
	})
	waits := make(map[string]directionDue, len(s.Progress))
	for id, p := range s.Progress {
		waits[id] = directionDue{p.side("en").Due - s.Round - 1, p.side("ru").Due - s.Round - 1}
	}
	for key, round := range s.Dictionaries {
		round.Prompts = savedPrompts(round.Queue, func(id string) directionDue { return round.Due[id] })
		for id, due := range round.Due {
			wait := waits[id]
			waits[id] = directionDue{
				English: max(wait.English, due.English-round.Round-1),
				Russian: max(wait.Russian, due.Russian-round.Round-1),
			}
		}
		round.Due = nil
		s.Dictionaries[key] = round
	}
	for id, wait := range waits {
		p := s.Progress[id]
		p.English = migrateDirection(p.side("en"), wait.English)
		p.Russian = migrateDirection(p.side("ru"), wait.Russian)
		p.Due, p.Level = 0, 0
		s.Progress[id] = p
	}
	s.SharedSchedule = true
}

func savedPrompts(queue []string, dueFor func(string) directionDue) map[string]string {
	prompts := make(map[string]string)
	for _, id := range queue {
		due := dueFor(id)
		if prompt := earlierPrompt(due.English, due.Russian); prompt != "" {
			prompts[id] = prompt
		}
	}
	return prompts
}

func migrateDirection(side directionProgress, wait int64) *directionProgress {
	// With several legacy schedules only the last grade is shared. Keep the
	// longest remaining wait consistent with what that grade could have assigned.
	maximum := int64(easyIntervals[len(easyIntervals)-1].max)
	switch side.Grade {
	case "miss":
		maximum = 0
	case "hard":
		maximum = 1
	case "easy":
		level := min(max(1, side.Level), int32(len(easyIntervals)))
		maximum = int64(easyIntervals[level-1].max)
	}
	side.Due = 0
	if remaining := min(max(0, wait), maximum); remaining > 0 {
		side.Next = remaining + 1
	}
	if side == (directionProgress{}) {
		return nil
	}
	return &side
}
