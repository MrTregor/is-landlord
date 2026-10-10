package contract

import (
	"context"

	"github.com/google/uuid"

	"landlord/backend/internal/billing"
	"landlord/backend/internal/billing/calc"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
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
	Balances  Balances
}

// Balances — суммы по начислениям и платежам договора (залог — с этапа M6).
type Balances struct {
	DebtKop, OverdueKop, PenaltiesAccruingKop, AdvanceKop int64
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
	if d.NextDue, err = s.nextDue(ctx, q, c, d.Terms); err != nil {
		return d, err
	}
	if d.Balances, err = s.balances(ctx, q, c.ID); err != nil {
		return d, err
	}
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

// nextDue — ближайший срок оплаты не раньше сегодняшнего: по открытым начислениям, а если их нет — по графику.
func (s *Service) nextDue(ctx context.Context, q *sqlc.Queries, c sqlc.Contract, terms []sqlc.ContractTerm) (*Due, error) {
	if c.Status == StatusDraft || len(terms) == 0 {
		return nil, nil
	}
	today := s.clock.Today()
	r, err := q.NextDueCharges(ctx, sqlc.NextDueChargesParams{ContractID: c.ID, Today: today})
	if err == nil {
		return &Due{Date: r.DueDate, AmountKop: money.Kop(r.AmountKop)}, nil
	}
	if !db.IsNoRows(err) {
		return nil, err
	}
	if c.Status != StatusActive && c.Status != StatusExtended {
		return nil, nil
	}
	for _, l := range calc.Schedule(c.StartDate, actualEnd(c), c.FirstDueDate, billing.TermsOf(terms), c.UtilitiesMode == UtilitiesFixed, 0) {
		if !l.DueDate.Before(today) {
			return &Due{Date: l.DueDate, AmountKop: l.RentKop + l.UtilitiesKop}, nil
		}
	}
	return nil, nil
}

// balances — долг, просрочка, начисляемые пени и аванс договора.
func (s *Service) balances(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (Balances, error) {
	var b Balances
	rows, err := q.ContractBalances(ctx, sqlc.ContractBalancesParams{Ids: []uuid.UUID{id}, Today: s.clock.Today()})
	if err != nil {
		return b, err
	}
	for _, r := range rows {
		b.DebtKop, b.OverdueKop, b.PenaltiesAccruingKop = r.DebtKop, r.OverdueKop, r.PenaltiesAccruingKop
	}
	pays, err := q.PaymentsWithAdvance(ctx, id)
	if err != nil {
		return b, err
	}
	for _, p := range pays {
		if p.AdvanceKop > 0 {
			b.AdvanceKop += p.AdvanceKop
		}
	}
	return b, nil
}

// Debts — долг по договорам (списки).
func (s *Service) Debts(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]int64, error) {
	out := map[uuid.UUID]int64{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.db.Queries().ContractBalances(ctx, sqlc.ContractBalancesParams{Ids: ids, Today: s.clock.Today()})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ContractID] = r.DebtKop
	}
	return out, nil
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
