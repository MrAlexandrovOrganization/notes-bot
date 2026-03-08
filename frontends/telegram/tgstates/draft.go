package tgstates

import "encoding/json"

// ReminderDraft holds the in-progress state of the reminder creation wizard.
// It replaces the previous untyped map[string]any, giving compile-time safety.
type ReminderDraft struct {
	Title        string `json:"title"`
	ScheduleType string `json:"schedule_type"`
	CreateTask   bool   `json:"create_task"`

	// Time fields (all schedule types except "once" use hour/minute).
	Hour   int `json:"hour"`
	Minute int `json:"minute"`

	// weekly: days of week (0=Mon … 6=Sun).
	Days []int `json:"days,omitempty"`

	// monthly: day of month (1–31).
	DayOfMonth int `json:"day_of_month"`

	// yearly: calendar month (1–12) and day (1–31).
	Month int `json:"month"`
	Day   int `json:"day"`

	// once: ISO date string "YYYY-MM-DD".
	Date string `json:"date,omitempty"`

	// custom_days: repeat every N days.
	IntervalDays int `json:"interval_days"`
}

// scheduleParams is the JSON shape expected by the notifications service.
type scheduleParams struct {
	Hour         int    `json:"hour"`
	Minute       int    `json:"minute"`
	Days         []int  `json:"days,omitempty"`
	DayOfMonth   int    `json:"day_of_month"`
	Month        int    `json:"month"`
	Day          int    `json:"day"`
	Date         string `json:"date,omitempty"`
	IntervalDays int    `json:"interval_days"`
	TzOffset     int    `json:"tz_offset"`
}

// ToParamsJSON serializes the schedule-specific fields into the JSON expected
// by the notifications CreateReminder RPC.
func (d ReminderDraft) ToParamsJSON(tzOffset int) (string, error) {
	p := scheduleParams{
		Hour:         d.Hour,
		Minute:       d.Minute,
		Days:         d.Days,
		DayOfMonth:   d.DayOfMonth,
		Month:        d.Month,
		Day:          d.Day,
		Date:         d.Date,
		IntervalDays: d.IntervalDays,
		TzOffset:     tzOffset,
	}
	data, err := json.Marshal(p)
	return string(data), err
}
