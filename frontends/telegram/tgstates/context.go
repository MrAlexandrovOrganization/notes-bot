package tgstates

import pb "notes-bot/proto/english/v1"

// UserState represents the current state of a user in the bot workflow.
type UserState string

const (
	StateEnglishMenu       UserState = "english_menu"
	StateEnglishInput      UserState = "english_input"
	StateEnglishPreview    UserState = "english_preview"
	StateEnglishClarify    UserState = "english_clarify"
	StateEnglishReview     UserState = "english_review"
	StateAutomationTask    UserState = "automation_task"
	StateAutomationStatus  UserState = "automation_status"
	StateAutomationText    UserState = "automation_text"
	StateAutomationDelay   UserState = "automation_delay"
	StateAutomationRepeat  UserState = "automation_repeat"
	StateAutomationConfirm UserState = "automation_confirm"

	StateGeoName                    UserState = "geo_name"
	StateGeoPoint                   UserState = "geo_point"
	StateGeoRadius                  UserState = "geo_radius"
	StateGeoPlaceConfirm            UserState = "geo_place_confirm"
	StateGeoTitle                   UserState = "geo_title"
	StateGeoRepeat                  UserState = "geo_repeat"
	StateGeoRuleConfirm             UserState = "geo_rule_confirm"
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

	// Vault browser — навигация по структуре хранилища.
	StateBrowseView UserState = "browse_view"
	StateBrowseFile UserState = "browse_file"
)

// UserContext stores all session data for a user.
type UserContext struct {
	English                   EnglishContext  `json:"english"`
	AutomationDraft           AutomationDraft `json:"automation_draft"`
	GeoDraft                  GeoDraft        `json:"geo_draft"`
	UserID                    int64           `json:"user_id"`
	State                     UserState       `json:"state"`
	ActiveDate                string          `json:"active_date"`
	CalendarMonth             int             `json:"calendar_month"`
	CalendarYear              int             `json:"calendar_year"`
	TaskPage                  int             `json:"task_page"`
	NotePage                  int             `json:"note_page"`
	LastMessageID             int             `json:"last_message_id"`
	ReminderDraft             ReminderDraft   `json:"reminder_draft"`
	PendingPostponeReminderID int64           `json:"pending_postpone_reminder_id"`
	PendingPostponeDate       string          `json:"pending_postpone_date"`
	ReminderCalMonth          int             `json:"reminder_cal_month"`
	ReminderCalYear           int             `json:"reminder_cal_year"`
	ReminderListPage          int             `json:"reminder_list_page"`
	SmartDraft                SmartDraft      `json:"smart_draft"`

	// Find/view/append flow state.
	FindQuery         string      `json:"find_query"`
	FindResults       []SearchHit `json:"find_results"`
	FindResultsPage   int         `json:"find_results_page"`
	ActiveRelpath     string      `json:"active_relpath"`
	ActiveNoteID      int64       `json:"active_note_id"`
	AppendReturnState UserState   `json:"append_return_state,omitempty"`

	// Vault browse state.
	BrowsePath string `json:"browse_path"`
}

type GeoDraft struct {
	RequestID  string  `json:"request_id"`
	PlaceID    string  `json:"place_id"`
	Name       string  `json:"name"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	Radius     float64 `json:"radius"`
	Title      string  `json:"title"`
	Transition string  `json:"transition"`
	Repeat     string  `json:"repeat"`
}

// SearchHit is the minimum view of a search hit kept in user context for pagination.
// Mirrors clients.SearchHit but avoids importing the clients package.
type SearchHit struct {
	NoteID  int64  `json:"note_id"`
	Relpath string `json:"relpath"`
	Name    string `json:"name"`
	Snippet string `json:"snippet"`
}

type AutomationChoice struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
}
type AutomationDraft struct {
	Date         string             `json:"date"`
	RequestID    string             `json:"request_id"`
	Choices      []AutomationChoice `json:"choices"`
	TaskID       string             `json:"task_id"`
	TaskTitle    string             `json:"task_title"`
	TargetStatus int32              `json:"target_status"`
	Text         string             `json:"text"`
	DelaySeconds int32              `json:"delay_seconds"`
	Repeat       bool               `json:"repeat"`
}

// EnglishContext is a resumable UI snapshot; Core rereads canonical Markdown
// and checks revisions before any rating. No learning progress lives in Redis.
type EnglishContext struct {
	NeedsRussianSource bool     `json:"needs_russian_source,omitempty"`
	Transcript         string   `json:"transcript,omitempty"`
	Kind               string   `json:"kind"`
	Input              string   `json:"input"`
	Date               string   `json:"date"`
	OperationID        string   `json:"operation_id"`
	Nonce              string   `json:"nonce"`
	Draft              *pb.Card `json:"draft,omitempty"`
	Question           string   `json:"question"`
	Queue              []string `json:"queue,omitempty"`
	Current            *pb.Card `json:"current,omitempty"`
	Revealed           bool     `json:"revealed"`
	Graded             bool     `json:"graded"`
	Instruction        string   `json:"instruction"`
}
