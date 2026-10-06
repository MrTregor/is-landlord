// Package clock — единственный источник текущего времени в системе.
// Вызов time.Now() вне этого пакета запрещён линтером (forbidigo): бизнес-логика получает
// время через интерфейс Clock, что позволяет тестам и демонстрационному режиму подменять «сегодня».
package clock

import (
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
