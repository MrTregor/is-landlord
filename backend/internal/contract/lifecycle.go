package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
)

// Статусы объекта.
const (
	propFree     = "free"
	propReserved = "reserved"
	propRented   = "rented"
	propRepair   = "repair"
)

// Blockers — причины, по которым черновик нельзя ввести в действие (05 разд. 4.4); overlap — отдельно,
// потому что для него ответ API — 409 conflict_overlap.
type Blockers struct {
	List    []string
	Overlap string
}

func (b Blockers) empty() bool { return len(b.List) == 0 && b.Overlap == "" }

// All — все причины одним списком (overlap — первым).
func (b Blockers) All() []string {
	if b.Overlap == "" {
		return b.List
	}
	return append([]string{b.Overlap}, b.List...)
}

func period(from, to date.Date) string { return from.Format() + "–" + to.Format() }

// blockers проверяет предусловия активации 2–5 (статус проверяется отдельно).
func (s *Service) blockers(ctx context.Context, q *sqlc.Queries, c sqlc.Contract) (Blockers, error) {
	var b Blockers
	p, err := q.GetProperty(ctx, c.PropertyID)
	if err != nil {
		return b, err
	}
	if p.ArchivedAt != nil {
		b.List = append(b.List, "Объект в архиве. Сначала верните его из архива")
	}
	// Статус объекта на дату начала: запись истории, покрывающая дату (для будущей даты — текущая).
	on := date.Min(c.StartDate, s.clock.Today())
	rows, err := q.StatusHistoryFrom(ctx, sqlc.StatusHistoryFromParams{PropertyID: c.PropertyID, FromDate: on})
	if err != nil {
		return b, err
	}
	for _, r := range rows {
		if !r.ValidFrom.After(on) && r.Status == propRepair {
			b.List = append(b.List, "Объект на ремонте. Сначала смените статус объекта")
			break
		}
	}
	over, err := q.OverlappingContracts(ctx, sqlc.OverlappingContractsParams{
		PropertyID: c.PropertyID, ExcludeID: c.ID, FromDate: c.StartDate, ToDate: actualEnd(c),
	})
	if err != nil {
		return b, err
	}
	if len(over) > 0 {
		o := over[0]
		end := o.StartDate
		if o.ActualEndDate != nil {
			end = *o.ActualEndDate
		}
		b.Overlap = fmt.Sprintf("Сроки пересекаются с договором %s (%s)", o.Number, period(o.StartDate, end))
	}
	t, err := q.GetTenant(ctx, c.TenantID)
	if err != nil {
		return b, err
	}
	if t.AnonymizedAt != nil {
		b.List = append(b.List, "Наниматель обезличен")
	}
	if c.LegalBasis == BasisCh35 {
		m, err := s.tenants.Mask(t)
		if err != nil {
			return b, err
		}
		if !m.HasDocument {
			b.List = append(b.List, "Для договора найма заполните документ и адрес регистрации нанимателя")
		}
	}
	if c.UtilitiesMode == UtilitiesMeters {
		n, err := q.CountActiveMeters(ctx, c.PropertyID)
		if err != nil {
			return b, err
		}
		if n == 0 {
			b.List = append(b.List, "Для порядка ЖКУ «По счётчикам» добавьте объекту хотя бы один счётчик")
		}
	}
	return b, nil
}

// Warnings — предупреждения, которые не блокируют действия (05 разд. 4.3, 14 разд. 4.3).
func (s *Service) Warnings(ctx context.Context, q *sqlc.Queries, c sqlc.Contract, p sqlc.Property) ([]string, error) {
	var w []string
	if c.Status == StatusDraft && RegistrationRequired(c.StartDate, c.EndDate) {
		w = append(w, "Договор на срок от года подлежит государственной регистрации (п. 2 ст. 674 ГК РФ / ст. 651 ГК РФ). "+
			"Напоминание будет добавлено в календарь")
	}
	if p.Kind != "residential" && p.FiscalMode == "none" {
		w = append(w, "По нежилым объектам освобождение от применения ККТ не действует")
	}
	if c.Status == StatusActive || c.Status == StatusExtended {
		if RegistrationRequired(c.StartDate, c.EndDate) && c.RegistrationDoneOn == nil {
			w = append(w, "Договор подлежит государственной регистрации — регистрация не отмечена")
		}
		acts, err := q.ListActs(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		for _, a := range acts {
			if a.Kind == ActHandover && !a.Signed {
				w = append(w, "Акт приёма-передачи не подписан")
			}
		}
	}
	return w, nil
}

// Activate — «Подписан, ввести в действие» (05 разд. 4.4).
func (s *Service) Activate(ctx context.Context, id uuid.UUID, concludedOn date.Date, inviteTenant bool) (sqlc.Contract, error) {
	if concludedOn.After(s.clock.Today()) {
		return sqlc.Contract{}, apperr.Validation(apperr.FieldError{Field: "concludedOn", Message: "Дата подписания не может быть позже сегодняшней"})
	}
	var c sqlc.Contract
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		before, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if before.Status != StatusDraft {
			return apperr.Conflict(apperr.CodeConflictState, "Договор уже введён в действие")
		}
		p, err := tx.Q.GetPropertyForUpdate(ctx, before.PropertyID)
		if err != nil {
			return err
		}
		b, err := s.blockers(ctx, tx.Q, before)
		if err != nil {
			return err
		}
		if b.Overlap != "" {
			return apperr.Conflict(apperr.CodeConflictOverlap, b.Overlap)
		}
		if len(b.List) > 0 {
			return apperr.BusinessRule(b.List[0])
		}
		// П. 7: документ договора формируется, если его не сформировали после последнего изменения черновика.
		docFresh, err := s.docs.ContractDocumentFresh(ctx, tx.Q, before)
		if err != nil {
			return err
		}
		c, err = tx.Q.ActivateContract(ctx, sqlc.ActivateContractParams{
			ID: id, ConcludedOn: &concludedOn, Now: ptr(s.clock.Now()), ActivatedBy: actorID(ctx), NextIndexationOn: before.NextIndexationOn,
		})
		if err != nil {
			return dbError(err)
		}
		today := s.clock.Today()
		if !c.StartDate.After(today) {
			if err := s.setRented(ctx, tx, c); err != nil {
				return err
			}
		} else if p.Status == propFree {
			if err := s.setPropertyStatus(ctx, tx, c.PropertyID, propReserved, today, nil, "Договор № "+c.Number+" с "+c.StartDate.Format()); err != nil {
				return err
			}
		}
		created, err := s.billing.EnsureRent(ctx, tx, c, today)
		if err != nil {
			return err
		}
		if err := s.syncEvents(ctx, tx, c); err != nil {
			return err
		}
		if err := s.createHandoverAct(ctx, tx, c); err != nil {
			return err
		}
		if !docFresh {
			if _, err := s.docs.ContractDocumentTx(ctx, tx, c.ID); err != nil {
				return err
			}
		}
		if inviteTenant {
			if err := s.invite(ctx, tx, c.TenantID); err != nil {
				return err
			}
		}
		if err := s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "contract.activate", EntityType: "contract", EntityID: id.String(),
			Summary: fmt.Sprintf("Договор № %s введён в действие, подписан %s", c.Number, concludedOn.Format()),
			Before:  map[string]any{"status": before.Status},
			After:   map[string]any{"status": c.Status, "concludedOn": concludedOn.String(), "chargesCreated": len(created)},
		}); err != nil {
			return err
		}
		// Договор, срок которого уже истёк (внесение прошлого договора), сразу завершается.
		if c.EndDate.Before(today) {
			c, err = s.finish(ctx, tx, c)
		}
		return err
	})
	return c, err
}

// invite — приглашение нанимателя в кабинет при активации, если есть e-mail и нет учётной записи.
func (s *Service) invite(ctx context.Context, tx *db.Tx, tenantID uuid.UUID) error {
	t, err := tx.Q.GetTenantForUpdate(ctx, tenantID)
	if err != nil {
		return err
	}
	if t.Email == nil || strings.TrimSpace(*t.Email) == "" {
		return nil
	}
	if _, err := tx.Q.GetUserByTenantID(ctx, &t.ID); err == nil {
		return nil
	} else if !db.IsNoRows(err) {
		return err
	}
	_, err = s.tenants.InviteTx(ctx, tx, t)
	return err
}

// setRented — статус объекта «Сдан» с даты начала договора (05 разд. 4.4, п. 3): записи истории, начинающиеся
// с даты начала или позже, удаляются (если free/reserved), покрывающая запись закрывается днём раньше.
func (s *Service) setRented(ctx context.Context, tx *db.Tx, c sqlc.Contract) error {
	rows, err := tx.Q.StatusHistoryFrom(ctx, sqlc.StatusHistoryFromParams{PropertyID: c.PropertyID, FromDate: c.StartDate})
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.ContractID != nil && *r.ContractID == c.ID && r.Status == propRented {
			return nil // уже «Сдан» по этому договору
		}
		if r.Status == propRepair || r.Status == propRented {
			return apperr.BusinessRule("История статусов объекта противоречит дате начала договора")
		}
	}
	for _, r := range rows {
		if !r.ValidFrom.Before(c.StartDate) {
			if err := tx.Q.DeleteStatusHistory(ctx, r.ID); err != nil {
				return err
			}
			continue
		}
		if err := tx.Q.SetStatusHistoryValidTo(ctx, sqlc.SetStatusHistoryValidToParams{ID: r.ID, ValidTo: ptr(c.StartDate.AddDays(-1))}); err != nil {
			return err
		}
	}
	if err := tx.Q.InsertStatusHistory(ctx, sqlc.InsertStatusHistoryParams{
		PropertyID: c.PropertyID, Status: propRented, ValidFrom: c.StartDate, ContractID: &c.ID, CreatedBy: actorID(ctx), Now: s.clock.Now(),
	}); err != nil {
		return err
	}
	return tx.Q.SetPropertyStatus(ctx, sqlc.SetPropertyStatusParams{ID: c.PropertyID, Status: propRented})
}

// setPropertyStatus — сменить текущий статус объекта с даты since (как ручная смена: та же дата начала — замена записи).
func (s *Service) setPropertyStatus(ctx context.Context, tx *db.Tx, propertyID uuid.UUID, status string, since date.Date,
	contractID *uuid.UUID, reason string,
) error {
	cur, err := tx.Q.CurrentStatusRow(ctx, propertyID)
	if err != nil {
		return err
	}
	if cur.Status == status && (contractID == nil || (cur.ContractID != nil && *cur.ContractID == *contractID)) {
		return nil
	}
	if since.After(cur.ValidFrom) {
		if err := tx.Q.SetStatusHistoryValidTo(ctx, sqlc.SetStatusHistoryValidToParams{ID: cur.ID, ValidTo: ptr(since.AddDays(-1))}); err != nil {
			return err
		}
	} else if err := tx.Q.DeleteStatusHistory(ctx, cur.ID); err != nil {
		return err
	}
	if err := tx.Q.InsertStatusHistory(ctx, sqlc.InsertStatusHistoryParams{
		PropertyID: propertyID, Status: status, ValidFrom: date.Max(since, cur.ValidFrom), ContractID: contractID,
		Reason: trim(&reason), CreatedBy: actorID(ctx), Now: s.clock.Now(),
	}); err != nil {
		return err
	}
	return tx.Q.SetPropertyStatus(ctx, sqlc.SetPropertyStatusParams{ID: propertyID, Status: status})
}

// release — объект освобождается со дня после фактического конца договора, если он «Сдан» по этому договору;
// если с этого дня начинается другой действующий договор — объект «Сдан» по нему.
func (s *Service) release(ctx context.Context, tx *db.Tx, c sqlc.Contract) error {
	cur, err := tx.Q.CurrentStatusRow(ctx, c.PropertyID)
	if err != nil {
		return err
	}
	if cur.Status != propRented || cur.ContractID == nil || *cur.ContractID != c.ID {
		return nil
	}
	day := actualEnd(c).AddDays(1)
	next, err := tx.Q.ContractStartingOn(ctx, sqlc.ContractStartingOnParams{PropertyID: c.PropertyID, OnDate: day})
	switch {
	case err == nil:
		return s.setPropertyStatus(ctx, tx, c.PropertyID, propRented, day, &next.ID, "")
	case db.IsNoRows(err):
		return s.setPropertyStatus(ctx, tx, c.PropertyID, propFree, day, nil, "Окончание договора № "+c.Number)
	default:
		return err
	}
}

// Terminate — расторжение (05 разд. 4.7): начисления после месяца последнего дня отменяются, последний месяц
// пересчитывается; расхождение «переплата» по остатку аванса подключается вместе с платежами (M6).
func (s *Service) Terminate(ctx context.Context, id uuid.UUID, last date.Date, initiator, reason string) (sqlc.Contract, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return sqlc.Contract{}, apperr.Validation(apperr.FieldError{Field: "reason", Message: "Укажите причину расторжения"})
	}
	var c sqlc.Contract
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		before, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if before.Status != StatusActive && before.Status != StatusExtended {
			return apperr.Conflict(apperr.CodeConflictState, "Расторгнуть можно только действующий договор")
		}
		if last.Before(before.StartDate) || last.After(before.EndDate) {
			return apperr.Validation(apperr.FieldError{Field: "terminationDate",
				Message: "Последний день пользования — от " + before.StartDate.Format() + " до " + before.EndDate.Format()})
		}
		if _, err := tx.Q.GetPropertyForUpdate(ctx, before.PropertyID); err != nil {
			return err
		}
		c, err = tx.Q.TerminateContract(ctx, sqlc.TerminateContractParams{
			ID: id, TerminationDate: &last, TerminationInitiator: &initiator, TerminationReason: &reason, Now: ptr(s.clock.Now()),
		})
		if err != nil {
			return dbError(err)
		}
		if err := s.billing.Terminate(ctx, tx, c); err != nil {
			return err
		}
		if _, err := tx.Q.CancelDraftAmendments(ctx, sqlc.CancelDraftAmendmentsParams{ContractID: id, CancelReason: ptr("Расторжение договора")}); err != nil {
			return err
		}
		today := s.clock.Today()
		endKey := "contract_end:" + id.String()
		if err := tx.Q.DeleteFutureAutoEvents(ctx, sqlc.DeleteFutureAutoEventsParams{ContractID: &id, AfterDate: today, KeepKey: &endKey}); err != nil {
			return err
		}
		if err := tx.Q.MoveAutoEvent(ctx, sqlc.MoveAutoEventParams{AutoKey: &endKey, StartsOn: last, Now: ptr(s.clock.Now())}); err != nil {
			return err
		}
		if !last.AddDays(1).After(today) {
			if err := s.release(ctx, tx, c); err != nil {
				return err
			}
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "contract.terminate", EntityType: "contract", EntityID: id.String(),
			Summary: fmt.Sprintf("Договор № %s расторгнут, последний день пользования %s", c.Number, last.Format()),
			Before:  map[string]any{"status": before.Status},
			After:   map[string]any{"status": c.Status, "terminationDate": last.String(), "initiator": initiator, "reason": reason},
		})
	})
	return c, err
}

// finish — завершение договора по окончании срока (05 разд. 4.8).
func (s *Service) finish(ctx context.Context, tx *db.Tx, c sqlc.Contract) (sqlc.Contract, error) {
	f, err := tx.Q.FinishContract(ctx, sqlc.FinishContractParams{ID: c.ID, Now: ptr(s.clock.Now())})
	if err != nil {
		return c, err
	}
	if err := s.release(ctx, tx, f); err != nil {
		return f, err
	}
	return f, s.audit.Record(ctx, tx.Q, audit.Entry{
		Action: "contract.finish", EntityType: "contract", EntityID: c.ID.String(),
		Summary: fmt.Sprintf("Договор № %s завершён: срок истёк %s", c.Number, c.EndDate.Format()),
		Before:  map[string]any{"status": c.Status}, After: map[string]any{"status": StatusFinished},
	})
}

// syncEvents — автоматические события календаря договора (05 разд. 13.1): окончание, предупреждения об окончании,
// регистрация, индексация. Повторный вызов обновляет даты.
func (s *Service) syncEvents(ctx context.Context, tx *db.Tx, c sqlc.Contract) error {
	if c.Status != StatusActive && c.Status != StatusExtended {
		return nil
	}
	p, err := tx.Q.GetProperty(ctx, c.PropertyID)
	if err != nil {
		return err
	}
	org, err := s.settings.Get(ctx, tx.Q)
	if err != nil {
		return err
	}
	id := c.ID.String()
	now := s.clock.Now()
	today := s.clock.Today()
	upsert := func(kind, key, title string, on date.Date) error {
		return tx.Q.UpsertAutoEvent(ctx, sqlc.UpsertAutoEventParams{
			Kind: kind, Title: title, StartsOn: on, PropertyID: &c.PropertyID, ContractID: &c.ID, AutoKey: &key, Now: now,
		})
	}
	if err := upsert("contract_end", "contract_end:"+id, fmt.Sprintf("Окончание договора %s, %s", c.Number, p.Code), c.EndDate); err != nil {
		return err
	}
	if err := tx.Q.DeleteAutoEventsByPrefix(ctx, "contract_end_notice:"+id+":"); err != nil {
		return err
	}
	title := "Предупредить нанимателя об окончании договора " + c.Number
	if c.LegalBasis == BasisCh35 && RegistrationRequired(c.StartDate, c.EndDate) {
		title += " — не позднее чем за 3 месяца предложить продление или отказ, ст. 684 ГК РФ"
	}
	for _, n := range org.ContractEndNoticeDays {
		on := c.EndDate.AddDays(-int(n))
		if on.Before(today) {
			continue
		}
		if err := upsert("contract_end_notice", fmt.Sprintf("contract_end_notice:%s:%d", id, n), title, on); err != nil {
			return err
		}
	}
	regKey := "registration:" + id
	if RegistrationRequired(c.StartDate, c.EndDate) && c.ConcludedOn != nil {
		if err := upsert("registration", regKey, fmt.Sprintf("Подать договор %s на государственную регистрацию", c.Number), c.ConcludedOn.AddDays(7)); err != nil {
			return err
		}
		if c.RegistrationDoneOn != nil {
			if err := tx.Q.SetAutoEventDone(ctx, sqlc.SetAutoEventDoneParams{AutoKey: &regKey, Done: true, DoneAt: &now}); err != nil {
				return err
			}
		}
	}
	if err := tx.Q.DeleteAutoEventsByPrefix(ctx, "indexation:"+id+":"); err != nil {
		return err
	}
	if c.IndexationEnabled && c.NextIndexationOn != nil && c.IndexationPct != nil && !c.NextIndexationOn.After(actualEnd(c)) {
		key := fmt.Sprintf("indexation:%s:%s", id, c.NextIndexationOn.String())
		pct := strings.Replace(c.IndexationPct.String(), ".", ",", 1)
		if err := upsert("indexation", key, fmt.Sprintf("Индексация платы по договору %s на %s %%", c.Number, pct), *c.NextIndexationOn); err != nil {
			return err
		}
	}
	return nil
}

// Виды актов.
const (
	ActHandover = "handover"
	ActReturn   = "return"
)

// createHandoverAct — черновик акта приёма-передачи: опись из объекта, дата = дата начала (05 разд. 4.4, п. 6).
func (s *Service) createHandoverAct(ctx context.Context, tx *db.Tx, c sqlc.Contract) error {
	items, err := tx.Q.ListInventory(ctx, c.PropertyID)
	if err != nil {
		return err
	}
	inv := make([]InventoryItem, 0, len(items))
	for _, it := range items {
		inv = append(inv, InventoryItem{Name: it.Name, Quantity: int(it.Quantity), Condition: it.Condition, Note: it.Note})
	}
	raw, err := json.Marshal(inv)
	if err != nil {
		return err
	}
	_, err = tx.Q.CreateAct(ctx, sqlc.CreateActParams{
		ContractID: c.ID, Kind: ActHandover, ActDate: c.StartDate, Inventory: raw, CreatedBy: actorID(ctx), Now: s.clock.Now(),
	})
	return err
}
