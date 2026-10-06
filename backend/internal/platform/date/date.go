// Package date — бизнес-дата (год, месяц, день) без времени и часового пояса.
// Все бизнес-даты системы (сроки оплаты, периоды, даты поступления) — значения этого типа;
// «сегодня» получается только через clock.Clock.Today().
package date

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// Layout — формат обмена датами в API и БД (YYYY-MM-DD).
const Layout = "2006-01-02"

// Date — календарная дата. Нулевое значение (IsZero) означает «дата не задана».
type Date struct {
	year  int
	month time.Month
	day   int
}

// New создаёт дату; переполнение нормализуется так же, как в time.Date (31 апреля → 1 мая).
func New(year int, month time.Month, day int) Date {
	return FromTime(time.Date(year, month, day, 0, 0, 0, 0, time.UTC))
}

// FromTime возвращает календарную дату момента t в его собственном часовом поясе.
func FromTime(t time.Time) Date {
	y, m, d := t.Date()
	return Date{year: y, month: m, day: d}
}

// In возвращает календарную дату момента t в часовом поясе loc.
func In(t time.Time, loc *time.Location) Date {
	return FromTime(t.In(loc))
}

// Parse разбирает строку YYYY-MM-DD.
func Parse(s string) (Date, error) {
	t, err := time.Parse(Layout, s)
	if err != nil {
		return Date{}, fmt.Errorf("неверная дата %q: ожидается ГГГГ-ММ-ДД", s)
	}
	return FromTime(t), nil
}

// MustParse — Parse для констант в тестах; при ошибке паникует.
func MustParse(s string) Date {
	d, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}

func (d Date) Year() int             { return d.year }
func (d Date) Month() time.Month     { return d.month }
func (d Date) Day() int              { return d.day }
func (d Date) IsZero() bool          { return d.year == 0 && d.month == 0 && d.day == 0 }
func (d Date) Weekday() time.Weekday { return d.Time().Weekday() }

// Time — полночь даты в UTC (для арифметики и хранения в pgtype.Date).
func (d Date) Time() time.Time {
	return time.Date(d.year, d.month, d.day, 0, 0, 0, 0, time.UTC)
}

// String — YYYY-MM-DD; для нулевой даты — пустая строка.
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.year, int(d.month), d.day)
}

// Format — ДД.ММ.ГГГГ (вывод в документах и письмах).
func (d Date) Format() string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%02d.%02d.%04d", d.day, int(d.month), d.year)
}

// AddDays прибавляет n дней (n может быть отрицательным).
func (d Date) AddDays(n int) Date { return FromTime(d.Time().AddDate(0, 0, n)) }

// AddMonths прибавляет n месяцев; если в целевом месяце нет такого дня, берётся последний день месяца
// (31.01 + 1 мес. → 28/29.02), а не переполнение, как в time.AddDate.
func (d Date) AddMonths(n int) Date {
	first := time.Date(d.year, d.month, 1, 0, 0, 0, 0, time.UTC).AddDate(0, n, 0)
	y, m, _ := first.Date()
	day := min(d.day, DaysIn(y, m))
	return Date{year: y, month: m, day: day}
}

// FirstOfMonth — первое число месяца даты (period_month).
func (d Date) FirstOfMonth() Date { return Date{year: d.year, month: d.month, day: 1} }

// LastOfMonth — последний день месяца даты.
func (d Date) LastOfMonth() Date {
	return Date{year: d.year, month: d.month, day: DaysIn(d.year, d.month)}
}

// DaysInMonth — число дней в месяце даты.
func (d Date) DaysInMonth() int { return DaysIn(d.year, d.month) }

// DaysIn — число дней в месяце.
func DaysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// Sub — число дней от other до d (d − other).
func (d Date) Sub(other Date) int {
	return int(d.Time().Sub(other.Time()).Hours() / 24)
}

// DaysInclusive — число дней в периоде [from, to] включительно.
func DaysInclusive(from, to Date) int { return to.Sub(from) + 1 }

// Compare возвращает −1, 0, +1.
func (d Date) Compare(other Date) int {
	switch {
	case d.Before(other):
		return -1
	case d.After(other):
		return 1
	default:
		return 0
	}
}

func (d Date) Before(other Date) bool { return d.Time().Before(other.Time()) }
func (d Date) After(other Date) bool  { return d.Time().After(other.Time()) }
func (d Date) Equal(other Date) bool  { return d == other }

// Min и Max — меньшая и большая из двух дат.
func Min(a, b Date) Date {
	if a.Before(b) {
		return a
	}
	return b
}

func Max(a, b Date) Date {
	if a.After(b) {
		return a
	}
	return b
}

// MarshalJSON — строка "YYYY-MM-DD", нулевая дата — null.
func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(d.String())
}

// UnmarshalJSON принимает "YYYY-MM-DD" или null.
func (d *Date) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*d = Date{}
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := Parse(s)
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// MarshalText и UnmarshalText — для параметров запросов и форматов, работающих с текстом.
func (d Date) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

func (d *Date) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*d = Date{}
		return nil
	}
	v, err := Parse(string(b))
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// Value — для database/sql; нулевая дата — NULL.
func (d Date) Value() (driver.Value, error) {
	if d.IsZero() {
		return nil, nil
	}
	return d.Time(), nil
}

// Scan — из time.Time или строки.
func (d *Date) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*d = Date{}
		return nil
	case time.Time:
		*d = FromTime(v)
		return nil
	case string:
		p, err := Parse(v)
		if err != nil {
			return err
		}
		*d = p
		return nil
	default:
		return fmt.Errorf("date: неподдерживаемый тип %T", src)
	}
}
