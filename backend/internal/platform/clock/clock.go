// Package clock — единственный источник текущего времени в системе.
// Вызов time.Now() вне этого пакета запрещён линтером (forbidigo): бизнес-логика получает
// время через интерфейс Clock, что позволяет тестам и демонстрационному режиму подменять «сегодня».
package clock

import (
	"context"
	"sync"
	"time"

	"landlord/backend/internal/platform/date"
)

// Clock возвращает текущий момент и бизнес-дату «сегодня».
type Clock interface {
	// Now — текущий момент в UTC.
	Now() time.Time
	// Today — календарная дата «сегодня» в часовом поясе бизнес-дат (TZ_BUSINESS, Europe/Moscow).
	Today() date.Date
	// Location — часовой пояс бизнес-дат.
	Location() *time.Location
}

// Real — реальное время системы.
type Real struct {
	loc *time.Location
}

// NewReal создаёт часы реального времени для часового пояса бизнес-дат.
func NewReal(loc *time.Location) *Real { return &Real{loc: loc} }

func (c *Real) Now() time.Time           { return time.Now().UTC() }
func (c *Real) Today() date.Date         { return date.In(c.Now(), c.loc) }
func (c *Real) Location() *time.Location { return c.loc }

// Fixed — часы, остановленные на заданном моменте (тесты, расчёты на дату).
type Fixed struct {
	now time.Time
	loc *time.Location
}

// NewFixed создаёт часы, всегда возвращающие момент now.
func NewFixed(now time.Time, loc *time.Location) *Fixed {
	return &Fixed{now: now.UTC(), loc: loc}
}

// NewFixedDate создаёт часы на полдень указанной бизнес-даты.
func NewFixedDate(d date.Date, loc *time.Location) *Fixed {
	return NewFixed(time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, loc), loc)
}

func (c *Fixed) Now() time.Time           { return c.now }
func (c *Fixed) Today() date.Date         { return date.In(c.now, c.loc) }
func (c *Fixed) Location() *time.Location { return c.loc }

// Set переставляет часы (тесты).
func (c *Fixed) Set(now time.Time) { c.now = now.UTC() }

// Advance сдвигает часы на d (тесты: «прошло 15 минут»).
func (c *Fixed) Advance(d time.Duration) { c.now = c.now.Add(d) }

// LoadLocation загружает часовой пояс бизнес-дат; встроенная база tzdata подключена в main-пакетах,
// поэтому работает и в образе distroless без системной базы часовых поясов.
func LoadLocation(name string) (*time.Location, error) {
	return time.LoadLocation(name)
}

// Offset — сдвинутое время демонстрационного режима (03 Архитектура, «Время и часовой пояс»): реальное время плюс
// сдвиг из таблицы demo_clock. Сдвиг перечитывается не чаще раза в 5 с, чтобы server и worker видели одно время.
type Offset struct {
	real *Real
	load func(context.Context) (int64, error)
	ttl  time.Duration

	mu       sync.Mutex
	offset   time.Duration
	loadedAt time.Time
}

// NewOffset создаёт сдвинутые часы; load читает сдвиг в секундах (нет записи — 0).
func NewOffset(loc *time.Location, load func(context.Context) (int64, error)) *Offset {
	return &Offset{real: NewReal(loc), load: load, ttl: 5 * time.Second}
}

func (c *Offset) current() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	wall := time.Now()
	if c.loadedAt.IsZero() || wall.Sub(c.loadedAt) >= c.ttl {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if s, err := c.load(ctx); err == nil {
			c.offset = time.Duration(s) * time.Second
		}
		c.loadedAt = wall
	}
	return c.offset
}

func (c *Offset) Now() time.Time           { return c.real.Now().Add(c.current()) }
func (c *Offset) Today() date.Date         { return date.In(c.Now(), c.real.loc) }
func (c *Offset) Location() *time.Location { return c.real.loc }

// RealNow — реальное время без сдвига.
func (c *Offset) RealNow() time.Time { return c.real.Now() }

// Seconds — текущий сдвиг в секундах.
func (c *Offset) Seconds() int64 { return int64(c.current() / time.Second) }

// Apply — запомнить новый сдвиг сразу после записи в базу (без ожидания перечитывания).
func (c *Offset) Apply(seconds int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset, c.loadedAt = time.Duration(seconds)*time.Second, time.Now()
}

// OffsetFor — сдвиг в секундах, при котором «сегодня» станет датой today, а время суток сохранится.
func OffsetFor(realNow time.Time, today date.Date, loc *time.Location) int64 {
	local := realNow.In(loc)
	target := time.Date(today.Year(), today.Month(), today.Day(), local.Hour(), local.Minute(), local.Second(), local.Nanosecond(), loc)
	return int64(target.Sub(local) / time.Second)
}
