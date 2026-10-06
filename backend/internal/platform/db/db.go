// Package db — пул соединений PostgreSQL и транзакции.
// Сгенерированный sqlc-код — в подпакете sqlc (make gen-db), запросы — в backend/queries.
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	pgxdecimal "github.com/jackc/pgx-shopspring-decimal"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"landlord/backend/internal/platform/db/sqlc"
)

// Querier — общий интерфейс пула и транзакции (для sqlc.New и прямых запросов).
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// DB — пул соединений приложения.
type DB struct {
	Pool *pgxpool.Pool
}

// Open создаёт пул и проверяет соединение. Тип numeric регистрируется как shopspring/decimal.
func Open(ctx context.Context, url string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("строка подключения к БД: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
		pgxdecimal.Register(conn.TypeMap())
		return nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("подключение к БД: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("БД недоступна: %w", err)
	}
	return &DB{Pool: pool}, nil
}

// Close закрывает пул.
func (d *DB) Close() { d.Pool.Close() }

// Ping проверяет доступность БД (используется /readyz).
func (d *DB) Ping(ctx context.Context) error { return d.Pool.Ping(ctx) }

// Queries — sqlc-запросы вне транзакции (только чтение).
func (d *DB) Queries() *sqlc.Queries { return sqlc.New(d.Pool) }

// Tx — открытая транзакция: sqlc-запросы и сама pgx.Tx (для river.InsertTx и прямых запросов).
type Tx struct {
	pgx.Tx
	Q *sqlc.Queries
}

// WithTx выполняет fn в одной транзакции READ COMMITTED: при ошибке или панике — откат, иначе фиксация.
// Правило 03 Архитектура: изменяющая операция, запись audit_log и постановка задач River — в одной транзакции.
func (d *DB) WithTx(ctx context.Context, fn func(tx *Tx) error) (err error) {
	ptx, err := d.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("начало транзакции: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = ptx.Rollback(context.WithoutCancel(ctx))
			panic(p)
		}
		if err != nil {
			if rbErr := ptx.Rollback(context.WithoutCancel(ctx)); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
				err = errors.Join(err, fmt.Errorf("откат транзакции: %w", rbErr))
			}
		}
	}()
	if err = fn(&Tx{Tx: ptx, Q: sqlc.New(ptx)}); err != nil {
		return err
	}
	if err = ptx.Commit(ctx); err != nil {
		return fmt.Errorf("фиксация транзакции: %w", err)
	}
	return nil
}

// IsNoRows — запрос не вернул строк.
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// Коды ошибок PostgreSQL, которые сервисы переводят в понятные сообщения.
const (
	CodeUniqueViolation    = "23505"
	CodeExclusionViolation = "23P01"
	CodeCheckViolation     = "23514"
	CodeForeignKey         = "23503"
)

// ConstraintError возвращает код и имя ограничения, если err — нарушение ограничения БД.
func ConstraintError(err error) (code, constraint string, ok bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code, pgErr.ConstraintName, true
	}
	return "", "", false
}
