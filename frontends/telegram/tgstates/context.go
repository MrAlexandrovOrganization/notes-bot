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
	StateReminderPostponeInput      UserState = "reminder_postpone_input"
	StateReminderPostponeTime       UserState = "reminder_postpone_time"
	StateReminderCreateTaskConfirm  UserState = "reminder_create_task_confirm"
	StateReminderCreateNL           UserState = "reminder_create_nl"

	// Smart router: одно сообщение → LLM понимает intent (note/task/reminder) → подтверждение.
	StateSmartInput   UserState = "smart_input"
	StateSmartConfirm UserState = "smart_confirm"

	// Find / view / append flow over arbitrary vault notes.
	StateFindNoteInput     UserState = "find_note_input"
	StateFindNoteResults   UserState = "find_note_results"
	StateViewNote          UserState = "view_note"
	StateAppendToNoteInput UserState = "append_to_note_input"

	// Semantic Q&A — vector search + LLM RAG.
	StateAskQuestion UserState = "ask_question"
)

// UserContext stores all session data for a user.
type UserContext struct {
	UserID                    int64         `json:"user_id"`
	State                     UserState     `json:"state"`
	ActiveDate                string        `json:"active_date"`
	CalendarMonth             int           `json:"calendar_month"`
	CalendarYear              int           `json:"calendar_year"`
	TaskPage                  int           `json:"task_page"`
	NotePage                  int           `json:"note_page"`
	LastMessageID             int           `json:"last_message_id"`
	ReminderDraft             ReminderDraft `json:"reminder_draft"`
	PendingPostponeReminderID int64         `json:"pending_postpone_reminder_id"`
	PendingPostponeDate       string        `json:"pending_postpone_date"`
	ReminderCalMonth          int           `json:"reminder_cal_month"`
	ReminderCalYear           int           `json:"reminder_cal_year"`
	ReminderListPage          int           `json:"reminder_list_page"`
	SmartDraft                SmartDraft    `json:"smart_draft"`

	// Find/view/append flow state.
	FindQuery       string       `json:"find_query"`
	FindResults     []SearchHit  `json:"find_results"`
	FindResultsPage int          `json:"find_results_page"`
	ActiveRelpath   string       `json:"active_relpath"`
}

// SearchHit is the minimum view of a search hit kept in user context for pagination.
// Mirrors clients.SearchHit but avoids importing the clients package.
type SearchHit struct {
	NoteID  int64  `json:"note_id"`
	Relpath string `json:"relpath"`
	Name    string `json:"name"`
	Snippet string `json:"snippet"`
}
