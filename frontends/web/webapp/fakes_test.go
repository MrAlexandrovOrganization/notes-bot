package webapp

import (
	"context"

	"notes-bot/frontends/telegram/clients"
	pb "notes-bot/proto/notifications"
)

// fakeCore is a configurable in-memory stand-in for clients.CoreService.
type fakeCore struct {
	todayDate     string
	existingDates []string
	notes         map[string]string
	ratings       map[string]int
	hasRating     map[string]bool
	tasks         map[string][]*clients.Task
	dirs          map[string][]clients.DirEntry
	err           error
	appendedText  string
	appendedDate  string
	appendedPath  string
	addedTaskText string
	toggledIndex  int
	updatedRating int
	ensuredNote   string
}

func newFakeCore() *fakeCore {
	return &fakeCore{
		todayDate: "01-Jan-2026",
		notes:     map[string]string{},
		ratings:   map[string]int{},
		hasRating: map[string]bool{},
		tasks:     map[string][]*clients.Task{},
		dirs:      map[string][]clients.DirEntry{},
	}
}

func (f *fakeCore) GetTodayDate(ctx context.Context) (string, error) { return f.todayDate, f.err }
func (f *fakeCore) GetExistingDates(ctx context.Context) ([]string, error) {
	return f.existingDates, f.err
}
func (f *fakeCore) EnsureNote(ctx context.Context, date string) (bool, error) {
	f.ensuredNote = date
	return true, f.err
}
func (f *fakeCore) GetNote(ctx context.Context, date string) (string, error) {
	return f.notes[date], f.err
}
func (f *fakeCore) GetRating(ctx context.Context, date string) (int, bool, error) {
	return f.ratings[date], f.hasRating[date], f.err
}
func (f *fakeCore) UpdateRating(ctx context.Context, date string, rating int) (bool, error) {
	f.updatedRating = rating
	f.ratings[date] = rating
	f.hasRating[date] = true
	return true, f.err
}
func (f *fakeCore) GetTasks(ctx context.Context, date string) ([]*clients.Task, error) {
	return f.tasks[date], f.err
}
func (f *fakeCore) ToggleTask(ctx context.Context, date string, taskIndex int) (bool, error) {
	f.toggledIndex = taskIndex
	return true, f.err
}
func (f *fakeCore) AddTask(ctx context.Context, date, taskText string) (bool, error) {
	f.addedTaskText = taskText
	return true, f.err
}
func (f *fakeCore) AppendToNote(ctx context.Context, date, text string) (bool, error) {
	f.appendedDate = date
	f.appendedText = text
	return true, f.err
}
func (f *fakeCore) AppendToNoteByPath(ctx context.Context, relpath, text string) (bool, error) {
	f.appendedPath = relpath
	f.appendedText = text
	return true, f.err
}
func (f *fakeCore) ListDirectory(ctx context.Context, relpath string) ([]clients.DirEntry, error) {
	return f.dirs[relpath], f.err
}
func (f *fakeCore) GetNoteByPath(ctx context.Context, relpath string) (string, error) {
	return f.notes[relpath], f.err
}

var _ clients.CoreService = (*fakeCore)(nil)

// fakeNotifications is a configurable stand-in for clients.NotificationsService.
type fakeNotifications struct {
	reminders     []*clients.ReminderInfo
	createErr     error
	created       *clients.ReminderInfo
	deletedID     int64
	postponedMins int32
}

func (f *fakeNotifications) CreateReminder(ctx context.Context, userID int64, title, scheduleType string, scheduleParams *pb.ScheduleParams, createTask bool) (*clients.ReminderInfo, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.created, nil
}
func (f *fakeNotifications) ListReminders(ctx context.Context, userID int64) ([]*clients.ReminderInfo, error) {
	return f.reminders, nil
}
func (f *fakeNotifications) DeleteReminder(ctx context.Context, reminderID, userID int64) (bool, error) {
	f.deletedID = reminderID
	return true, nil
}
func (f *fakeNotifications) PostponeReminder(ctx context.Context, reminderID, userID int64, postponeMinutes int32) (*clients.ReminderInfo, error) {
	f.postponedMins = postponeMinutes
	return f.created, nil
}

var _ clients.NotificationsService = (*fakeNotifications)(nil)

// fakeSearch is a configurable stand-in for clients.SearchService.
type fakeSearch struct {
	byName    []*clients.SearchHit
	byContent []*clients.SearchHit
	semantic  []*clients.SearchHit
	note      *clients.SearchNote
	err       error
}

func (f *fakeSearch) SearchByName(ctx context.Context, query string, limit int) ([]*clients.SearchHit, error) {
	return f.byName, f.err
}
func (f *fakeSearch) FindNotes(ctx context.Context, query string, limit int, options clients.SearchOptions) ([]*clients.SearchHit, error) {
	return append(append([]*clients.SearchHit(nil), f.byName...), f.byContent...), f.err
}
func (f *fakeSearch) SearchByContent(ctx context.Context, query string, limit int) ([]*clients.SearchHit, error) {
	return f.byContent, f.err
}
func (f *fakeSearch) SearchSemantic(ctx context.Context, query string, limit int) ([]*clients.SearchHit, error) {
	return f.semantic, f.err
}
func (f *fakeSearch) SearchHybrid(ctx context.Context, query string, limit int, options clients.SearchOptions) ([]*clients.SearchHit, error) {
	return f.semantic, f.err
}
func (f *fakeSearch) SearchProfiles(ctx context.Context, query string, limit int, options clients.SearchOptions) ([]*clients.SearchHit, error) {
	return f.semantic, f.err
}
func (f *fakeSearch) AskNotes(ctx context.Context, question, currentDateTime string, options clients.SearchOptions) (*clients.AskNotesResult, error) {
	return &clients.AskNotesResult{Answer: "answer", Evidence: f.semantic}, f.err
}
func (f *fakeSearch) GetNoteByID(ctx context.Context, id int64) (*clients.SearchNote, error) {
	return f.note, f.err
}

var _ clients.SearchService = (*fakeSearch)(nil)

// fakeLLM is a configurable stand-in for clients.LLMService.
type fakeLLM struct {
	reminder *clients.LLMReminderResult
	intent   *clients.LLMIntentResult
	answer   string
	err      error
}

func (f *fakeLLM) ParseReminder(ctx context.Context, text, currentDateTime, today, tomorrow, dayAfter string) (*clients.LLMReminderResult, error) {
	return f.reminder, f.err
}
func (f *fakeLLM) ClassifyIntent(ctx context.Context, text, currentDateTime string) (*clients.LLMIntentResult, error) {
	return f.intent, f.err
}
func (f *fakeLLM) Ask(ctx context.Context, system, user string, numPredict int) (string, error) {
	return f.answer, f.err
}

var _ clients.LLMService = (*fakeLLM)(nil)
