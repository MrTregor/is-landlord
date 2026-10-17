package audit

import (
	"context"

	"landlord/backend/internal/api"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/xlsx"
)

// exportMax — предел строк выгрузки журнала (11 разд. 7).
const exportMax = 50000

var roleTitles = map[string]string{"owner": "Собственник", "tenant": "Наниматель", "system": "Система"}

// ExportAudit — GET /audit.xlsx с фильтрами списка: дата и время (МСК), пользователь, роль, действие, сущность,
// описание, значения до и после (JSON, ПДн уже замаскированы при записи), IP-адрес. Больше 50 000 строк — 422.
func (h *Handler) ExportAudit(ctx context.Context, req api.ExportAuditRequestObject) (api.ExportAuditResponseObject, error) {
	p := req.Params
	f := Filter{ActorUserID: p.ActorUserId, EntityType: apiconv.TrimPtr(p.EntityType), EntityID: apiconv.TrimPtr(p.EntityId),
		Action: apiconv.TrimPtr(p.Action), Q: apiconv.TrimPtr(p.Q), Limit: exportMax + 1}
	loc := h.clock.Location()
	if p.From != nil {
		f.From = apiconv.Ptr(apiconv.DayStart(apiconv.ToDate(*p.From), loc))
	}
	if p.To != nil {
		f.To = apiconv.Ptr(apiconv.DayStart(apiconv.ToDate(*p.To).AddDays(1), loc))
	}
	rows, total, err := h.svc.List(ctx, f)
	if err != nil {
		return nil, err
	}
	if total > exportMax {
		return nil, apperr.BusinessRule("Сузьте период выгрузки")
	}
	data := make([][]any, 0, len(rows))
	for _, r := range rows {
		ip := ""
		if r.Ip != nil {
			ip = r.Ip.String()
		}
		entity := r.EntityType
		if r.EntityID != "" {
			entity += " " + r.EntityID
		}
		data = append(data, []any{r.OccurredAt.In(loc), deref(r.ActorName), roleTitles[r.ActorRole], ActionTitle(r.Action), entity,
			r.Summary, string(r.Before), string(r.After), ip})
	}
	file, err := xlsx.Build(xlsx.Sheet{
		Name: "Журнал действий", Title: "Журнал действий", Subtitle: xlsx.Subtitle("", h.clock.Now().In(loc), ""),
		Columns: []xlsx.Column{{Title: "Дата и время (МСК)", Width: 17, Kind: xlsx.DateTime}, {Title: "Пользователь", Width: 22},
			{Title: "Роль", Width: 13}, {Title: "Действие", Width: 26}, {Title: "Сущность", Width: 30}, {Title: "Описание", Width: 50},
			{Title: "Значения до", Width: 40}, {Title: "Значения после", Width: 40}, {Title: "IP-адрес", Width: 15}},
		Rows: data,
	})
	if err != nil {
		return nil, err
	}
	return api.ExportAudit200ApplicationvndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse{
		XlsxApplicationvndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse: xlsx.Response(
			xlsx.FileName("Журнал действий на", h.clock.Today().Format()), file)}, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
