package seed

import (
	"fmt"
	"net/http"

	"landlord/backend/internal/platform/date"
)

// loadPlan — режим --load (17 разд. 7): 100 объектов «НТ-001…НТ-100» и нанимателей с договорами 12-месячной
// истории; плата 20 000–40 000 с шагом 500 по номеру, 90 % платежей в срок, 10 % — через 7 дней; ЖКУ — 1 000.
// Номера договоров — 101…200, чтобы не пересекаться с договорами демо.
func (w *world) loadPlan(p *plan) {
	start := w.M(-12)
	p.start(start, func() error {
		for i := 1; i <= 100; i++ {
			n := fmt.Sprintf("%03d", i)
			pid, err := w.owner.id(http.MethodPost, "/properties", map[string]any{
				"code": "НТ-" + n, "kind": "residential", "address": "г. Руза, ул. Нагрузочная, д. 1, кв. " + n, "areaSqm": "30.0",
				"floor": 1 + i%9, "rooms": 1, "cadastralNumber": nil, "ownershipBasis": nil, "description": nil, "furnished": true,
				"expectedRentKop": 0, "fiscalMode": "none", "initialStatus": "free", "statusSince": start.AddDays(-1).String(),
			})
			if err != nil {
				return err
			}
			t := person("Нагрузочный", "Наниматель", n, "1990-01-01", fmt.Sprintf("46 00 %06d", i), "1"+n, "nt-"+n+"@landlord.local", "")
			t["phone"] = nil
			tid, err := w.owner.id(http.MethodPost, "/tenants", t)
			if err != nil {
				return err
			}
			w.ids["nt-p"+n], w.ids["nt-t"+n] = pid, tid
			c := contractSpec{key: "nt" + n, property: "nt-p" + n, tenant: "nt-t" + n, start: start, end: start.AddMonths(24).AddDays(-1),
				number: 100 + i, body: map[string]any{"rentKop": 2000000 + 50000*int64((i-1)%41), "paymentDay": 10, "depositKop": 0,
					"utilitiesMode": "fixed", "utilitiesFixedKop": 100000}}
			if err := w.activate(c); err != nil {
				return err
			}
		}
		return nil
	})
	p.every = append(p.every, func(d date.Date) error {
		for i := 1; i <= 100; i++ {
			key := fmt.Sprintf("nt%03d", i)
			if _, ok := w.ids[key]; !ok {
				return nil
			}
			due := d
			if i%10 == 0 {
				due = d.AddDays(-7)
			}
			rows, err := w.open(key, d)
			if err != nil {
				return err
			}
			if !dueOn(rows, due) {
				continue
			}
			if err := w.pay(key, total(rows), "bank_transfer", nil, d, ""); err != nil {
				return err
			}
		}
		return nil
	})
}
