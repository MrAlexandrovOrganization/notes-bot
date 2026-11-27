package journalreport

import (
	"testing"
	"testing/fstest"
)

func TestScanHistory(t *testing.T) {
	vault := fstest.MapFS{
		"Daily/01-Jan-2024.md":  {Data: []byte("---\r\nОценка: 0\r\n---\r\n- [x] ЗАЛ [completion:: 2024-01-02] <!-- task -->\r\n- [ ] Зал\r\n---\r\nПривет 🌍\r\n[[Алиса|А]] [[Борис#Данные|Б]] [[Алиса|А]]\r\n![[image.png]]\r\n```text\r\n[[Ошибка]]\r\n```\r\n")},
		"Daily/02-Jan-2024.md":  {Data: []byte("---\nОценка:\n---\n- [X] зал\n- [x] Зал\n- [-] зал\n- [x] Купить зал\n")},
		"Daily/03-Jan-2024.md":  {Data: []byte("---\nОценка: 12\n---\n- [-] зал\n- [x] Не зал\n")},
		"Daily/README.md":       {Data: []byte("no daily date")},
		"Other/Backlink.md":     {Data: []byte("---\ndate: '[[01-Jan-2024]]'\n---\n[день](../Daily/01-Jan-2024.md)\n")},
		"Tags/People/Алиса.md":  {Data: []byte("[[01-Jan-2024]]")},
		"Tags/People/Борис.md":  {Data: []byte("")},
		"Tags/People/Ошибка.md": {Data: []byte("")},
		"Trash/Old.md":          {Data: []byte("[[01-Jan-2024]]")},
		"Templates/Test.md":     {Data: []byte("[[01-Jan-2024]]")},
		".obsidian/Hidden.md":   {Data: []byte("[[01-Jan-2024]]")},
	}
	snapshot, err := scan(t.Context(), vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Days) != 3 {
		t.Fatalf("expected three daily notes, got %d", len(snapshot.Days))
	}
	first := snapshot.Days[0]
	if first.Rating == nil || *first.Rating != 0 || first.Links != 3 || first.Characters != 13 {
		t.Fatalf("unexpected first day: %+v", first)
	}
	for _, day := range snapshot.Days[1:] {
		if day.Rating != nil {
			t.Fatalf("unexpected rating on %v", day.Date)
		}
	}
	if len(snapshot.GymDays) != 2 || snapshot.GymDays[0].Day() != 1 || snapshot.GymDays[1].Day() != 2 {
		t.Fatalf("expected one gym mark per note on January 1 and 2, got %v", snapshot.GymDays)
	}
	if len(snapshot.Meetings) != 1 || snapshot.Meetings[0].Date.Day() != 1 {
		t.Fatalf("expected one pair on January 1, got %v", snapshot.Meetings)
	}
}

func TestScanResolvesLinksAndRejectsAmbiguousNames(t *testing.T) {
	vault := fstest.MapFS{
		"Daily/29-Feb-2024.md":    {Data: []byte("`[[Alice]]` [[Alice]] [[Tags/People/Bob.md]] [Личное](../Tags/People/John%20Doe.md#test) [[29-Feb-2024]]")},
		"Tags/People/Alice.md":    {Data: []byte("")},
		"Other/Alice.md":          {Data: []byte("")},
		"Tags/People/Bob.md":      {Data: []byte("")},
		"Tags/People/John Doe.md": {Data: []byte("")},
	}
	snapshot, err := scan(t.Context(), vault)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Days[0].Links != 2 || len(snapshot.Meetings) != 1 {
		t.Fatalf("expected two linked people and one pair, got %+v", snapshot)
	}
	if _, err := scan(t.Context(), fstest.MapFS{}); err == nil {
		t.Fatal("empty vault must not replace a prior snapshot")
	}
}
