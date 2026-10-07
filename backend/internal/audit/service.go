// Package audit — неизменяемый журнал действий с цепочкой хешей (FR-12, NFR-Sec-04).
// Запись делается в транзакции изменяющего действия; персональные данные в журнал не попадают.
package audit

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/reqctx"
)

// Entry — запись журнала. Before и After — любые значения, сериализуемые в JSON; ПДн маскируются.
type Entry struct {
	Action     string
	EntityType string
	EntityID   string
	// Summary — описание на русском без персональных данных: номера договоров, коды объектов,
	// наниматель — словом «наниматель».
	Summary string
	Before  any
	After   any
	// ActorUserID и ActorRole — если действие выполнено не от имени пользователя запроса
	// (например, неудачный вход или приглашение по команде cli). По умолчанию — из контекста.
	ActorUserID *uuid.UUID
	ActorRole   string
}

// Service — запись и чтение журнала.
type Service struct {
	db    *db.DB
	clock clock.Clock
}

// NewService создаёт сервис журнала.
func NewService(database *db.DB, clk clock.Clock) *Service { return &Service{db: database, clock: clk} }

// Record пишет запись в журнал в транзакции действия.
func (s *Service) Record(ctx context.Context, q *sqlc.Queries, e Entry) error {
	before, err := Mask(e.Before)
	if err != nil {
		return fmt.Errorf("журнал: значения «до»: %w", err)
	}
	after, err := Mask(e.After)
	if err != nil {
		return fmt.Errorf("журнал: значения «после»: %w", err)
	}
	actorID, role := e.ActorUserID, e.ActorRole
	if a := reqctx.ActorFrom(ctx); a != nil {
		if actorID == nil {
			actorID = &a.UserID
		}
		if role == "" {
			role = a.Role
		}
	}
	if role == "" {
		role = reqctx.RoleSystem
	}
	meta := reqctx.MetaFrom(ctx)
	params := sqlc.InsertAuditParams{
		OccurredAt:  s.clock.Now(),
		ActorUserID: actorID,
		ActorRole:   role,
		Action:      e.Action,
		EntityType:  e.EntityType,
		EntityID:    e.EntityID,
		Summary:     e.Summary,
		Before:      before,
		After:       after,
		Ip:          meta.IP,
	}
	if meta.UserAgent != "" {
		params.UserAgent = &meta.UserAgent
	}
	if meta.RequestID != "" {
		params.RequestID = &meta.RequestID
	}
	_, err = q.InsertAudit(ctx, params)
	return err
}

// VerifyResult — итог проверки цепочки хешей.
type VerifyResult struct {
	OK         bool
	Checked    int64
	BrokenAtID *int64
}

var zeroHash = make([]byte, 32)

// Verify проверяет цепочку: хеш каждой записи совпадает с пересчитанным, prev_hash — с хешем предыдущей записи.
func (s *Service) Verify(ctx context.Context) (VerifyResult, error) {
	const batch = 5000
	q := s.db.Queries()
	var res VerifyResult
	prev := zeroHash
	var after int64
	for {
		rows, err := q.AuditChain(ctx, sqlc.AuditChainParams{AfterID: after, Lim: batch})
		if err != nil {
			return res, err
		}
		for _, r := range rows {
			res.Checked++
			if !bytes.Equal(r.PrevHash, prev) || !bytes.Equal(r.Hash, r.Computed) {
				id := r.ID
				res.BrokenAtID = &id
				return res, nil
			}
			prev = r.Hash
			after = r.ID
		}
		if len(rows) < batch {
			res.OK = true
			return res, nil
		}
	}
}

// Filter — фильтры списка журнала.
type Filter struct {
	From, To    *time.Time
	ActorUserID *uuid.UUID
	EntityType  *string
	EntityID    *string
	Action      *string
	Q           *string
	Limit       int32
	Offset      int32
}

// List — записи журнала, новые сверху, и их общее число.
func (s *Service) List(ctx context.Context, f Filter) ([]sqlc.ListAuditRow, int64, error) {
	q := s.db.Queries()
	rows, err := q.ListAudit(ctx, sqlc.ListAuditParams{
		FromAt: f.From, ToAt: f.To, ActorUserID: f.ActorUserID, EntityType: f.EntityType, EntityID: f.EntityID,
		Action: f.Action, Q: f.Q, Lim: f.Limit, Off: f.Offset,
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := q.CountAudit(ctx, sqlc.CountAuditParams{
		FromAt: f.From, ToAt: f.To, ActorUserID: f.ActorUserID, EntityType: f.EntityType, EntityID: f.EntityID,
		Action: f.Action, Q: f.Q,
	})
	return rows, total, err
}
