package tghandlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/tgstates"
)

type noteFlowState struct {
	tgstates.StateStore
	value tgstates.UserContext
}

func (s *noteFlowState) GetContext(context.Context, int64) (*tgstates.UserContext, error) {
	value := s.value
	return &value, nil
}
func (s *noteFlowState) UpdateContext(_ context.Context, _ int64, update func(*tgstates.UserContext)) error {
	update(&s.value)
	return nil
}

type noteFlowCore struct {
	clients.CoreService
	path, text, listed string
	fail               bool
}

func (c *noteFlowCore) AppendToNoteByPath(_ context.Context, path, text string) (bool, error) {
	c.path, c.text = path, text
	if c.fail {
		return false, errors.New("write failed")
	}
	return true, nil
}
func (c *noteFlowCore) ListDirectory(_ context.Context, path string) ([]clients.DirEntry, error) {
	c.listed = path
	return nil, nil
}

type noteFlowHTTP struct{ forms []url.Values }

func (c *noteFlowHTTP) Do(req *http.Request) (*http.Response, error) {
	if err := req.ParseForm(); err != nil {
		return nil, err
	}
	c.forms = append(c.forms, req.PostForm)
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"Test","message_id":1}}`)), Header: make(http.Header)}, nil
}

func TestAppendNotePreservesNavigation(t *testing.T) {
	for _, source := range []tgstates.UserState{tgstates.StateBrowseFile, tgstates.StateViewNote} {
		t.Run(string(source), func(t *testing.T) {
			ctx := context.Background()
			state := &noteFlowState{value: tgstates.UserContext{State: source, BrowsePath: "Projects", ActiveRelpath: "Projects/Idea.md", ActiveNoteID: 42, FindResults: []tgstates.SearchHit{{NoteID: 42}}}}
			core := &noteFlowCore{}
			transport := &noteFlowHTTP{}
			bot, err := tgbotapi.NewBotAPIWithClient("test", "https://example.invalid/bot%s/%s", transport)
			require.NoError(t, err)
			app := &App{State: state, Core: core, Logger: zap.NewNop()}
			query := &tgbotapi.CallbackQuery{Message: &tgbotapi.Message{MessageID: 1, Chat: &tgbotapi.Chat{ID: 1}}}
			if source == tgstates.StateBrowseFile {
				require.NoError(t, app.showBrowseFile(ctx, bot, query, 1, "Projects/Idea.md", "Existing content", 0))
				require.Zero(t, state.value.ActiveNoteID)
				require.Contains(t, transport.forms[len(transport.forms)-1].Get("reply_markup"), "note:append")
			}
			for range 2 {
				require.NoError(t, app.handleNoteAppendAction(ctx, bot, query, 1))
				// A repeated click must not overwrite the navigation origin.
				require.NoError(t, app.handleNoteAppendAction(ctx, bot, query, 1))
				require.Equal(t, tgstates.StateAppendToNoteInput, state.value.State)
				app.handleAppendToNoteInput(ctx, bot, 1, 1, "New text")
				require.Equal(t, "Projects/Idea.md", core.path)
				require.Equal(t, "New text", core.text)
				require.Equal(t, source, state.value.State)
				markup := transport.forms[len(transport.forms)-1].Get("reply_markup")
				require.Contains(t, markup, "note:append")
				if source == tgstates.StateBrowseFile {
					require.Contains(t, markup, "browse:file_back")
					require.NotContains(t, markup, "find:back")
				} else {
					require.Contains(t, markup, "find:back")
				}
			}
			if source == tgstates.StateBrowseFile {
				require.NoError(t, app.handleBrowseAction(ctx, bot, query, 1, []string{"browse", "file_back"}))
				require.Equal(t, "Projects", core.listed)
				require.Empty(t, state.value.ActiveRelpath)
				require.Equal(t, tgstates.StateBrowseView, state.value.State)
			}
		})
	}
}

func TestBrowseAppendFailureKeepsInputForRetry(t *testing.T) {
	ctx := context.Background()
	state := &noteFlowState{value: tgstates.UserContext{State: tgstates.StateAppendToNoteInput, AppendReturnState: tgstates.StateBrowseFile, ActiveRelpath: "Projects/Idea.md"}}
	core := &noteFlowCore{fail: true}
	transport := &noteFlowHTTP{}
	bot, err := tgbotapi.NewBotAPIWithClient("test", "https://example.invalid/bot%s/%s", transport)
	require.NoError(t, err)
	app := &App{State: state, Core: core, Logger: zap.NewNop()}
	app.handleAppendToNoteInput(ctx, bot, 1, 1, "Retry me")
	require.Equal(t, tgstates.StateAppendToNoteInput, state.value.State)
	require.Equal(t, tgstates.StateBrowseFile, state.value.AppendReturnState)
	require.Contains(t, transport.forms[len(transport.forms)-1].Get("text"), "Не удалось")
	core.fail = false
	app.handleAppendToNoteInput(ctx, bot, 1, 1, "Retry me")
	require.Equal(t, tgstates.StateBrowseFile, state.value.State)
}
