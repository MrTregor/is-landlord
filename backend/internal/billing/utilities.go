package billing

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/billing/calc"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
)

var meterTitles = map[string]string{
	"cold_water": "Холодная вода", "hot_water": "Горячая вода", "electricity": "Электроэнергия",
	"electricity_day": "Электроэнергия (день)", "electricity_night": "Электроэнергия (ночь)", "gas": "Газ", "heating": "Отопление",
}

// UtilitiesResult — созданное начисление ЖКУ и предупреждения (счётчики без показаний).
type UtilitiesResult struct {
	ChargeID uuid.UUID
	Amount   money.Kop
	Warnings []string
}

// reading — показание счётчика для строки ЖКУ.
type reading struct {
	value decimal.Decimal
	date  date.Date
	id    uuid.UUID
}

func (s *Service) tariffs(ctx context.Context, q *sqlc.Queries) ([]calc.Tariff, error) {
	rows, err := q.AllTariffs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]calc.Tariff, 0, len(rows))
	for _, t := range rows {
		out = append(out, calc.Tariff{MeterType: t.MeterType, PropertyID: t.PropertyID, Price: t.Price, EffectiveFrom: t.EffectiveFrom})
	}
	return out, nil
}

// previous — предыдущее принятое показание раньше даты, иначе начальное показание счётчика (05 разд. 6.2).
func previous(ctx context.Context, q *sqlc.Queries, m sqlc.Meter, before date.Date, exclude uuid.UUID) (decimal.Decimal, date.Date, error) {
	p, err := q.PrevAccepted(ctx, sqlc.PrevAcceptedParams{MeterID: m.ID, BeforeDate: before, ExcludeID: exclude})
	if err == nil {
		return p.Value, p.ReadingDate, nil
	}
	if !db.IsNoRows(err) {
		return decimal.Decimal{}, date.Date{}, err
	}
	return m.InitialValue, m.InitialDate, nil
}

// lines — строки счётчиков: расход по показаниям, тариф на день tariffOn; нет тарифа — ошибка.
func (s *Service) lines(ctx context.Context, q *sqlc.Queries, propertyID uuid.UUID, meters []sqlc.Meter,
	values map[uuid.UUID]reading, tariffOn date.Date, monthName string,
) ([]calc.MeterLine, []string, error) {
	tariffs, err := s.tariffs(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	var out []calc.MeterLine
	var warnings []string
	for _, m := range meters {
		price, ok := calc.SelectTariff(tariffs, m.Type, propertyID, tariffOn)
		if !ok {
			return nil, nil, apperr.BusinessRule(fmt.Sprintf("Не задан тариф «%s» на %s", meterTitles[m.Type], monthName))
		}
		r, has := values[m.ID]
		before := tariffOn.AddDays(1)
		var exclude uuid.UUID
		if has {
			before, exclude = r.date, r.id
		}
		prev, prevDate, err := previous(ctx, q, m, before, exclude)
		if err != nil {
			return nil, nil, err
		}
		var v *decimal.Decimal
		if has {
			v = &r.value
		} else {
			warnings = append(warnings, fmt.Sprintf("Нет показания счётчика «%s» — расход принят равным 0", meterTitles[m.Type]))
		}
		out = append(out, calc.MeterAmount(m.ID, m.Type, prev, prevDate, v, price))
	}
	return out, warnings, nil
}

func utilitiesDetails(lines []calc.MeterLine, fixed money.Kop, extra map[string]any) []byte {
	ls := make([]map[string]any, 0, len(lines))
	for _, l := range lines {
		row := map[string]any{
			"meterId": l.MeterID.String(), "type": l.Type, "prev": l.Prev.StringFixed(3), "prevDate": l.PrevDate.String(),
			"value": l.Value.StringFixed(3), "consumption": l.Consumption.StringFixed(3), "tariff": l.Tariff.StringFixed(4),
			"amount": int64(l.Amount),
		}
		if l.Missing {
			row["missing"] = true
		}
		ls = append(ls, row)
	}
	m := map[string]any{"lines": ls, "fixedKop": int64(fixed)}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return b
}

// Utilities — начисление ЖКУ по показаниям за месяц month (05 разд. 6.3). onlyIfComplete — автоматический случай
// (при принятии показания): создаётся, только если приняты показания всех неархивных счётчиков; иначе — кнопкой
// собственника, по счётчикам без показаний расход 0 и предупреждение. Повтор за тот же месяц — ошибка или nil (авто).
func (s *Service) Utilities(ctx context.Context, tx *db.Tx, c sqlc.Contract, month date.Date, onlyIfComplete bool) (*UtilitiesResult, error) {
	month = month.FirstOfMonth()
	monthName := calc.MonthName(month)
	if c.UtilitiesMode != "meters" {
		if onlyIfComplete {
			return nil, nil
		}
		return nil, apperr.BusinessRule("Начисление ЖКУ по показаниям — только для порядка «По счётчикам»")
	}
	if _, ok := calc.PeriodOf(c.StartDate, ActualEnd(c), month); !ok {
		if onlyIfComplete {
			return nil, nil
		}
		return nil, apperr.Validation(apperr.FieldError{Field: "periodMonth", Message: "Месяц вне срока договора"})
	}
	key := fmt.Sprintf("util:%s:%s", c.ID, monthKey(month))
	if _, err := tx.Q.ChargeByKey(ctx, &key); err == nil {
		if onlyIfComplete {
			return nil, nil
		}
		return nil, apperr.Conflict(apperr.CodeConflictExists, "Начисление ЖКУ за "+monthName+" уже создано")
	} else if !db.IsNoRows(err) {
		return nil, err
	}
	meters, err := tx.Q.ActivePropertyMeters(ctx, c.PropertyID)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Q.MonthReadings(ctx, sqlc.MonthReadingsParams{PropertyID: c.PropertyID, PeriodMonth: month})
	if err != nil {
		return nil, err
	}
	values := map[uuid.UUID]reading{}
	for _, r := range rows {
		values[r.MeterID] = reading{value: r.Value, date: r.ReadingDate, id: r.ID}
	}
	if onlyIfComplete {
		for _, m := range meters {
			if _, ok := values[m.ID]; !ok {
				return nil, nil
			}
		}
	}
	lines, warnings, err := s.lines(ctx, tx.Q, c.PropertyID, meters, values, month.LastOfMonth(), monthName)
	if err != nil {
		return nil, err
	}
	terms, err := tx.Q.ListTerms(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	t := calc.TermsFor(TermsOf(terms), date.Max(c.StartDate, month))
	// Фиксированная часть за неполный месяц (месяц начала) — пропорционально дням пользования, как в итоговом
	// начислении по акту возврата (№ 31).
	fixed := money.Kop(0)
	start, end := month, month.LastOfMonth()
	if p, ok := calc.PeriodOf(c.StartDate, ActualEnd(c), month); ok {
		fixed = calc.ProRate(t.UtilitiesFixedKop, p)
		start = date.Max(c.StartDate, month)
	}
	total := calc.UtilitiesTotal(lines, fixed)
	today := s.clock.Today()
	due := calc.UtilitiesDue(month, t.PaymentDay, today)
	id, err := tx.Q.InsertAutoCharge(ctx, sqlc.InsertAutoChargeParams{
		ContractID: c.ID, Kind: KindUtilities, Title: "Компенсация ЖКУ, " + monthName, PeriodMonth: &month,
		PeriodStart: &start, PeriodEnd: &end, DueDate: &due, AmountKop: int64(total), Status: "open",
		CalcDetails: utilitiesDetails(lines, fixed, nil), IdempotencyKey: &key, Now: s.clock.Now(),
	})
	if err != nil {
		return nil, err
	}
	if err := s.Rebalance(ctx, tx, c.ID); err != nil {
		return nil, err
	}
	return &UtilitiesResult{ChargeID: id, Amount: total, Warnings: warnings}, nil
}

// FinalUtilities — итоговое начисление ЖКУ при подписании акта возврата (05 разд. 6.3): строки от предыдущего
// показания до показания акта; фиксированная часть — пропорционально дням последнего месяца.
func (s *Service) FinalUtilities(ctx context.Context, tx *db.Tx, c sqlc.Contract, act sqlc.ContractAct) (*UtilitiesResult, error) {
	if c.UtilitiesMode != "meters" {
		return nil, nil
	}
	key := "util-final:" + c.ID.String()
	if _, err := tx.Q.ChargeByKey(ctx, &key); err == nil {
		return nil, nil
	} else if !db.IsNoRows(err) {
		return nil, err
	}
	meters, err := tx.Q.ActivePropertyMeters(ctx, c.PropertyID)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Q.ActReadings(ctx, &act.ID)
	if err != nil {
		return nil, err
	}
	values := map[uuid.UUID]reading{}
	for _, r := range rows {
		values[r.MeterID] = reading{value: r.Value, date: r.ReadingDate, id: r.ID}
	}
	lines, warnings, err := s.lines(ctx, tx.Q, c.PropertyID, meters, values, act.ActDate, act.ActDate.Format())
	if err != nil {
		return nil, err
	}
	end := ActualEnd(c)
	month := end.FirstOfMonth()
	terms, err := tx.Q.ListTerms(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	t := calc.TermsFor(TermsOf(terms), date.Max(c.StartDate, month))
	fixed := money.Kop(0)
	if p, ok := calc.PeriodOf(c.StartDate, end, month); ok {
		fixed = calc.ProRate(t.UtilitiesFixedKop, p)
	}
	total := calc.UtilitiesTotal(lines, fixed)
	due := calc.UtilitiesDue(month, t.PaymentDay, s.clock.Today())
	start := date.Max(c.StartDate, month)
	id, err := tx.Q.InsertAutoCharge(ctx, sqlc.InsertAutoChargeParams{
		ContractID: c.ID, Kind: KindUtilities, Title: "Компенсация ЖКУ по акту возврата от " + act.ActDate.Format(),
		PeriodMonth: &month, PeriodStart: &start, PeriodEnd: &end, DueDate: &due, AmountKop: int64(total), Status: "open",
		CalcDetails: utilitiesDetails(lines, fixed, map[string]any{"actId": act.ID.String()}), IdempotencyKey: &key, Now: s.clock.Now(),
	})
	if err != nil {
		return nil, err
	}
	if err := s.Rebalance(ctx, tx, c.ID); err != nil {
		return nil, err
	}
	return &UtilitiesResult{ChargeID: id, Amount: total, Warnings: warnings}, nil
}

// CreateUtilities — кнопка «Начисление ЖКУ за месяц»: начисление по показаниям с записью в журнал.
func (s *Service) CreateUtilities(ctx context.Context, contractID uuid.UUID, month date.Date) (*UtilitiesResult, error) {
	var res *UtilitiesResult
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		c, err := lockContract(ctx, tx, contractID)
		if err != nil {
			return err
		}
		if c.Status == "draft" {
			return apperr.Conflict(apperr.CodeConflictState, "Договор не введён в действие")
		}
		if res, err = s.Utilities(ctx, tx, c, month, false); err != nil {
			return err
		}
		return s.RecordCreated(ctx, tx, c, res, "по кнопке собственника")
	})
	return res, err
}

// RecordCreated — запись charge.create о созданном начислении ЖКУ.
func (s *Service) RecordCreated(ctx context.Context, tx *db.Tx, c sqlc.Contract, res *UtilitiesResult, how string) error {
	if res == nil {
		return nil
	}
	return s.audit.Record(ctx, tx.Q, audit.Entry{
		Action: "charge.create", EntityType: "charge", EntityID: res.ChargeID.String(),
		Summary: fmt.Sprintf("Договор № %s: начисление ЖКУ на %s (%s)", c.Number, res.Amount.Format(), how),
		After:   map[string]any{"amountKop": int64(res.Amount), "warnings": res.Warnings},
	})
}
