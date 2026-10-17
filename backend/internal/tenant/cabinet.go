package tenant

import (
	"context"

	"github.com/google/uuid"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
)

// Действия нанимателя в профиле кабинета (15 Экраны нанимателя, раздел 8). Наниматель меняет только свой
// телефон и своё отдельное согласие; tenantID берётся из сессии.

// SetPhone — телефон из профиля кабинета (пустой — удалить, если есть e-mail).
func (s *Service) SetPhone(ctx context.Context, tenantID uuid.UUID, phone *string) (sqlc.Tenant, error) {
	phone = trim(phone)
	if phone != nil {
		p, ok := NormalizePhone(*phone)
		if !ok {
			return sqlc.Tenant{}, apperr.Validation(apperr.FieldError{Field: "phone", Message: "Телефон — 10 цифр после +7"})
		}
		phone = &p
	}
	var t sqlc.Tenant
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		before, err := s.lock(ctx, tx, tenantID)
		if err != nil {
			return err
		}
		if before.AnonymizedAt != nil {
			return apperr.NotFound()
		}
		if phone == nil && before.Email == nil {
			return apperr.Validation(apperr.FieldError{Field: "phone", Message: "Укажите телефон или e-mail"})
		}
		if t, err = tx.Q.SetTenantPhone(ctx, sqlc.SetTenantPhoneParams{ID: tenantID, Phone: phone}); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "tenant.update", EntityType: "tenant", EntityID: tenantID.String(),
			Summary: "Изменён телефон в личном кабинете: " + summaryName(t),
			Before:  map[string]any{"phone": before.Phone}, After: map[string]any{"phone": t.Phone},
		})
	})
	return t, err
}

// ActiveConsent — действующее отдельное согласие нанимателя (nil — нет).
func (s *Service) ActiveConsent(ctx context.Context, q *sqlc.Queries, tenantID uuid.UUID) (*sqlc.PdnConsent, error) {
	c, err := q.ActiveConsent(ctx, tenantID)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// GiveElectronicConsent — отдельное электронное согласие из профиля (method = electronic, IP из запроса, 12 разд. 2).
// Повтор при действующем согласии ничего не меняет.
func (s *Service) GiveElectronicConsent(ctx context.Context, tenantID uuid.UUID) error {
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		t, err := s.lock(ctx, tx, tenantID)
		if err != nil {
			return err
		}
		if t.AnonymizedAt != nil {
			return apperr.NotFound()
		}
		if t.Kind != KindIndividual {
			return apperr.BusinessRule("Согласие на обработку ПДн оформляется только для физлица")
		}
		cur, err := s.ActiveConsent(ctx, tx.Q, tenantID)
		if err != nil || cur != nil {
			return err
		}
		_, err = s.recordConsent(ctx, tx, t, s.clock.Today(), nil, nil, "electronic")
		return err
	})
}

// RevokeOwnConsent — отзыв действующего согласия нанимателем сегодняшней датой.
func (s *Service) RevokeOwnConsent(ctx context.Context, tenantID uuid.UUID) error {
	cur, err := s.ActiveConsent(ctx, s.db.Queries(), tenantID)
	if err != nil {
		return err
	}
	if cur == nil {
		return apperr.Conflict(apperr.CodeConflictState, "Согласие не дано или уже отозвано")
	}
	note := "Отозвано в личном кабинете"
	_, err = s.RevokeConsent(ctx, tenantID, cur.ID, s.clock.Today(), &note)
	return err
}
