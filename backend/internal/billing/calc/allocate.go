package calc

import (
	"sort"

	"github.com/google/uuid"

	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/money"
)

// OpenCharge — открытое начисление для распределения платежа (05 разд. 10.3).
type OpenCharge struct {
	ID           uuid.UUID
	Kind         string     // rent, utilities, penalty, damage, other
	PenaltyState string     // для пеней: accruing, final, manual
	DueDate      *date.Date // nil только у пеней accruing
	Seq          int        // порядок создания (created_at)
	Outstanding  money.Kop  // amount − paid
}

// Allocation — сумма распределения на начисление.
type Allocation struct {
	ChargeID uuid.UUID
	Amount   money.Kop
}

func kindRank(kind string) int {
	switch kind {
	case "rent":
		return 1
	case "utilities":
		return 2
	case "damage":
		return 3
	case "other":
		return 4
	default:
		return 5
	}
}

// payable — начисление можно погашать: есть остаток, не начисляемые пени (у них нет срока).
func payable(c OpenCharge) bool {
	return c.Outstanding > 0 && c.DueDate != nil && (c.Kind != "penalty" || c.PenaltyState != "accruing")
}

// less — по сроку оплаты, затем по виду (rent → utilities → damage → other → пени), затем по порядку создания.
func less(a, b OpenCharge) bool {
	if c := a.DueDate.Compare(*b.DueDate); c != 0 {
		return c < 0
	}
	if ra, rb := kindRank(a.Kind), kindRank(b.Kind); ra != rb {
		return ra < rb
	}
	return a.Seq < b.Seq
}

// AllocationOrder — порядок погашения (05 разд. 10.3): 1) выбранные начисления по сроку и виду; 2) основной долг
// (rent, utilities, damage, other) по сроку, при равном сроке rent → utilities → damage → other, затем по созданию;
// 3) пени final и manual по сроку и созданию. Начисляемые пени (accruing) и погашенные начисления не входят.
func AllocationOrder(list []OpenCharge, requested []uuid.UUID) []OpenCharge {
	req := make(map[uuid.UUID]bool, len(requested))
	for _, id := range requested {
		req[id] = true
	}
	var first, main, penalties []OpenCharge
	for _, c := range list {
		switch {
		case !payable(c):
		case req[c.ID]:
			first = append(first, c)
		case c.Kind == "penalty":
			penalties = append(penalties, c)
		default:
			main = append(main, c)
		}
	}
	for _, part := range [][]OpenCharge{first, main, penalties} {
		sort.SliceStable(part, func(i, j int) bool { return less(part[i], part[j]) })
	}
	out := make([]OpenCharge, 0, len(first)+len(main)+len(penalties))
	out = append(out, first...)
	out = append(out, main...)
	return append(out, penalties...)
}

// Allocate распределяет доступную сумму avail по порядку AllocationOrder: каждое начисление погашается на
// min(avail, остаток). Возвращает распределения и нераспределённый остаток (аванс платежа).
func Allocate(avail money.Kop, list []OpenCharge, requested []uuid.UUID) ([]Allocation, money.Kop) {
	var out []Allocation
	for _, c := range AllocationOrder(list, requested) {
		if avail <= 0 {
			break
		}
		amt := min(avail, c.Outstanding)
		out = append(out, Allocation{ChargeID: c.ID, Amount: amt})
		avail -= amt
	}
	return out, avail
}
