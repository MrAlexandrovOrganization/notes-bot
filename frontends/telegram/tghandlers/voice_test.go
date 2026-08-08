package tghandlers

import "testing"

func TestTelegramFileURL_LocalAPIEndpoint(t *testing.T) {
	got := telegramFileURL("http://telegram-bot-api:8081/", "bottest-token", "/voice/file_0.oga")
	want := "http://telegram-bot-api:8081/file/botbottest-token/voice/file_0.oga"
	if got != want {
		t.Fatalf("telegramFileURL() = %q, want %q", got, want)
	}
}
