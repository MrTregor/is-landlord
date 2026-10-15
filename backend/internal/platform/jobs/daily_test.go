package jobs

import (
	"testing"
	"time"
)

// «Каждые 15 минут» (fiscal.reconcile): 00, 15, 30, 45 минут каждого часа.
func TestEverySchedule(t *testing.T) {
	s := EverySchedule{Minutes: 15}
	cases := map[time.Time]time.Time{
		time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC):   time.Date(2026, 10, 18, 10, 15, 0, 0, time.UTC),
		time.Date(2026, 10, 18, 10, 14, 59, 0, time.UTC): time.Date(2026, 10, 18, 10, 15, 0, 0, time.UTC),
		time.Date(2026, 10, 18, 23, 50, 0, 0, time.UTC):  time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC),
	}
	for at, want := range cases {
		if got := s.Next(at); !got.Equal(want) {
			t.Errorf("после %v: %v, ожидалось %v", at, got, want)
		}
	}
	if !(Daily{EveryMinutes: 15}).Repeating() || (Daily{Hour: 1}).Repeating() {
		t.Fatal("Repeating")
	}
}
