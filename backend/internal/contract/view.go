package contract

import (
	"context"

	"github.com/google/uuid"

	"landlord/backend/internal/billing/calc"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
)

// Detail — карточка договора.
type Detail struct {
	Contract  sqlc.Contract
	Property  sqlc.Property
	Tenant    sqlc.Tenant
	Terms     []sqlc.ContractTerm
	Occupants []sqlc.ContractOccupant
	Warnings  []string
	Blockers  Blockers
	NextDue   *Due
}

// Due — ближайший срок оплаты по графику договора.
type Due struct {
	Date      date.Date
	AmountKop money.Kop
}

// Detail собирает карточку договора.
func (s *Service) Detail(ctx context.Context, c sqlc.Contract) (Detail, error) {
	q := s.db.Queries()
	d := Detail{Contract: c}
	var err error
	if d.Property, err = q.GetProperty(ctx, c.PropertyID); err != nil {
		return d, err
	}
	if d.Tenant, err = q.GetTenant(ctx, c.TenantID); err != nil {
		return d, err
	}
	if d.Terms, err = q.ListTerms(ctx, c.ID); err != nil {
		return d, err
	}
	if d.Occupants, err = q.ListOccupants(ctx, c.ID); err != nil {
		return d, err
	}
	if d.Warnings, err = s.Warnings(ctx, q, c, d.Property); err != nil {
		return d, err
	}
	if c.Status == StatusDraft {
		if d.Blockers, err = s.blockers(ctx, q, c); err != nil {
			return d, err
		}
	}
	d.NextDue = s.nextDue(c, d.Terms)
	return d, nil
}

// CurrentTerms — версия условий на сегодня (до начала договора — версия 1).
func (s *Service) CurrentTerms(c sqlc.Contract, terms []sqlc.ContractTerm) sqlc.ContractTerm {
	cur := terms[0]
	on := date.Max(c.StartDate, s.clock.Today())
	for _, t := range terms {
		if !t.EffectiveFrom.After(on) && !t.EffectiveFrom.Before(cur.EffectiveFrom) {
			cur = t
		}
	}
	return cur
}

// nextDue — ближайший срок оплаты не раньше сегодняшнего по графику (05 разд. 5); с этапа M4 — по начислениям.
func (s *Service) nextDue(c sqlc.Contract, terms []sqlc.ContractTerm) *Due {
	if c.Status != StatusActive && c.Status != StatusExtended || len(terms) == 0 {
		return nil
	}
	today := s.clock.Today()
	for _, l := range calc.Schedule(c.StartDate, actualEnd(c), c.FirstDueDate, TermsOf(terms), c.UtilitiesMode == UtilitiesFixed, 0) {
		if !l.DueDate.Before(today) {
			return &Due{Date: l.DueDate, AmountKop: l.RentKop + l.UtilitiesKop}
		}
	}
	return nil
}

// ListFilter — фильтры списка договоров.
type ListFilter struct {
	Statuses         []string
	PropertyID       *uuid.UUID
	TenantID         *uuid.UUID
	EndingWithinDays *int
	Q                *string
	Sort             string
	Limit, Offset    int32
}

// List — договоры с объектом, нанимателем и текущей платой.
func (s *Service) List(ctx context.Context, f ListFilter) ([]sqlc.ListContractsRow, int64, error) {
	q := s.db.Queries()
	var endingBefore *date.Date
	if f.EndingWithinDays != nil {
		d := s.clock.Today().AddDays(*f.EndingWithinDays)
		endingBefore = &d
	}
	statuses := f.Statuses
	if statuses == nil {
		statuses = []string{}
	}
	rows, err := q.ListContracts(ctx, sqlc.ListContractsParams{
		Today: s.clock.Today(), Statuses: statuses, PropertyID: f.PropertyID, TenantID: f.TenantID, EndingBefore: endingBefore,
		Q: f.Q, Sort: f.Sort, Lim: f.Limit, Off: f.Offset,
	})
	if err != nil {
		return nil, 0, err
	}
	n, err := q.CountContracts(ctx, sqlc.CountContractsParams{
		Statuses: statuses, PropertyID: f.PropertyID, TenantID: f.TenantID, EndingBefore: endingBefore, Q: f.Q,
	})
	return rows, n, err
}
