// Package apiconv — общие преобразования между типами API (internal/api) и доменными типами.
package apiconv

import (
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"landlord/backend/internal/platform/date"
)

// Размеры страниц списков (06 API, раздел 1).
const (
	DefaultPageSize = 25
	MaxPageSize     = 100
)

// Paging — номер страницы, размер, LIMIT и OFFSET.
type Paging struct {
	Page, PageSize int
}

// NewPaging нормализует page и pageSize.
func NewPaging(page, pageSize *int) Paging {
	p := Paging{Page: 1, PageSize: DefaultPageSize}
	if page != nil && *page > 0 {
		p.Page = *page
	}
	if pageSize != nil && *pageSize > 0 {
		p.PageSize = min(*pageSize, MaxPageSize)
	}
	return p
}

// Limit — LIMIT запроса.
func (p Paging) Limit() int32 { return int32(p.PageSize) } //nolint:gosec // не больше 100

// Offset — OFFSET запроса.
func (p Paging) Offset() int32 { return int32((p.Page - 1) * p.PageSize) } //nolint:gosec // разумные страницы

// ToDate — дата API → бизнес-дата.
func ToDate(d openapi_types.Date) date.Date { return date.FromTime(d.Time) }

// ToDatePtr — необязательная дата API → бизнес-дата.
func ToDatePtr(d *openapi_types.Date) *date.Date {
	if d == nil {
		return nil
	}
	v := ToDate(*d)
	return &v
}

// FromDate — бизнес-дата → дата API.
func FromDate(d date.Date) openapi_types.Date { return openapi_types.Date{Time: d.Time()} }

// FromDatePtr — необязательная бизнес-дата → дата API.
func FromDatePtr(d *date.Date) *openapi_types.Date {
	if d == nil || d.IsZero() {
		return nil
	}
	v := FromDate(*d)
	return &v
}

// DayStart — начало бизнес-дня в часовом поясе loc (для фильтров по моментам времени).
func DayStart(d date.Date, loc *time.Location) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc).UTC()
}

// TrimPtr — строка без пробелов по краям; пустая → nil.
func TrimPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}

// Ptr — указатель на значение.
func Ptr[T any](v T) *T { return &v }
