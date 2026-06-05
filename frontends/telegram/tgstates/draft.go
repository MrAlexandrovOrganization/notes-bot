package tgstates

import (
	"encoding/json"

	pb "notes-bot/proto/notifications"
)

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

// ToParamsJSON serializes the schedule-specific fields into a JSON string.
// Kept for internal use (e.g. reading params back from state); use ToScheduleParams
// for the gRPC CreateReminder call.
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

// ToScheduleParams converts the draft into the typed proto ScheduleParams
// expected by the notifications CreateReminder RPC.
func (d ReminderDraft) ToScheduleParams(tzOffset int) *pb.ScheduleParams {
	sp := &pb.ScheduleParams{
		Hour:     int32(d.Hour),
		Minute:   int32(d.Minute),
		TzOffset: int32(tzOffset),
	}
	switch d.ScheduleType {
	case "weekly":
		days := make([]int32, len(d.Days))
		for i, day := range d.Days {
			days[i] = int32(day)
		}
		sp.Extra = &pb.ScheduleParams_Weekly{
			Weekly: &pb.ScheduleParams_WeeklyExtra{Days: days},
		}
	case "monthly":
		sp.Extra = &pb.ScheduleParams_Monthly{
			Monthly: &pb.ScheduleParams_MonthlyExtra{DayOfMonth: int32(d.DayOfMonth)},
		}
	case "yearly":
		sp.Extra = &pb.ScheduleParams_Yearly{
			Yearly: &pb.ScheduleParams_YearlyExtra{Month: int32(d.Month), Day: int32(d.Day)},
		}
	case "once":
		sp.Extra = &pb.ScheduleParams_Once{
			Once: &pb.ScheduleParams_OnceExtra{Date: d.Date},
		}
	case "custom_days":
		sp.Extra = &pb.ScheduleParams_CustomDays{
			CustomDays: &pb.ScheduleParams_CustomDaysExtra{IntervalDays: int32(d.IntervalDays)},
		}
	}
	return sp
}
