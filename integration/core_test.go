package integration_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"notes_bot/core"
	pb "notes_bot/proto/notes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const testDate = "01-Mar-2026"

var (
	client  pb.NotesServiceClient
	tempDir string
)

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	var err error
	tempDir, err = os.MkdirTemp("", "notes-integration-*")
	if err != nil {
		panic(fmt.Sprintf("failed to create temp dir: %v", err))
	}
	defer os.RemoveAll(tempDir)

	dailyDir := filepath.Join(tempDir, "Daily")
	templatesDir := filepath.Join(tempDir, "Templates")
	os.MkdirAll(dailyDir, 0755)
	os.MkdirAll(templatesDir, 0755)

	templateContent := "---\n" +
		"date: \"[[{{date:DD-MMM-YYYY}}]]\"\n" +
		"title: \"[[{{date:DD-MMM-YYYY}}]]\"\n" +
		"tags:\n" +
		"  - daily\n" +
		"Оценка:\n" +
		"---\n" +
		"---\n\n"
	os.WriteFile(filepath.Join(templatesDir, "Daily.md"), []byte(templateContent), 0644)

	// Устанавливаем env до первого вызова GetConfig (sync.Once)
	os.Setenv("NOTES_DIR", tempDir)
	core.GetConfig(context.Background())

	// Запускаем gRPC-сервер на случайном порту
	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		panic(fmt.Sprintf("failed to listen: %v", err))
	}
	port := lis.Addr().(*net.TCPAddr).Port

	grpcServer := grpc.NewServer()
	pb.RegisterNotesServiceServer(grpcServer, core.NewDefaultNotesServer())
	go grpcServer.Serve(lis)
	defer grpcServer.GracefulStop()

	conn, err := grpc.NewClient(
		fmt.Sprintf("localhost:%d", port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		panic(fmt.Sprintf("failed to connect: %v", err))
	}
	defer conn.Close()

	client = pb.NewNotesServiceClient(conn)
	return m.Run()
}

// --- GetTodayDate ---

func TestGetTodayDate_ReturnsDate(t *testing.T) {
	resp, err := client.GetTodayDate(context.Background(), &pb.Empty{})
	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`^\d{2}-[A-Z][a-z]{2}-\d{4}$`), resp.Date)
}

// --- GetExistingDates ---

func TestGetExistingDates_InitiallyEmpty(t *testing.T) {
	resp, err := client.GetExistingDates(context.Background(), &pb.Empty{})
	require.NoError(t, err)
	assert.Empty(t, resp.Dates)
}

// --- EnsureNote ---

func TestEnsureNote_CreatesFile(t *testing.T) {
	resp, err := client.EnsureNote(context.Background(), &pb.DateRequest{Date: testDate})
	require.NoError(t, err)
	assert.True(t, resp.Success)

	_, statErr := os.Stat(filepath.Join(tempDir, "Daily", testDate+".md"))
	assert.NoError(t, statErr)
}

func TestEnsureNote_IdempotentOnExistingFile(t *testing.T) {
	resp, err := client.EnsureNote(context.Background(), &pb.DateRequest{Date: testDate})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

// --- GetExistingDates (после создания заметки) ---

func TestGetExistingDates_AfterEnsureNote(t *testing.T) {
	resp, err := client.GetExistingDates(context.Background(), &pb.Empty{})
	require.NoError(t, err)
	assert.Contains(t, resp.Dates, testDate)
}

// --- GetNote ---

func TestGetNote_ReturnsContent(t *testing.T) {
	resp, err := client.GetNote(context.Background(), &pb.DateRequest{Date: testDate})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Content)
	assert.Contains(t, resp.Content, testDate)
}

func TestGetNote_NotFoundForMissingDate(t *testing.T) {
	_, err := client.GetNote(context.Background(), &pb.DateRequest{Date: "01-Jan-1999"})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

// --- GetRating ---

func TestGetRating_NoRatingAfterTemplateCreate(t *testing.T) {
	resp, err := client.GetRating(context.Background(), &pb.DateRequest{Date: testDate})
	require.NoError(t, err)
	assert.False(t, resp.HasRating)
}

func TestGetRating_NoRatingForMissingDate(t *testing.T) {
	resp, err := client.GetRating(context.Background(), &pb.DateRequest{Date: "01-Jan-1999"})
	require.NoError(t, err)
	assert.False(t, resp.HasRating)
}

// --- UpdateRating + GetRating ---

func TestUpdateRating_SetsRating(t *testing.T) {
	resp, err := client.UpdateRating(context.Background(), &pb.UpdateRatingRequest{
		Date:   testDate,
		Rating: 8,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestGetRating_AfterUpdate(t *testing.T) {
	resp, err := client.GetRating(context.Background(), &pb.DateRequest{Date: testDate})
	require.NoError(t, err)
	assert.True(t, resp.HasRating)
	assert.Equal(t, int32(8), resp.Rating)
}

func TestUpdateRating_ZeroIsValidRating(t *testing.T) {
	_, err := client.UpdateRating(context.Background(), &pb.UpdateRatingRequest{
		Date:   testDate,
		Rating: 0,
	})
	require.NoError(t, err)

	resp, err := client.GetRating(context.Background(), &pb.DateRequest{Date: testDate})
	require.NoError(t, err)
	assert.True(t, resp.HasRating)
	assert.Equal(t, int32(0), resp.Rating)

	// Возвращаем 8 обратно для следующих тестов
	client.UpdateRating(context.Background(), &pb.UpdateRatingRequest{Date: testDate, Rating: 8})
}

// --- GetTasks ---

func TestGetTasks_EmptyAfterTemplateCreate(t *testing.T) {
	resp, err := client.GetTasks(context.Background(), &pb.DateRequest{Date: testDate})
	require.NoError(t, err)
	assert.Empty(t, resp.Tasks)
}

func TestGetTasks_EmptyForMissingDate(t *testing.T) {
	resp, err := client.GetTasks(context.Background(), &pb.DateRequest{Date: "01-Jan-1999"})
	require.NoError(t, err)
	assert.Empty(t, resp.Tasks)
}

// --- AddTask + GetTasks ---

func TestAddTask_AddsTask(t *testing.T) {
	resp, err := client.AddTask(context.Background(), &pb.AddTaskRequest{
		Date:     testDate,
		TaskText: "Buy groceries",
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestGetTasks_AfterAddTask(t *testing.T) {
	resp, err := client.GetTasks(context.Background(), &pb.DateRequest{Date: testDate})
	require.NoError(t, err)
	require.Len(t, resp.Tasks, 1)
	assert.Equal(t, "Buy groceries", resp.Tasks[0].Text)
	assert.False(t, resp.Tasks[0].Completed)
	assert.Equal(t, int32(0), resp.Tasks[0].Index)
}

func TestAddTask_MultipleTasksPreserveOrder(t *testing.T) {
	client.AddTask(context.Background(), &pb.AddTaskRequest{Date: testDate, TaskText: "Task A"})
	client.AddTask(context.Background(), &pb.AddTaskRequest{Date: testDate, TaskText: "Task B"})

	resp, err := client.GetTasks(context.Background(), &pb.DateRequest{Date: testDate})
	require.NoError(t, err)
	require.Len(t, resp.Tasks, 3)

	texts := []string{resp.Tasks[0].Text, resp.Tasks[1].Text, resp.Tasks[2].Text}
	assert.Equal(t, []string{"Buy groceries", "Task A", "Task B"}, texts)
}

// --- ToggleTask ---

func TestToggleTask_MarksCompleted(t *testing.T) {
	resp, err := client.ToggleTask(context.Background(), &pb.ToggleTaskRequest{
		Date:      testDate,
		TaskIndex: 0,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)

	tasks, err := client.GetTasks(context.Background(), &pb.DateRequest{Date: testDate})
	require.NoError(t, err)
	assert.True(t, tasks.Tasks[0].Completed)
}

func TestToggleTask_MarksIncomplete(t *testing.T) {
	client.ToggleTask(context.Background(), &pb.ToggleTaskRequest{Date: testDate, TaskIndex: 0})

	tasks, err := client.GetTasks(context.Background(), &pb.DateRequest{Date: testDate})
	require.NoError(t, err)
	assert.False(t, tasks.Tasks[0].Completed)
}

// --- AppendToNote ---

func TestAppendToNote_AppendsText(t *testing.T) {
	resp, err := client.AppendToNote(context.Background(), &pb.AppendRequest{
		Date: testDate,
		Text: "Hello from integration test",
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestGetNote_ContainsAppendedText(t *testing.T) {
	resp, err := client.GetNote(context.Background(), &pb.DateRequest{Date: testDate})
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "Hello from integration test")
}

func TestAppendToNote_CreatesNoteIfMissing(t *testing.T) {
	newDate := "15-Apr-2026"
	resp, err := client.AppendToNote(context.Background(), &pb.AppendRequest{
		Date: newDate,
		Text: "Auto-created",
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)

	note, err := client.GetNote(context.Background(), &pb.DateRequest{Date: newDate})
	require.NoError(t, err)
	assert.Contains(t, note.Content, "Auto-created")
}

// --- GetExistingDates (финальная проверка) ---

func TestGetExistingDates_ContainsAllCreatedDates(t *testing.T) {
	resp, err := client.GetExistingDates(context.Background(), &pb.Empty{})
	require.NoError(t, err)

	dates := resp.Dates
	sort.Strings(dates)
	assert.Contains(t, dates, testDate)
	assert.Contains(t, dates, "15-Apr-2026")
}
