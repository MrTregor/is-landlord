//go:build integration

package report_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"landlord/backend/internal/testsupport"
)

func TestMain(m *testing.M) { testsupport.Main(m) }

const password = "Пароль-отчёты-1"

var seq int

func nextSeq() int { seq++; return seq }

type world struct {
	env *testsupport.Env
	c   *testsupport.Client
	sfx string
}

func setup(t *testing.T, sfx string) world {
	t.Helper()
	env := testsupport.NewEnv(t, "2026-10-15")
	c := env.ActiveOwner(t, "reports-"+sfx+"@landlord.local", password)
	var s map[string]any
	c.Do(http.MethodGet, "/settings/organization", nil).JSON(t, &s)
	s["fullName"], s["shortName"], s["inn"], s["address"] = "Индивидуальный предприниматель Гордейчик Александр Васильевич",
		"ИП Гордейчик А.В.", "507500363778", "Московская область, г. Руза"
	s["email"], s["signatoryName"], s["taxRegime"], s["taxRatePct"] = "owner@landlord.local", "А.В. Гордейчик", "usn_income", "6.00"
	delete(s, "updatedAt")
	delete(s, "updatedBy")
	r := c.Do(http.MethodPut, "/settings/organization", s)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	return world{env: env, c: c, sfx: sfx}
}

// lease — объект (свободен с since), наниматель и договор с даты start; возвращает объект и договор.
func (w world) lease(t *testing.T, code, since, start string, rent int64) (string, string) {
	t.Helper()
	r := w.c.Do(http.MethodPost, "/properties", map[string]any{
		"code": code + " " + w.sfx, "kind": "residential", "address": "г. Руза, ул. Солнечная, д. 9, кв. " + code + w.sfx,
		"areaSqm": "30.00", "floor": 1, "rooms": 1, "cadastralNumber": nil, "ownershipBasis": nil, "description": nil, "furnished": false,
		"expectedRentKop": rent, "fiscalMode": "none", "initialStatus": "free", "statusSince": since,
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	prop := r.ID(t)
	r = w.c.Do(http.MethodPost, "/tenants", map[string]any{
		"kind": "individual", "lastName": "Лебедев", "firstName": "Артём", "middleName": "Олегович", "docType": "passport_rf",
		"docNumber": "45 18 777888", "registrationAddress": "г. Москва, ул. Примерная, д. 5", "email": fmt.Sprintf("lebedev-%s-%d@landlord.local", w.sfx, nextSeq()),
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	r = w.c.Do(http.MethodPost, "/contracts", map[string]any{
		"propertyId": prop, "tenantId": r.ID(t), "startDate": start, "endDate": "2027-08-31", "rentKop": rent, "paymentDay": 1,
		"depositKop": 3000000, "utilitiesMode": "included", "utilitiesFixedKop": 0, "penaltyEnabled": false, "penaltyGraceDays": 0,
		"penaltyOnUtilities": false, "indexationEnabled": false, "indexationPeriodMonths": 12, "occupants": []any{},
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	contract := r.ID(t)
	r = w.c.Do(http.MethodPost, "/contracts/"+contract+"/activate", map[string]any{"concludedOn": start, "inviteTenant": false})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	return prop, contract
}

func (w world) pay(t *testing.T, contract, purpose string, amount int64, on string) string {
	t.Helper()
	r := w.c.Do(http.MethodPost, "/payments", map[string]any{
		"contractId": contract, "purpose": purpose, "amountKop": amount, "receivedOn": on, "method": "cash",
		"chargeIds": []string{}, "comment": nil,
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	return r.ID(t)
}

type taxReport struct {
	Periods []struct {
		Label      string `json:"label"`
		Cumulative bool   `json:"cumulative"`
		RentKop    int64  `json:"rentKop"`
		AdvanceKop int64  `json:"advanceKop"`
		Withheld   int64  `json:"depositWithheldKop"`
		RefundsKop int64  `json:"refundsKop"`
		ToDeposit  int64  `json:"toDepositKop"`
		IncomeKop  int64  `json:"incomeKop"`
		TaxBaseKop int64  `json:"taxBaseKop"`
		TaxKop     *int64 `json:"taxKop"`
	} `json:"periods"`
	Operations []struct {
		Kind      string `json:"kind"`
		AmountKop int64  `json:"amountKop"`
		Document  string `json:"document"`
	} `json:"operations"`
	RegimeTitle         string `json:"regimeTitle"`
	DepositsReceivedKop int64  `json:"depositsReceivedKop"`
}

// IT-17, ТК-27: налоговая база за год совпадает с суммой, посчитанной SQL-запросом напрямую; виды поступлений,
// налог 6 % с округлением до рубля, без двойного учёта переноса в залог (FR-11.3, FR-05.8).
func TestTaxBase_IT17_TK27_FR_11_3(t *testing.T) {
	ctx := context.Background()
	w := setup(t, "tax")
	_, contract := w.lease(t, "Студия 1", "2026-01-01", "2026-03-01", 3000000)
	w.pay(t, contract, "deposit", 3000000, "2026-03-01")
	w.pay(t, contract, "charges", 3000000, "2026-03-02")
	// Март–октябрь начислено 8 × 30 000; второй платёж гасит апрель–октябрь, остаток 40 000,50 — аванс.
	adv := w.pay(t, contract, "charges", 25000050, "2026-04-03")
	r := w.c.Do(http.MethodPost, "/payments/"+adv+"/refunds", map[string]any{"amountKop": 50000, "refundedOn": "2026-07-06",
		"method": "cash", "reason": "Переплата"})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	r = w.c.Do(http.MethodPost, "/contracts/"+contract+"/deposit/withholdings", map[string]any{"amountKop": 300000, "opDate": "2026-08-10",
		"reason": "Сломана дверца шкафа"})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))

	var rep taxReport
	r = w.c.Do(http.MethodGet, "/reports/tax-base?year=2026", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &rep)
	require.Len(t, rep.Periods, 8)
	year := rep.Periods[7]
	assert.Equal(t, "год", year.Label)
	assert.True(t, year.Cumulative)

	// Тот же доход прямым SQL (05 разд. 12.2): платежи «за начисления» + удержания − возвраты − переносы в залог.
	var direct int64
	require.NoError(t, testsupport.Owner(t).QueryRow(ctx, `
		SELECT (SELECT COALESCE(sum(amount_kop), 0) FROM payments WHERE status = 'succeeded' AND purpose = 'charges'
		          AND received_on BETWEEN '2026-01-01' AND '2026-12-31')
		     + (SELECT COALESCE(sum(amount_kop), 0) FROM deposit_transactions WHERE kind = 'withholding' AND cancelled_at IS NULL
		          AND op_date BETWEEN '2026-01-01' AND '2026-12-31')
		     - (SELECT COALESCE(sum(amount_kop), 0) FROM refunds WHERE refunded_on BETWEEN '2026-01-01' AND '2026-12-31')
		     - (SELECT COALESCE(sum(amount_kop), 0) FROM deposit_transactions WHERE kind = 'receipt' AND from_advance
		          AND cancelled_at IS NULL AND op_date BETWEEN '2026-01-01' AND '2026-12-31')`).Scan(&direct))
	assert.Equal(t, direct, year.IncomeKop, "налоговая база совпадает с прямым расчётом")
	assert.Equal(t, year.IncomeKop, year.TaxBaseKop)
	assert.Equal(t, int64(-50000), year.RefundsKop)
	assert.Equal(t, int64(300000), year.Withheld)
	require.NotNil(t, year.TaxKop)
	// 6 % от дохода, округление до рубля половиной вверх.
	want := (year.IncomeKop*6 + 5000) / 10000 * 100
	assert.Equal(t, want, *year.TaxKop)
	assert.Equal(t, int64(3000000), rep.DepositsReceivedKop, "залог — справочно, не доход")
	assert.Equal(t, "Режим: УСН «доходы», ставка 6 %", rep.RegimeTitle)
	// Квартал I + II + III + IV = год.
	var sum int64
	for _, p := range rep.Periods[:4] {
		sum += p.IncomeKop
	}
	assert.Equal(t, year.IncomeKop, sum)

	// Перенос переплаты в залог уменьшает доход и не учитывается дважды.
	r = w.c.Do(http.MethodPost, "/payments/"+adv+"/move-to-deposit", map[string]any{"amountKop": 100000, "opDate": "2026-09-01"})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var rep2 taxReport
	w.c.Do(http.MethodGet, "/reports/tax-base?year=2026", nil).JSON(t, &rep2)
	assert.Equal(t, year.IncomeKop-100000, rep2.Periods[7].IncomeKop)
	assert.Equal(t, int64(-100000), rep2.Periods[7].ToDeposit)
	assert.Equal(t, year.AdvanceKop, rep2.Periods[7].AdvanceKop, "аванс платежа остаётся в доходе периода поступления")

	var book struct {
		Rows []struct {
			RowType     string  `json:"rowType"`
			Description string  `json:"description"`
			Document    *string `json:"document"`
			IncomeKop   int64   `json:"incomeKop"`
		} `json:"rows"`
	}
	r = w.c.Do(http.MethodGet, "/reports/income-book?year=2026", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &book)
	last := book.Rows[len(book.Rows)-1]
	assert.Equal(t, "year_total", last.RowType)
	assert.Contains(t, book.Rows[0].Description, "Плата за наём за март 2026 по договору №")
	require.NotNil(t, book.Rows[0].Document)
	assert.Contains(t, *book.Rows[0].Document, "02.03.2026, квитанция КВ-2026-")
}

// ТК-17: заполняемость за месяц с простоем — дни простоя и недополученный доход по формуле (FR-05.7).
func TestOccupancy_TK17_FR_05_7(t *testing.T) {
	w := setup(t, "occ")
	prop, _ := w.lease(t, "Студия 3", "2026-09-01", "2026-10-12", 3100000)
	var rep struct {
		Rows []struct {
			PropertyID    string  `json:"propertyId"`
			PeriodDays    int     `json:"periodDays"`
			RentedDays    int     `json:"rentedDays"`
			FreeDays      int     `json:"freeDays"`
			OccupancyPct  *string `json:"occupancyPct"`
			LostIncomeKop int64   `json:"lostIncomeKop"`
		} `json:"rows"`
		IdlePeriods []struct {
			PropertyID string `json:"propertyId"`
			Status     string `json:"status"`
			From       string `json:"from"`
			To         string `json:"to"`
			Days       int    `json:"days"`
		} `json:"idlePeriods"`
	}
	r := w.c.Do(http.MethodGet, "/reports/occupancy?from=2026-10-01&to=2026-10-31", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &rep)
	found := false
	for _, x := range rep.Rows {
		if x.PropertyID != prop {
			continue
		}
		found = true
		assert.Equal(t, 15, x.PeriodDays, "период обрезан сегодняшним днём")
		assert.Equal(t, 11, x.FreeDays)
		assert.Equal(t, 4, x.RentedDays)
		require.NotNil(t, x.OccupancyPct)
		assert.Equal(t, "26.7", *x.OccupancyPct)
		assert.Equal(t, int64(1100000), x.LostIncomeKop, "31 000 × 11 / 31")
	}
	assert.True(t, found)
	for _, p := range rep.IdlePeriods {
		if p.PropertyID == prop {
			assert.Equal(t, "free", p.Status)
			assert.Equal(t, "2026-10-01", p.From)
			assert.Equal(t, "2026-10-11", p.To)
			assert.Equal(t, 11, p.Days)
		}
	}
	var prof struct {
		RentedDays   int     `json:"rentedDays"`
		IdleDays     int     `json:"idleDays"`
		OccupancyPct *string `json:"occupancyPct"`
		ByMonth      []struct {
			Month string `json:"month"`
		} `json:"byMonth"`
	}
	r = w.c.Do(http.MethodGet, "/properties/"+prop+"/profitability?from=2026-10-01&to=2026-10-31", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &prof)
	assert.Equal(t, 4, prof.RentedDays)
	assert.Equal(t, 11, prof.IdleDays)
	require.Len(t, prof.ByMonth, 1)
	assert.Equal(t, http.StatusUnprocessableEntity, w.c.Do(http.MethodGet, "/reports/occupancy?from=2026-10-31&to=2026-10-01", nil).Status)
}

// Задолженность по возрасту долга и случаи просрочки (FR-11.2); сводка (FR-14).
func TestDebtsAndDashboard_FR_11_2(t *testing.T) {
	w := setup(t, "debt")
	_, contract := w.lease(t, "Студия 4", "2026-01-01", "2026-07-01", 2000000)
	w.pay(t, contract, "charges", 2000000, "2026-07-01")
	w.pay(t, contract, "charges", 2000000, "2026-08-20") // август оплачен с опозданием
	var rep struct {
		Rows []struct {
			Contract struct {
				ID string `json:"id"`
			} `json:"contract"`
			NotDueKop     int64 `json:"notDueKop"`
			Days1to30Kop  int64 `json:"days1to30Kop"`
			Days31to60Kop int64 `json:"days31to60Kop"`
			TotalDebtKop  int64 `json:"totalDebtKop"`
		} `json:"rows"`
		Overdue struct {
			Count int `json:"count"`
			Rows  []struct {
				Contract struct {
					ID string `json:"id"`
				} `json:"contract"`
				DueDate     string  `json:"dueDate"`
				PaidOffOn   *string `json:"paidOffOn"`
				OverdueDays int     `json:"overdueDays"`
			} `json:"rows"`
		} `json:"overdue"`
	}
	r := w.c.Do(http.MethodGet, "/reports/debts?asOf=2026-10-15", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &rep)
	var row *struct {
		Contract struct {
			ID string `json:"id"`
		} `json:"contract"`
		NotDueKop     int64 `json:"notDueKop"`
		Days1to30Kop  int64 `json:"days1to30Kop"`
		Days31to60Kop int64 `json:"days31to60Kop"`
		TotalDebtKop  int64 `json:"totalDebtKop"`
	}
	for i := range rep.Rows {
		if rep.Rows[i].Contract.ID == contract {
			row = &rep.Rows[i]
		}
	}
	require.NotNil(t, row)
	assert.Equal(t, int64(2000000), row.Days1to30Kop, "октябрь: срок 01.10, 14 дн.")
	assert.Equal(t, int64(2000000), row.Days31to60Kop, "сентябрь: срок 01.09, 44 дн.")
	assert.GreaterOrEqual(t, row.TotalDebtKop, int64(4000000))
	// Просрочки: август (погашен 20.08 — 19 дн.), сентябрь и октябрь — не погашены.
	got := map[string]int{}
	for _, c := range rep.Overdue.Rows {
		if c.Contract.ID == contract {
			got[c.DueDate] = c.OverdueDays
		}
	}
	assert.Equal(t, 19, got["2026-08-01"])
	assert.Equal(t, 44, got["2026-09-01"])
	assert.Equal(t, 14, got["2026-10-01"])
	_, ok := got["2026-07-01"]
	assert.False(t, ok, "июль оплачен в срок")

	var dash struct {
		IncomeByMonth []struct {
			Month     string `json:"month"`
			AmountKop int64  `json:"amountKop"`
		} `json:"incomeByMonth"`
		DebtTotalKop     int64  `json:"debtTotalKop"`
		OverdueKop       int64  `json:"overdueKop"`
		OverdueContracts int    `json:"overdueContracts"`
		OccupancyPct     string `json:"occupancyPct"`
		Properties       []struct {
			ContractID *string `json:"contractId"`
			DebtKop    int64   `json:"debtKop"`
		} `json:"properties"`
	}
	r = w.c.Do(http.MethodGet, "/dashboard", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &dash)
	require.Len(t, dash.IncomeByMonth, 12)
	assert.Equal(t, "2026-10-01", dash.IncomeByMonth[11].Month)
	assert.GreaterOrEqual(t, dash.OverdueKop, int64(4000000))
	assert.GreaterOrEqual(t, dash.OverdueContracts, 1)
	found := false
	for _, p := range dash.Properties {
		if p.ContractID != nil && *p.ContractID == contract {
			found = true
			assert.Equal(t, int64(4000000), p.DebtKop)
		}
	}
	assert.True(t, found)
}

// IT-18: XLSX каждого отчёта и выгрузки открывается excelize заново, заголовки и итоги на месте (FR-11.4, NFR-Comp-01).
func TestXLSX_IT18_FR_11_4(t *testing.T) {
	w := setup(t, "xlsx")
	prop, contract := w.lease(t, "Помещение", "2026-01-01", "2026-06-01", 4500000)
	w.pay(t, contract, "charges", 4500000, "2026-06-02")
	r := w.c.Do(http.MethodPost, "/expenses", map[string]any{
		"propertyId": prop, "category": "repair", "amountKop": 350000, "paidOn": "2026-06-10", "periodMonth": "2026-06-01",
		"description": "Уборка", "counterparty": nil, "fileId": nil, "maintenanceRequestId": nil, "recovery": "none",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	cases := []struct {
		path, file  string
		sheets      []string
		title, head string
		total       bool
	}{
		{"/reports/income-expense.xlsx?from=2026-01-01&to=2026-12-31", "Доходы и расходы", []string{"По объектам", "По месяцам"},
			"Доходы и расходы по объектам", "Объект", true},
		{"/reports/occupancy.xlsx?from=2026-01-01&to=2026-10-15", "Заполняемость", []string{"Заполняемость", "Периоды простоя"},
			"Заполняемость и простои", "Объект", true},
		{"/reports/debts.xlsx?asOf=2026-10-15", "Задолженность на 15.10.2026", []string{"Задолженность", "Просрочки"},
			"Задолженность по нанимателям", "Наниматель", true},
		{"/reports/tax-base.xlsx?year=2026", "Налоговая база 2026", []string{"Сводка", "Операции"}, "Налоговая база", "Период", false},
		{"/reports/income-book.xlsx?year=2026", "Книга учёта доходов 2026", []string{"Раздел I"},
			"Доходы и расходы для книги учёта (раздел I)", "№ п/п", false},
		{"/charges.xlsx?contractId=" + contract, "Начисления на", []string{"Начисления"}, "Начисления", "Срок оплаты", true},
		{"/payments.xlsx?contractId=" + contract, "Платежи на", []string{"Платежи"}, "Платежи", "Дата поступления", true},
		{"/expenses.xlsx?propertyId=" + prop, "Затраты на", []string{"Затраты"}, "Затраты", "Дата оплаты", true},
		{"/audit.xlsx?entityType=expense", "Журнал действий на", []string{"Журнал действий"}, "Журнал действий", "Дата и время (МСК)", false},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			r := w.c.Do(http.MethodGet, tc.path, nil)
			require.Equal(t, http.StatusOK, r.Status, string(r.Body))
			assert.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", r.Header.Get("Content-Type"))
			assert.Contains(t, r.Header.Get("Content-Disposition"), "attachment")
			f, err := excelize.OpenReader(bytes.NewReader(r.Body))
			require.NoError(t, err)
			defer func() { _ = f.Close() }()
			assert.Equal(t, tc.sheets, f.GetSheetList())
			sh := tc.sheets[0]
			title, _ := f.GetCellValue(sh, "A1")
			assert.Equal(t, tc.title, title)
			sub, _ := f.GetCellValue(sh, "A2")
			assert.Contains(t, sub, "Сформирован ")
			rows, err := f.GetRows(sh)
			require.NoError(t, err)
			headAt := -1
			for i, row := range rows {
				if len(row) > 0 && row[0] == tc.head {
					headAt = i
					break
				}
			}
			require.GreaterOrEqual(t, headAt, 3, "заголовки — после названия, подзаголовка и пустой строки")
			if tc.total {
				var labels []string
				for _, row := range rows[headAt+1:] {
					if len(row) > 0 {
						labels = append(labels, row[0])
					}
				}
				joined := strings.Join(labels, "|")
				assert.Contains(t, joined, "Итого")
				assert.Contains(t, joined, "Итого по видимым строкам")
			}
		})
	}
	tenant := w.env.ActiveTenant(t, "reports-tenant-xlsx@landlord.local", password)
	assert.Equal(t, http.StatusForbidden, tenant.Do(http.MethodGet, "/reports/tax-base?year=2026", nil).Status)
	assert.Equal(t, http.StatusForbidden, tenant.Do(http.MethodGet, "/dashboard", nil).Status)
}
