package calc

import (
	"testing"

	"github.com/google/uuid"

	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/money"
)

func due(s string) *date.Date {
	v := d(s)
	return &v
}

func amounts(list []Allocation) map[uuid.UUID]money.Kop {
	out := map[uuid.UUID]money.Kop{}
	for _, a := range list {
		out[a.ChargeID] += a.Amount
	}
	return out
}

// A1 (05 разд. 10.3, ТК-21): A «Плата, сентябрь» 28 000 (10.09), B «ЖКУ, август» 3 450,60 (10.10), C «Плата, октябрь»
// 28 000 (10.10), P «Пени по A» 120 (final). Платёж 40 000: A → 28 000, C → 12 000; B и P — 0; аванса нет.
func TestAllocateA1_FR07_1(t *testing.T) {
	a, b, c, p := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	list := []OpenCharge{
		{ID: b, Kind: "utilities", DueDate: due("2026-10-10"), Seq: 2, Outstanding: 345060},
		{ID: p, Kind: "penalty", PenaltyState: "final", DueDate: due("2026-10-12"), Seq: 4, Outstanding: 12000},
		{ID: c, Kind: "rent", DueDate: due("2026-10-10"), Seq: 3, Outstanding: 2800000},
		{ID: a, Kind: "rent", DueDate: due("2026-09-10"), Seq: 1, Outstanding: 2800000},
	}
	got, rest := Allocate(4000000, list, nil)
	if rest != 0 {
		t.Fatalf("аванс %d, ожидалось 0", rest)
	}
	m := amounts(got)
	if m[a] != 2800000 || m[c] != 1200000 || m[b] != 0 || m[p] != 0 {
		t.Fatalf("распределение %v", got)
	}
	if got[0].ChargeID != a || got[1].ChargeID != c {
		t.Fatalf("порядок %v", got)
	}
}

// Выбранные начисления погашаются первыми; остаток — по общему порядку.
func TestAllocateRequestedFirst_FR07_1(t *testing.T) {
	a, b, p := uuid.New(), uuid.New(), uuid.New()
	list := []OpenCharge{
		{ID: a, Kind: "rent", DueDate: due("2026-09-10"), Seq: 1, Outstanding: 2800000},
		{ID: b, Kind: "utilities", DueDate: due("2026-10-10"), Seq: 2, Outstanding: 345060},
		{ID: p, Kind: "penalty", PenaltyState: "final", DueDate: due("2026-10-12"), Seq: 3, Outstanding: 12000},
	}
	got, rest := Allocate(1000000, list, []uuid.UUID{p, b})
	if rest != 0 || len(got) != 3 {
		t.Fatalf("%v, аванс %d", got, rest)
	}
	if got[0].ChargeID != b || got[0].Amount != 345060 || got[1].ChargeID != p || got[1].Amount != 12000 {
		t.Fatalf("сначала выбранные по сроку: %v", got)
	}
	if got[2].ChargeID != a || got[2].Amount != 1000000-345060-12000 {
		t.Fatalf("остаток на основной долг: %v", got)
	}
}

// Пени accruing не погашаются даже выбранные; остаток — аванс.
func TestAllocateAccruingPenaltySkipped_FR07_1(t *testing.T) {
	a, p := uuid.New(), uuid.New()
	list := []OpenCharge{
		{ID: p, Kind: "penalty", PenaltyState: "accruing", Seq: 2, Outstanding: 5000},
		{ID: a, Kind: "rent", DueDate: due("2026-10-10"), Seq: 1, Outstanding: 1000000},
	}
	got, rest := Allocate(1500000, list, []uuid.UUID{p})
	if len(got) != 1 || got[0].ChargeID != a || got[0].Amount != 1000000 {
		t.Fatalf("%v", got)
	}
	if rest != 500000 {
		t.Fatalf("аванс %d, ожидалось 500000", rest)
	}
}

// Аванс распределяется на новое начисление (05 разд. 10.4): срок в будущем тоже погашается; при равном сроке — по виду.
func TestAllocateAdvanceToNewCharge_FR07_1(t *testing.T) {
	rent, util, other := uuid.New(), uuid.New(), uuid.New()
	list := []OpenCharge{
		{ID: other, Kind: "other", DueDate: due("2026-12-10"), Seq: 1, Outstanding: 10000},
		{ID: util, Kind: "utilities", DueDate: due("2026-12-10"), Seq: 3, Outstanding: 20000},
		{ID: rent, Kind: "rent", DueDate: due("2026-12-10"), Seq: 2, Outstanding: 30000},
	}
	got, rest := Allocate(45000, list, nil)
	if rest != 0 || len(got) != 2 || got[0].ChargeID != rent || got[1].ChargeID != util || got[1].Amount != 15000 {
		t.Fatalf("%v, остаток %d", got, rest)
	}
}
