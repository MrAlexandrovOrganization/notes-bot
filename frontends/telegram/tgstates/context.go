package tgstates

// UserState represents the current state of a user in the bot workflow.
type UserState string

const (
	StateIdle                       UserState = "idle"
	StateWaitingRating              UserState = "waiting_rating"
	StateTasksView                  UserState = "tasks_view"
	StateWaitingNewTask             UserState = "waiting_new_task"
	StateCalendarView               UserState = "calendar_view"
	StateReminderList               UserState = "reminder_list"
	StateReminderCreateTitle        UserState = "reminder_create_title"
	StateReminderCreateScheduleType UserState = "reminder_create_schedule_type"
	StateReminderCreateTime         UserState = "reminder_create_time"
	StateReminderCreateDay          UserState = "reminder_create_day"
	StateReminderCreateInterval     UserState = "reminder_create_interval"
	StateReminderCreateDate         UserState = "reminder_create_date"
	StateReminderPostponeDate       UserState = "reminder_postpone_date"
	StateReminderCreateTaskConfirm  UserState = "reminder_create_task_confirm"
	StateReminderCreateNL           UserState = "reminder_create_nl"
)

// UserContext stores all session data for a user.
type UserContext struct {
	UserID                    int64         `json:"user_id"`
	State                     UserState     `json:"state"`
	ActiveDate                string        `json:"active_date"`
	CalendarMonth             int           `json:"calendar_month"`
	CalendarYear              int           `json:"calendar_year"`
	TaskPage                  int           `json:"task_page"`
	LastMessageID             int           `json:"last_message_id"`
	ReminderDraft             ReminderDraft `json:"reminder_draft"`
	PendingPostponeReminderID int64         `json:"pending_postpone_reminder_id"`
	ReminderCalMonth          int           `json:"reminder_cal_month"`
	ReminderCalYear           int           `json:"reminder_cal_year"`
	ReminderListPage          int           `json:"reminder_list_page"`
}
