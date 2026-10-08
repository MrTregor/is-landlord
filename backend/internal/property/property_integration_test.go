//go:build integration

package property_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/testsupport"
)

func TestMain(m *testing.M) { testsupport.Main(m) }

func studio(code string) map[string]any {
	return map[string]any{
		"code": code, "kind": "residential", "address": "г. Руза, ул. Солнечная, д. 5, кв. 12", "areaSqm": "24.50",
		"floor": 3, "rooms": 0, "cadastralNumber": "50:19:0010101:123", "ownershipBasis": "Выписка из ЕГРН от 12.03.2019",
		"furnished": true, "description": nil, "expectedRentKop": 2800000, "fiscalMode": "none",
		"initialStatus": "free", "statusSince": "2026-01-01",
	}
}

type detail struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Status      string `json:"status"`
	StatusSince string `json:"statusSince"`
	AreaSqm     string `json:"areaSqm"`
	ArchivedAt  *string
	Photos      []struct{ FileID string } `json:"photos"`
}

// ТК-01, ТК-02: объект с обязательными полями, смена статуса и история (FR-01.1, FR-01.2).
func TestPropertyCreateAndStatus_FR01_1_FR01_2(t *testing.T) {
	env := testsupport.NewEnv(t, "2026-10-16")
	c := env.ActiveOwner(t, "prop@landlord.local", "Пароль-объекты-1")

	r := c.Do(http.MethodPost, "/properties", studio("Студия 5"))
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var p detail
	r.JSON(t, &p)
	assert.Equal(t, "free", p.Status)
	assert.Equal(t, "2026-01-01", p.StatusSince)
	assert.Equal(t, "24.50", p.AreaSqm)

	// Код уникален.
	r = c.Do(http.MethodPost, "/properties", studio("Студия 5"))
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, string(r.Body), "уже есть")

	// Нежилое с числом комнат — ошибка поля.
	bad := studio("Помещение 9")
	bad["kind"] = "non_residential"
	r = c.Do(http.MethodPost, "/properties", bad)
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)

	// «Сдан» вручную не ставится.
	r = c.Do(http.MethodPost, "/properties/"+p.ID+"/status", map[string]any{"status": "rented", "since": "2026-10-16"})
	assert.Equal(t, http.StatusBadRequest, r.Status, "«Сдан» вне перечисления ручных статусов")

	r = c.Do(http.MethodPost, "/properties/"+p.ID+"/status", map[string]any{"status": "repair", "since": "2026-10-17"})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status, "дата в будущем")

	r = c.Do(http.MethodPost, "/properties/"+p.ID+"/status", map[string]any{"status": "repair", "since": "2026-10-16", "reason": "Замена труб"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &p)
	assert.Equal(t, "repair", p.Status)

	var hist []struct {
		Status, ValidFrom string
		ValidTo           *string
		Reason            *string
	}
	c.Do(http.MethodGet, "/properties/"+p.ID+"/status-history", nil).JSON(t, &hist)
	require.Len(t, hist, 2)
	assert.Equal(t, "repair", hist[0].Status)
	assert.Equal(t, "2026-10-16", hist[0].ValidFrom)
	assert.Nil(t, hist[0].ValidTo)
	assert.Equal(t, "free", hist[1].Status)
	require.NotNil(t, hist[1].ValidTo)
	assert.Equal(t, "2026-10-15", *hist[1].ValidTo, "запись «Свободен» закрыта днём раньше")

	// Тот же день — запись заменяется, а не добавляется.
	r = c.Do(http.MethodPost, "/properties/"+p.ID+"/status", map[string]any{"status": "free", "since": "2026-10-16"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	c.Do(http.MethodGet, "/properties/"+p.ID+"/status-history", nil).JSON(t, &hist)
	assert.Len(t, hist, 2)
	assert.Equal(t, "free", hist[0].Status)

	// Дата раньше начала текущей записи — ошибка.
	r = c.Do(http.MethodPost, "/properties/"+p.ID+"/status", map[string]any{"status": "reserved", "since": "2026-10-01"})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)

	// Архив и возврат.
	r = c.Do(http.MethodPost, "/properties/"+p.ID+"/archive", nil)
	require.Equal(t, http.StatusOK, r.Status)
	r.JSON(t, &p)
	assert.NotNil(t, p.ArchivedAt)
	var list struct{ Total int }
	c.Do(http.MethodGet, "/properties", nil).JSON(t, &list)
	assert.Equal(t, 0, list.Total, "архивные скрыты по умолчанию")
	c.Do(http.MethodGet, "/properties?archived=true", nil).JSON(t, &list)
	assert.Equal(t, 1, list.Total)
	require.Equal(t, http.StatusOK, c.Do(http.MethodPost, "/properties/"+p.ID+"/unarchive", nil).Status)

	var n int
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE entity_type = 'property' AND entity_id = $1`, p.ID).Scan(&n))
	assert.Equal(t, 5, n, "создание, 2 смены статуса, архив и возврат — каждое действие в журнале")
}

// Опись, фото и права на файлы (FR-01.1; 12 Безопасность и ПДн, разделы 3 и 9).
func TestInventoryPhotosFiles_FR01_1(t *testing.T) {
	env := testsupport.NewEnv(t, "2026-10-16")
	c := env.ActiveOwner(t, "inv@landlord.local", "Пароль-описи-1")
	id := c.Do(http.MethodPost, "/properties", studio("Студия 6")).ID(t)

	r := c.Do(http.MethodPut, "/properties/"+id+"/inventory", map[string]any{"items": []map[string]any{
		{"name": "Диван-кровать", "quantity": 1, "condition": "исправно"},
		{"name": "Стул", "quantity": 2, "condition": "исправно", "note": "деревянные"},
	}})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var inv struct {
		Items []struct {
			ID, Name string
			Quantity int
		}
	}
	c.Do(http.MethodGet, "/properties/"+id+"/inventory", nil).JSON(t, &inv)
	require.Len(t, inv.Items, 2)
	assert.Equal(t, "Диван-кровать", inv.Items[0].Name)
	assert.Equal(t, 2, inv.Items[1].Quantity)

	// Загрузка: SVG и PDF вместо фото отклоняются, PNG принимается.
	r = c.Upload("property_photo", "x.png", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`))
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	r = c.Upload("property_photo", "doc.pdf", []byte("%PDF-1.4 test"))
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	r = c.Upload("property_photo", "фото комнаты.png", testsupport.PNG)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	fileID := r.ID(t)

	require.Equal(t, http.StatusCreated, c.Do(http.MethodPost, "/properties/"+id+"/photos", map[string]any{"fileId": fileID}).Status)
	var p detail
	c.Do(http.MethodGet, "/properties/"+id, nil).JSON(t, &p)
	require.Len(t, p.Photos, 1)

	r = c.Do(http.MethodGet, "/files/"+fileID, nil)
	require.Equal(t, http.StatusOK, r.Status)
	assert.Equal(t, "image/png", r.Header.Get("Content-Type"))
	assert.Contains(t, r.Header.Get("Content-Disposition"), "filename*=UTF-8''")
	assert.Equal(t, testsupport.PNG, r.Body)

	// Наниматель без договора по объекту фото не видит, а грузить может только фото заявок.
	tenant := env.ActiveTenant(t, "files-tenant@landlord.local", "Пароль-наним-1")
	assert.Equal(t, http.StatusNotFound, tenant.Do(http.MethodGet, "/files/"+fileID, nil).Status)
	assert.Equal(t, http.StatusForbidden, tenant.Upload("property_photo", "a.png", testsupport.PNG).Status)
	own := tenant.Upload("request_photo", "течь.png", testsupport.PNG)
	require.Equal(t, http.StatusCreated, own.Status, string(own.Body))
	assert.Equal(t, http.StatusOK, tenant.Do(http.MethodGet, "/files/"+own.ID(t), nil).Status, "свою загрузку видно")

	require.Equal(t, http.StatusNoContent, c.Do(http.MethodDelete, "/properties/"+id+"/photos/"+fileID, nil).Status)
	c.Do(http.MethodGet, "/properties/"+id, nil).JSON(t, &p)
	assert.Empty(t, p.Photos)
}

// Счётчики и тарифы (FR-01.1, FR-05.5): событие поверки в календаре, уникальность тарифа.
func TestMetersAndTariffs_FR05_5(t *testing.T) {
	ctx := context.Background()
	env := testsupport.NewEnv(t, "2026-10-16")
	c := env.ActiveOwner(t, "meter@landlord.local", "Пароль-счётч-1")
	id := c.Do(http.MethodPost, "/properties", studio("Студия 7")).ID(t)

	r := c.Do(http.MethodPost, "/properties/"+id+"/meters", map[string]any{
		"type": "cold_water", "serialNumber": "ХВ-123", "initialValue": "100.000", "initialDate": "2026-01-01",
		"verificationDue": "2027-05-01", "note": nil,
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	meterID := r.ID(t)

	var title, starts string
	require.NoError(t, testsupport.Owner(t).QueryRow(ctx,
		`SELECT title, starts_on::text FROM calendar_events WHERE auto_key = $1`, "meter_verification:"+meterID).Scan(&title, &starts))
	assert.Equal(t, "Поверка счётчика: холодная вода, Студия 7", title)
	assert.Equal(t, "2027-05-01", starts)

	r = c.Do(http.MethodPost, "/properties/"+id+"/meters", map[string]any{
		"type": "electricity", "serialNumber": nil, "initialValue": "1.2345", "initialDate": "2026-01-01", "verificationDue": nil, "note": nil,
	})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status, "не более 3 знаков после запятой")

	var meters []struct {
		ID, Type, InitialValue string
		LastReading            any
	}
	c.Do(http.MethodGet, "/properties/"+id+"/meters", nil).JSON(t, &meters)
	require.Len(t, meters, 1)
	assert.Equal(t, "100.000", meters[0].InitialValue)
	assert.Nil(t, meters[0].LastReading)

	require.Equal(t, http.StatusOK, c.Do(http.MethodPost, "/meters/"+meterID+"/archive", nil).Status)
	var n int
	require.NoError(t, testsupport.Owner(t).QueryRow(ctx, `SELECT count(*) FROM calendar_events WHERE auto_key = $1`,
		"meter_verification:"+meterID).Scan(&n))
	assert.Equal(t, 0, n, "у архивного счётчика события поверки нет")

	// Тарифы.
	r = c.Do(http.MethodPost, "/settings/tariffs", map[string]any{"meterType": "cold_water", "propertyId": nil, "price": "54.37", "effectiveFrom": "2026-01-01", "note": nil})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	tariffID := r.ID(t)
	r = c.Do(http.MethodPost, "/settings/tariffs", map[string]any{"meterType": "cold_water", "propertyId": nil, "price": "60", "effectiveFrom": "2026-01-01", "note": nil})
	assert.Equal(t, http.StatusConflict, r.Status)
	r = c.Do(http.MethodPost, "/settings/tariffs", map[string]any{"meterType": "cold_water", "propertyId": id, "price": "58.2", "effectiveFrom": "2026-07-01", "note": "по объекту"})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))

	var tariffs []struct {
		Price        string
		PropertyCode *string
	}
	c.Do(http.MethodGet, "/settings/tariffs?meterType=cold_water", nil).JSON(t, &tariffs)
	require.Len(t, tariffs, 2)
	assert.Equal(t, "54.3700", tariffs[0].Price)
	assert.Nil(t, tariffs[0].PropertyCode)
	assert.Equal(t, "Студия 7", *tariffs[1].PropertyCode)

	assert.Equal(t, http.StatusNoContent, c.Do(http.MethodDelete, "/settings/tariffs/"+tariffID, nil).Status)
}
