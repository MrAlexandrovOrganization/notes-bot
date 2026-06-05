package tgstates

import "slices"

// ValidTransitions documents the allowed state transitions for the reminder/notes wizard.
// This is used for documentation and optional runtime validation — the bot does not
// enforce these at runtime, but tests can use CanTransition to detect typos.
var ValidTransitions = map[UserState][]UserState{
	StateIdle: {
		StateWaitingRating,
		StateTasksView,
		StateWaitingNewTask,
		StateCalendarView,
		StateReminderList,
	},
	StateWaitingRating: {StateIdle},
	StateTasksView:     {StateIdle, StateWaitingNewTask},
	StateWaitingNewTask: {StateIdle},
	StateCalendarView:  {StateIdle},

	// Reminder list is the hub for all reminder actions.
	StateReminderList: {
		StateIdle,
		StateReminderCreateTitle,
		StateReminderCreateNL,
	},

	// Creation wizard: title → schedule type → (type-specific step) → task confirm → time → done.
	StateReminderCreateTitle: {StateReminderCreateScheduleType, StateIdle},
	StateReminderCreateScheduleType: {
		StateReminderCreateTaskConfirm, // daily (no extra params)
		StateReminderCreateDay,         // weekly, monthly
		StateReminderCreateInterval,    // custom_days
		StateReminderCreateDate,        // once, yearly
		StateIdle,
	},
	StateReminderCreateDay:         {StateReminderCreateTaskConfirm, StateIdle},
	StateReminderCreateInterval:    {StateReminderCreateTaskConfirm, StateIdle},
	StateReminderCreateDate:        {StateReminderCreateTaskConfirm, StateIdle},
	StateReminderCreateTaskConfirm: {StateReminderCreateTime, StateIdle},
	StateReminderCreateTime:        {StateReminderList, StateIdle},

	// Natural-language path.
	StateReminderCreateNL: {StateReminderList, StateIdle},

	// Postpone flow.
	StateReminderPostponeDate:  {StateReminderPostponeTime, StateIdle},
	StateReminderPostponeInput: {StateIdle},
	StateReminderPostponeTime:  {StateIdle},
}

// CanTransition reports whether transitioning from → to is documented as valid.
// Returns false if from is unknown or to is not listed for that state.
func CanTransition(from, to UserState) bool {
	next, ok := ValidTransitions[from]
	if !ok {
		return false
	}
	return slices.Contains(next, to)
}
