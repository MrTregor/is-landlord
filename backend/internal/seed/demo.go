package seed

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"landlord/backend/internal/billing/calc"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/jobs"
)

// ---------------------------------------------------------------- справочные данные 17 разд. 1–4

type meterSpec struct {
	typ   string
	init  string
	step  string // расход за месяц
	serNo string
}

type propertySpec struct {
	key, code, kind, address, area string
	rentKop                        int64
	fiscal                         string
	color                          color.RGBA
	meters                         []meterSpec
	inventory                      bool
}

var properties = []propertySpec{
	{"s1", "Студия 1", "residential", "г. Руза, ул. Солнечная, д. 5, кв. 12", "24.5", 2800000, "none", color.RGBA{0x7c, 0xb3, 0x42, 255},
		[]meterSpec{{"cold_water", "100", "3.5", "ХВ-1012"}, {"hot_water", "60", "2.1", "ГВ-1013"}, {"electricity", "8000", "145", "Э-1014"}}, true},
	{"s2", "Студия 2", "residential", "г. Руза, ул. Солнечная, д. 5, кв. 31", "23.8", 2650000, "none", color.RGBA{0x42, 0x8b, 0xca, 255}, nil, true},
	{"s3", "Студия 3", "residential", "г. Руза, ул. Федеративная, д. 14, кв. 7", "26.1", 2700000, "none", color.RGBA{0xe0, 0x9a, 0x3e, 255},
		[]meterSpec{{"cold_water", "210", "3.2", "ХВ-2071"}, {"hot_water", "140", "1.9", "ГВ-2072"}, {"electricity", "12000", "160", "Э-2073"}}, true},
	{"s4", "Студия 4", "residential", "г. Руза, ул. Федеративная, д. 14, кв. 22", "25.0", 3000000, "none", color.RGBA{0x9b, 0x59, 0xb6, 255}, nil, true},
	{"p1", "Помещение 1", "non_residential", "г. Руза, ул. Социалистическая, д. 3, пом. II", "48.0", 4500000, "cloud_kkt",
		color.RGBA{0x5d, 0x6d, 0x7e, 255}, []meterSpec{{"electricity", "30000", "620", "Э-3001"}}, false},
}

var inventory = []map[string]any{
	{"name": "Диван-кровать", "quantity": 1, "condition": "исправно"},
	{"name": "Шкаф", "quantity": 1, "condition": "исправно"},
	{"name": "Стол", "quantity": 1, "condition": "исправно"},
	{"name": "Стул", "quantity": 2, "condition": "исправно"},
	{"name": "Холодильник", "quantity": 1, "condition": "исправно"},
	{"name": "Электроплита", "quantity": 1, "condition": "исправно"},
	{"name": "Стиральная машина", "quantity": 1, "condition": "исправно"},
	{"name": "Кухонный гарнитур", "quantity": 1, "condition": "исправно"},
}

func person(last, first, middle, birth, doc, house, email, phone string) map[string]any {
	return map[string]any{
		"kind": "individual", "lastName": last, "firstName": first, "middleName": middle, "birthDate": birth,
		"docType": "passport_rf", "docNumber": doc, "docIssuedBy": "ГУ МВД России по г. Москве", "docIssuedOn": "2018-05-20",
		"docDivisionCode": "770-001", "registrationAddress": "г. Москва, ул. Примерная, д. " + house, "email": email, "phone": phone,
	}
}

// M — первое число месяца M+k относительно текущего.
func (w *world) M(k int) date.Date { return w.m0.AddMonths(k) }

// ---------------------------------------------------------------- сценарий

type step func() error

// demo — 17 разд. 1–6: справочники на первый день, затем по дням до сегодняшнего с ежедневными задачами.
func (w *world) demo() error {
	first := w.M(-14).AddDays(-3)
	w.at(first)
	if err := w.setup(first); err != nil {
		return err
	}
	plan := w.plan()
	if w.loadMode {
		w.loadPlan(plan)
	}
	for d := first; !d.After(w.today); d = d.AddDays(1) {
		w.at(d)
		for _, s := range plan.before[d] {
			if err := s(); err != nil {
				return err
			}
		}
		if err := w.daily(d, !d.Before(w.today.AddDays(-30))); err != nil {
			return err
		}
		for _, s := range plan.after[d] {
			if err := s(); err != nil {
				return err
			}
		}
		for _, f := range plan.every {
			if err := f(d); err != nil {
				return err
			}
		}
	}
	return w.extras()
}

// daily — ежедневные задачи 07 за день (письма — только за последний месяц, 17 разд. 5).
func (w *world) daily(d date.Date, mail bool) error {
	args := jobs.DailyArgs{BusinessDate: d, Trigger: jobs.TriggerSchedule}
	for _, r := range w.a.DailyRunners() {
		isMail := strings.HasPrefix(r.Name, "notify.")
		if (isMail && !mail) || r.Name == "pdn.retention_daily" || r.Name == "maintenance.cleanup" {
			continue
		}
		st, err := r.Run(w.ctx, args)
		if err != nil {
			return fmt.Errorf("%s: %w", r.Name, err)
		}
		if st != nil && len(st.Errors) > 0 {
			return fmt.Errorf("%s: %s", r.Name, st.Errors[0])
		}
	}
	return nil
}

type plan struct {
	before, after map[date.Date][]step
	every         []func(d date.Date) error
}

func (p *plan) on(d date.Date, s step)    { p.after[d] = append(p.after[d], s) }
func (p *plan) start(d date.Date, s step) { p.before[d] = append(p.before[d], s) }

// setup — организация, тарифы, объекты со счётчиками, описью и фото, наниматели.
func (w *world) setup(first date.Date) error {
	if _, _, err := w.a.Users.CreateOwner(w.ctx, OwnerEmail, "Александр Васильевич"); err != nil {
		return err
	}
	var pol struct {
		Version string `json:"version"`
	}
	if err := w.newClient("", "").raw(http.MethodGet, "/public/privacy-policy", nil, "", &pol); err != nil {
		return err
	}
	owner, err := w.acceptInvite(OwnerEmail, OwnerPassword, false)
	if err != nil {
		return err
	}
	w.owner, w.policy = owner, pol.Version
	if err := w.organization(); err != nil {
		return err
	}
	if err := w.tariffs(first); err != nil {
		return err
	}
	for _, p := range properties {
		id, err := w.owner.id(http.MethodPost, "/properties", map[string]any{
			"code": p.code, "kind": p.kind, "address": p.address, "areaSqm": p.area, "floor": 3, "rooms": map[bool]any{true: 0, false: nil}[p.kind == "residential"],
			"cadastralNumber": nil, "ownershipBasis": "Выписка из ЕГРН", "description": nil, "furnished": p.kind == "residential",
			"expectedRentKop": p.rentKop, "fiscalMode": p.fiscal, "initialStatus": "free", "statusSince": first.String(),
		})
		if err != nil {
			return err
		}
		w.ids[p.key] = id
		for _, m := range p.meters {
			mid, err := w.owner.id(http.MethodPost, "/properties/"+id+"/meters", map[string]any{
				"type": m.typ, "serialNumber": m.serNo, "initialValue": m.init, "initialDate": first.String(),
				"verificationDue": w.today.AddMonths(30).String(), "note": nil,
			})
			if err != nil {
				return err
			}
			w.ids[p.key+":"+m.typ] = mid
		}
		if p.inventory {
			if err := w.owner.do(http.MethodPut, "/properties/"+id+"/inventory", map[string]any{"items": inventory}, nil); err != nil {
				return err
			}
		}
		for i, shade := range []uint8{0, 40} {
			c := p.color
			c.R, c.G, c.B = c.R-min(c.R, shade), c.G-min(c.G, shade), c.B-min(c.B, shade)
			fid, err := w.owner.upload("/files", map[string]string{"category": "property_photo"}, fmt.Sprintf("%s %d.png", p.code, i+1), placeholder(c))
			if err != nil {
				return err
			}
			if err := w.owner.do(http.MethodPost, "/properties/"+id+"/photos", map[string]any{"fileId": fid, "caption": fmt.Sprintf("%s, фото %d", p.code, i+1)}, nil); err != nil {
				return err
			}
		}
	}
	tenants := map[string]map[string]any{
		"smirnova":  person("Смирнова", "Анна", "Сергеевна", "1994-03-15", "45 19 123401", "1", "smirnova@landlord.local", "+79161000001"),
		"kuznetsov": person("Кузнецов", "Павел", "Игоревич", "1991-07-02", "45 17 123402", "2", "kuznetsov@landlord.local", "+79161000002"),
		"orlova":    person("Орлова", "Мария", "Дмитриевна", "1997-11-23", "45 20 123403", "3", "orlova@landlord.local", "+79161000003"),
		"lebedev":   person("Лебедев", "Артём", "Олегович", "1989-01-30", "45 15 123404", "4", "lebedev@landlord.local", "+79161000004"),
		"romashka": {
			"kind": "legal", "orgName": "ООО «Ромашка»", "inn": "5075123457", "kpp": "507501001", "ogrn": "1235000012345",
			"legalAddress": "Московская обл., г. Руза, ул. Социалистическая, д. 3, пом. II", "signatoryName": "Иванов Иван Иванович",
			"signatoryPosition": "Директор", "signatoryBasis": "Устава", "email": "romashka@landlord.local", "phone": "+79161000005",
		},
	}
	for _, k := range []string{"smirnova", "kuznetsov", "orlova", "lebedev", "romashka"} {
		id, err := w.owner.id(http.MethodPost, "/tenants", tenants[k])
		if err != nil {
			return err
		}
		w.ids[k] = id
	}
	return nil
}

func (w *world) organization() error {
	var o map[string]any
	if err := w.owner.do(http.MethodGet, "/settings/organization", nil, &o); err != nil {
		return err
	}
	delete(o, "updatedAt")
	delete(o, "updatedBy")
	for k, v := range map[string]any{
		"fullName": "Индивидуальный предприниматель Гордейчик Александр Васильевич", "shortName": "ИП Гордейчик А.В.",
		"inn": "507500363778", "ogrnip": "317502400029263", "address": "Московская область, г. Руза",
		"phone": "+79161775782", "email": OwnerEmail, "bankName": "АО «Демо-Банк»", "bankBik": "044525999",
		"bankAccount": "40802810900000012345", "bankCorrAccount": "30101810400000000999", "signatoryName": "А.В. Гордейчик",
		"documentCity": "г. Руза", "taxRegime": "usn_income", "taxRatePct": "6",
	} {
		o[k] = v
	}
	return w.owner.do(http.MethodPut, "/settings/organization", o, nil)
}

// tariffs — 17 разд. 2; для лет до текущего — значения «с 01.01» (№ 103).
func (w *world) tariffs(first date.Date) error {
	type t struct{ typ, jan, jul string }
	list := []t{{"cold_water", "54.37", "58.20"}, {"hot_water", "248.12", "265.60"}, {"electricity", "6.43", "6.88"}}
	y := w.today.Year()
	for _, tr := range list {
		if first.Year() < y {
			if err := w.tariff(tr.typ, tr.jan, date.New(first.Year(), 1, 1)); err != nil {
				return err
			}
		}
		if err := w.tariff(tr.typ, tr.jan, date.New(y, 1, 1)); err != nil {
			return err
		}
		if err := w.tariff(tr.typ, tr.jul, date.New(y, 7, 1)); err != nil {
			return err
		}
	}
	return nil
}

func (w *world) tariff(typ, price string, from date.Date) error {
	return w.owner.do(http.MethodPost, "/settings/tariffs", map[string]any{"meterType": typ, "propertyId": nil, "price": price,
		"effectiveFrom": from.String(), "note": "Условный тариф (демо)"}, nil)
}

// placeholder — изображение-заглушка 640×400 однотонного цвета.
func placeholder(c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 640, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 640; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// ---------------------------------------------------------------- договоры

type contractSpec struct {
	key, property, tenant string
	start, end            date.Date
	number                int
	body                  map[string]any
}

// seq — следующий номер договора года будет n (номера 17 разд. 4).
func (w *world) seq(year, n int) error {
	_, err := w.a.DB.Pool.Exec(w.ctx, `INSERT INTO number_sequences (name, year, last_value) VALUES ('contract', $1, $2)
		ON CONFLICT (name, year) DO UPDATE SET last_value = $2`, year, n-1)
	return err
}

func (w *world) activate(c contractSpec) error {
	body := map[string]any{
		"propertyId": w.ids[c.property], "tenantId": w.ids[c.tenant], "startDate": c.start.String(), "endDate": c.end.String(),
		"penaltyEnabled": false, "penaltyGraceDays": 0, "penaltyOnUtilities": false, "indexationEnabled": false,
		"indexationPeriodMonths": 12, "utilitiesFixedKop": 0, "occupants": []any{},
	}
	for k, v := range c.body {
		body[k] = v
	}
	if err := w.seq(w.day.Year(), c.number); err != nil {
		return err
	}
	id, err := w.owner.id(http.MethodPost, "/contracts", body)
	if err != nil {
		return err
	}
	w.ids[c.key] = id
	return w.owner.do(http.MethodPost, "/contracts/"+id+"/activate", map[string]any{"concludedOn": c.start.String(), "inviteTenant": false}, nil)
}

// cabinet — приглашение нанимателя и вход в кабинет с паролем демо.
func (w *world) cabinet(tenant, email string) (*client, error) {
	if err := w.owner.do(http.MethodPost, "/tenants/"+w.ids[tenant]+"/invite", nil, nil); err != nil {
		return nil, err
	}
	return w.acceptInvite(email, TenantPassword, true)
}

// open — открытые начисления договора с остатком и сроком не позже d (кроме начисляемых пеней).
func (w *world) open(contract string, d date.Date) ([]sqlc.ListChargesRow, error) {
	cid := uuid.MustParse(w.ids[contract])
	rows, err := w.a.DB.Queries().ListCharges(w.ctx, sqlc.ListChargesParams{ContractID: &cid, Kinds: []string{},
		Statuses: []string{"open", "partially_paid"}, Today: d, DueTo: &d, Lim: 1000})
	if err != nil {
		return nil, err
	}
	out := rows[:0]
	for _, r := range rows {
		if r.DueDate != nil && r.AmountKop > r.PaidKop && (r.PenaltyState == nil || *r.PenaltyState != "accruing") {
			out = append(out, r)
		}
	}
	return out, nil
}

func dueOn(rows []sqlc.ListChargesRow, d date.Date) bool {
	for _, r := range rows {
		if *r.DueDate == d {
			return true
		}
	}
	return false
}

func total(rows []sqlc.ListChargesRow) int64 {
	var s int64
	for _, r := range rows {
		s += r.AmountKop - r.PaidKop
	}
	return s
}

// pay — платёж собственника по начислениям (ids пусто — по порядку распределения).
func (w *world) pay(contract string, amount int64, method string, ids []string, on date.Date, comment string) error {
	if ids == nil {
		ids = []string{}
	}
	return w.owner.do(http.MethodPost, "/payments", map[string]any{"contractId": w.ids[contract], "purpose": "charges",
		"amountKop": amount, "receivedOn": on.String(), "method": method, "chargeIds": ids, "comment": nilIfEmpty(comment)}, nil)
}

func (w *world) deposit(contract string, amount int64, method string) error {
	return w.owner.do(http.MethodPost, "/payments", map[string]any{"contractId": w.ids[contract], "purpose": "deposit",
		"amountKop": amount, "receivedOn": w.day.String(), "method": method, "chargeIds": []string{}, "comment": nil}, nil)
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// payDue — в день срока оплатить всё, что к этому дню наступило (способ — method).
func (w *world) payDue(contract, method string) func(date.Date) error {
	return func(d date.Date) error {
		if _, ok := w.ids[contract]; !ok {
			return nil
		}
		rows, err := w.open(contract, d)
		if err != nil || !dueOn(rows, d) {
			return err
		}
		return w.pay(contract, total(rows), method, nil, d, "")
	}
}

// reading — значение счётчика на n-й месяц от начала: начальное + n × расход.
func reading(p propertySpec, typ string, n int) string {
	for _, m := range p.meters {
		if m.typ == typ {
			init, step := decimal.RequireFromString(m.init), decimal.RequireFromString(m.step)
			return init.Add(step.Mul(decimal.NewFromInt(int64(n)))).StringFixed(3)
		}
	}
	return ""
}

func spec(key string) propertySpec {
	for _, p := range properties {
		if p.key == key {
			return p
		}
	}
	panic(key)
}

// submitReadings — показания нанимателя за месяц m через кабинет.
func (w *world) submitReadings(tenant *client, contract, prop string, m date.Date, n int) error {
	list := []map[string]any{}
	for _, mt := range spec(prop).meters {
		list = append(list, map[string]any{"meterId": w.ids[prop+":"+mt.typ], "value": reading(spec(prop), mt.typ, n)})
	}
	return tenant.do(http.MethodPost, "/cabinet/meter-readings", map[string]any{"contractId": w.ids[contract], "periodMonth": m.String(),
		"readings": list}, nil)
}

// acceptReadings — собственник принимает показания договора на проверке.
func (w *world) acceptReadings(contract string) error {
	rows, err := w.a.DB.Pool.Query(w.ctx, `SELECT id FROM meter_readings WHERE contract_id = $1 AND status = 'submitted' ORDER BY period_month`,
		w.ids[contract])
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id.String())
	}
	rows.Close()
	for _, id := range ids {
		if err := w.owner.do(http.MethodPost, "/meter-readings/"+id+"/accept", nil, nil); err != nil {
			return err
		}
	}
	return nil
}

// plan — события договоров по дням (17 разд. 4, 5).
func (w *world) plan() *plan {
	p := &plan{before: map[date.Date][]step{}, after: map[date.Date][]step{}}
	var smirnova, kuznetsov, romashka *client

	// Смирнова — Студия 1, найм ГГГГ-001, по счётчикам, онлайн в день срока, показания каждый месяц.
	s1 := contractSpec{key: "c1", property: "s1", tenant: "smirnova", start: w.M(-7).AddDays(9), number: 1, body: map[string]any{
		"rentKop": 2800000, "paymentDay": 10, "depositKop": 2800000, "utilitiesMode": "meters", "penaltyEnabled": true,
		"penaltyRatePct": "0.1", "penaltyGraceDays": 3}}
	s1.end = s1.start.AddMonths(11).AddDays(-1)
	// Кузнецов — Студия 2, найм ГГГГ-002, 2 года, ЖКУ фиксированно 1 500, индексация 5 % раз в 12 мес.
	s2 := contractSpec{key: "c2", property: "s2", tenant: "kuznetsov", start: w.M(-7).AddDays(4), number: 2, body: map[string]any{
		"rentKop": 2650000, "paymentDay": 5, "depositKop": 2650000, "utilitiesMode": "fixed", "utilitiesFixedKop": 150000,
		"penaltyEnabled": true, "penaltyRatePct": "0.1", "penaltyGraceDays": 3, "indexationEnabled": true, "indexationPct": "5",
		"indexationPeriodMonths": 12}}
	s2.end = s2.start.AddMonths(24).AddDays(-1)
	// Орлова — Студия 3, (ГГГГ−1)-004, с M−14 по последний день M−3, завершён.
	s3 := contractSpec{key: "c3", property: "s3", tenant: "orlova", start: w.M(-14), end: w.M(-2).AddDays(-1), number: 4, body: map[string]any{
		"rentKop": 2700000, "paymentDay": 1, "depositKop": 2700000, "utilitiesMode": "meters"}}
	// Лебедев — Студия 4, ГГГГ-003, с 20-го M−3, ЖКУ включены, наличными в срок.
	s4 := contractSpec{key: "c4", property: "s4", tenant: "lebedev", start: w.M(-3).AddDays(19), number: 3, body: map[string]any{
		"rentKop": 3000000, "paymentDay": 20, "depositKop": 3000000, "utilitiesMode": "included"}}
	s4.end = s4.start.AddMonths(11).AddDays(-1)
	// ООО «Ромашка» — Помещение 1, аренда ГГГГ-005, 2 года, электроэнергия по счётчику.
	s5 := contractSpec{key: "c5", property: "p1", tenant: "romashka", start: w.M(-5), number: 5, body: map[string]any{
		"rentKop": 4500000, "paymentDay": 1, "depositKop": 4500000, "utilitiesMode": "meters", "penaltyEnabled": true,
		"penaltyRatePct": "0.1", "penaltyGraceDays": 5}}
	s5.end = s5.start.AddMonths(24).AddDays(-1)

	// Орлова.
	p.start(s3.start, func() error {
		if err := w.activate(s3); err != nil {
			return err
		}
		return w.deposit("c3", 2700000, "cash")
	})
	for m, n := s3.start, 1; m.Before(w.M(-3)); m, n = m.AddMonths(1), n+1 {
		month, idx := m, n
		p.start(m.AddMonths(1), func() error {
			for _, mt := range spec("s3").meters {
				if err := w.owner.do(http.MethodPost, "/meter-readings", map[string]any{"meterId": w.ids["s3:"+mt.typ], "periodMonth": month.String(),
					"readingDate": month.LastOfMonth().String(), "value": reading(spec("s3"), mt.typ, idx)}, nil); err != nil {
					return err
				}
			}
			return nil
		})
	}
	p.on(s3.end, func() error { return w.returnAct(s3) })
	p.on(s3.end.AddDays(2), func() error { return w.brokenDoor() })
	p.on(s3.end.AddDays(3), func() error {
		return w.owner.do(http.MethodPost, "/contracts/"+w.ids["c3"]+"/deposit/refunds", map[string]any{"amountKop": 2400000,
			"opDate": w.day.String(), "method": "bank_transfer", "reason": "Возврат обеспечительного платежа за вычетом удержания"}, nil)
	})
	p.every = append(p.every, func(d date.Date) error {
		if d.After(s3.end.AddDays(3)) {
			return nil
		}
		return w.payDue("c3", "bank_transfer")(d)
	})

	// Смирнова.
	p.start(s1.start, func() error {
		if err := w.activate(s1); err != nil {
			return err
		}
		if err := w.owner.do(http.MethodPost, "/tenants/"+w.ids["smirnova"]+"/consents", map[string]any{"givenOn": s1.start.String()}, nil); err != nil {
			return err
		}
		var err error
		if smirnova, err = w.cabinet("smirnova", "smirnova@landlord.local"); err != nil {
			return err
		}
		return w.payOnline(smirnova, w.ids["c1"], "deposit")
	})
	for m, n := s1.start.FirstOfMonth(), 1; m.Before(w.m0); m, n = m.AddMonths(1), n+1 {
		month, idx, last := m, n, m == w.M(-1)
		p.on(month.AddDays(24), func() error { return w.submitReadings(smirnova, "c1", "s1", month, idx) })
		if !last {
			p.on(month.AddDays(25), func() error { return w.acceptReadings("c1") })
		}
	}
	p.every = append(p.every, func(d date.Date) error {
		if smirnova == nil {
			return nil
		}
		rows, err := w.open("c1", d)
		if err != nil || !dueOn(rows, d) {
			return err
		}
		return w.payOnline(smirnova, w.ids["c1"], "charges")
	})

	// Кузнецов: переводом с опозданием 4–9 дней; ЖКУ прошлого месяца не оплачены; текущий месяц — 15 000.
	p.start(s2.start, func() error {
		if err := w.activate(s2); err != nil {
			return err
		}
		if err := w.deposit("c2", 2650000, "bank_transfer"); err != nil {
			return err
		}
		var err error
		kuznetsov, err = w.cabinet("kuznetsov", "kuznetsov@landlord.local")
		return err
	})
	p.on(s2.start.AddDays(10), func() error {
		return w.owner.do(http.MethodPatch, "/contracts/"+w.ids["c2"], map[string]any{"registrationDoneOn": w.day.String(),
			"registrationNumber": "50:19:0000000-" + w.day.Format()[:2], "reason": "Регистрация договора найма"}, nil)
	})
	for m, k := s2.start.FirstOfMonth(), 0; !m.After(w.m0); m, k = m.AddMonths(1), k+1 {
		due := m.AddDays(4)
		payOn := due.AddDays(4 + (k*5)%6)
		month := m
		if month == w.m0 {
			payOn = date.Min(due.AddDays(4), w.today)
		}
		if payOn.After(w.today) {
			continue
		}
		p.on(payOn, func() error {
			rows, err := w.open("c2", w.day)
			if err != nil {
				return err
			}
			switch month {
			case w.M(-1), w.m0:
				for _, r := range rows {
					if r.Kind == "rent" && r.PeriodMonth != nil && *r.PeriodMonth == month {
						amount := r.AmountKop - r.PaidKop
						if month == w.m0 {
							amount = min(amount, 1500000)
						}
						return w.pay("c2", amount, "bank_transfer", []string{r.ID.String()}, w.day, "")
					}
				}
				return nil
			default:
				if len(rows) == 0 {
					return nil
				}
				return w.pay("c2", total(rows), "bank_transfer", nil, w.day, "")
			}
		})
	}

	// Ромашка: переводом в срок; за прошлый месяц — из выписки банка; показания электроэнергии через кабинет.
	p.start(s5.start, func() error {
		if err := w.activate(s5); err != nil {
			return err
		}
		if err := w.deposit("c5", 4500000, "bank_transfer"); err != nil {
			return err
		}
		var err error
		romashka, err = w.cabinet("romashka", "romashka@landlord.local")
		return err
	})
	for m, n := s5.start, 1; m.Before(w.m0); m, n = m.AddMonths(1), n+1 {
		month, idx := m, n
		p.on(month.AddDays(24), func() error { return w.submitReadings(romashka, "c5", "p1", month, idx) })
		p.on(month.AddDays(25), func() error { return w.acceptReadings("c5") })
	}
	p.every = append(p.every, func(d date.Date) error {
		if _, ok := w.ids["c5"]; !ok {
			return nil
		}
		rows, err := w.open("c5", d)
		if err != nil || !dueOn(rows, d) {
			return err
		}
		if d == w.M(-1) {
			w.statLine = &pendingStatement{on: d, amount: total(rows)}
			return nil
		}
		return w.pay("c5", total(rows), "bank_transfer", nil, d, "")
	})
	p.on(date.Min(w.m0.AddDays(1), w.today), func() error { return w.statement() })

	// Лебедев: наличными в срок; последний платёж 31 000 — переплата 1 000.
	p.start(s4.start, func() error {
		if err := w.activate(s4); err != nil {
			return err
		}
		return w.deposit("c4", 3000000, "cash")
	})
	lastDue := w.M(-3).AddDays(19)
	for d := lastDue; !d.After(w.today); d = d.AddMonths(1) {
		lastDue = d
	}
	p.every = append(p.every, func(d date.Date) error {
		if _, ok := w.ids["c4"]; !ok {
			return nil
		}
		rows, err := w.open("c4", d)
		if err != nil || !dueOn(rows, d) {
			return err
		}
		amount, comment := total(rows), ""
		if d == lastDue {
			amount, comment = amount+100000, "Внесено 31 000 наличными"
		}
		return w.pay("c4", amount, "cash", nil, d, comment)
	})

	// Заявки (17 разд. 5).
	p.on(w.today.AddDays(-4), func() error { return w.oven() })
	p.on(w.today.AddDays(-1), func() error {
		fid, err := kuznetsov.upload("/files", map[string]string{"category": "request_photo"}, "смеситель.png", placeholder(color.RGBA{0x99, 0xaa, 0xbb, 255}))
		if err != nil {
			return err
		}
		return kuznetsov.do(http.MethodPost, "/cabinet/requests", map[string]any{"contractId": w.ids["c2"], "category": "plumbing",
			"title": "Течёт смеситель на кухне", "description": "Капает из-под рычага, под мойкой лужа", "photoFileIds": []string{fid}}, nil)
	})
	return p
}

// returnAct — акт возврата Студии 3 с показаниями и повреждением, подписание (итоговое начисление ЖКУ).
func (w *world) returnAct(c contractSpec) error {
	id, err := w.owner.id(http.MethodPost, "/contracts/"+w.ids[c.key]+"/acts", map[string]any{"kind": "return"})
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(inventory))
	for _, it := range inventory {
		cp := map[string]any{}
		for k, v := range it {
			cp[k] = v
		}
		if it["name"] == "Шкаф" {
			cp["condition"] = "сломана дверца"
		}
		items = append(items, cp)
	}
	months := 0
	for m := c.start; m.Before(w.M(-3)); m = m.AddMonths(1) {
		months++
	}
	readings := []map[string]any{}
	for _, mt := range spec("s3").meters {
		readings = append(readings, map[string]any{"meterId": w.ids["s3:"+mt.typ], "value": reading(spec("s3"), mt.typ, months+1)})
	}
	if err := w.owner.do(http.MethodPut, "/acts/"+id, map[string]any{"actDate": w.day.String(), "inventory": items, "keysCount": 2,
		"conditionNotes": "Помещение в удовлетворительном состоянии", "damages": "Сломана дверца шкафа", "readings": readings}, nil); err != nil {
		return err
	}
	return w.owner.do(http.MethodPost, "/acts/"+id+"/sign", map[string]any{"signedOn": w.day.String()}, nil)
}

// brokenDoor — заявка «Сломана дверца шкафа» по вине нанимателя, затрата 3 000 с удержанием из залога.
func (w *world) brokenDoor() error {
	rid, err := w.owner.id(http.MethodPost, "/maintenance-requests", map[string]any{"propertyId": w.ids["s3"], "contractId": w.ids["c3"],
		"category": "furniture", "title": "Сломана дверца шкафа", "description": "Обнаружено при приёмке по акту возврата",
		"priority": "normal", "photoFileIds": []string{}})
	if err != nil {
		return err
	}
	if err := w.owner.do(http.MethodPatch, "/maintenance-requests/"+rid, map[string]any{"tenantFault": true, "contractor": "Сервис-Руза"}, nil); err != nil {
		return err
	}
	for _, tr := range []map[string]any{{"toStatus": "in_progress"}, {"toStatus": "done", "resultNote": "Дверца заменена"},
		{"toStatus": "closed"}} {
		if err := w.owner.do(http.MethodPost, "/maintenance-requests/"+rid+"/transition", tr, nil); err != nil {
			return err
		}
	}
	return w.owner.do(http.MethodPost, "/expenses", map[string]any{"propertyId": w.ids["s3"], "category": "repair", "amountKop": 300000,
		"paidOn": w.day.String(), "periodMonth": w.day.FirstOfMonth().String(), "description": "Замена дверцы шкафа",
		"counterparty": "Сервис-Руза", "maintenanceRequestId": rid, "recovery": "deposit", "contractId": w.ids["c3"]}, nil)
}

// oven — заявка «Не работает духовка» в работе у «Сервис-Руза», плановая дата через 3 дня.
func (w *world) oven() error {
	rid, err := w.owner.id(http.MethodPost, "/maintenance-requests", map[string]any{"propertyId": w.ids["s1"], "contractId": w.ids["c1"],
		"category": "appliance", "title": "Не работает духовка", "description": "Духовой шкаф не нагревается, конфорки работают",
		"priority": "normal", "photoFileIds": []string{}})
	if err != nil {
		return err
	}
	if err := w.owner.do(http.MethodPost, "/maintenance-requests/"+rid+"/transition", map[string]any{"toStatus": "in_progress"}, nil); err != nil {
		return err
	}
	return w.owner.do(http.MethodPatch, "/maintenance-requests/"+rid, map[string]any{"contractor": "Сервис-Руза",
		"plannedDate": w.today.AddDays(3).String()}, nil)
}

// ---------------------------------------------------------------- выписка банка

type pendingStatement struct {
	on     date.Date
	amount int64
}

// statement — выписка CSV за прошлый месяц: платёж ООО «Ромашка» (по ИНН) и неопознанный «Иванова Т. В.».
func (w *world) statement() error {
	var b strings.Builder
	b.WriteString("Дата;Сумма;Плательщик;ИНН плательщика;Счёт плательщика;Назначение;Номер документа\n")
	if s := w.statLine; s != nil {
		var number string
		if err := w.a.DB.Pool.QueryRow(w.ctx, `SELECT number FROM contracts WHERE id = $1`, w.ids["c5"]).Scan(&number); err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s;%s;ООО «Ромашка»;5075123457;40702810400000000002;Арендная плата за %s по договору № %s. Без НДС;318\n",
			s.on.Format(), fmt.Sprintf("%d,%02d", s.amount/100, s.amount%100), calc.MonthName(s.on), number)
	}
	fmt.Fprintf(&b, "%s;12 000,00;Иванова Т. В.;;40817810300000000004;за квартиру;4410\n", w.M(-1).AddDays(11).Format())
	_, err := w.owner.upload("/bank-statements", nil, "Выписка за "+calc.MonthName(w.M(-1))+".csv", []byte(b.String()))
	return err
}

// ---------------------------------------------------------------- календарь и затраты

func (w *world) extras() error {
	w.at(w.today)
	sat := w.today.AddDays((6 - int(w.today.Weekday()) + 7) % 7)
	events := []map[string]any{
		{"kind": "showing", "title": "Показ Студии 3", "startsOn": sat.String(), "startTime": "12:00", "endTime": "12:30",
			"propertyId": w.ids["s3"]},
		{"kind": "repair", "title": "Плановая замена фильтров воды — Студия 1", "startsOn": w.today.AddDays(10).String(),
			"propertyId": w.ids["s1"]},
	}
	for _, e := range events {
		if err := w.owner.do(http.MethodPost, "/calendar/events", e, nil); err != nil {
			return err
		}
	}
	type exp struct {
		prop, category, description, counterparty string
		kop                                       int64
		daysAgo                                   int
	}
	for _, e := range []exp{
		{"s1", "maintenance", "Уборка квартиры", "Клининг-Руза", 350000, 150},
		{"s1", "repair", "Замена смесителя в ванной", "Сервис-Руза", 420000, 60},
		{"s2", "repair", "Замена смесителя на кухне", "Сервис-Руза", 420000, 40},
		{"s2", "tax", "Налог на имущество", "ФНС России", 280000, 100},
		{"s3", "maintenance", "Уборка после выезда нанимателя", "Клининг-Руза", 350000, 45},
		{"s4", "furniture_appliances", "Покупка электрочайника", "Магазин «Техника»", 250000, 80},
		{"s4", "maintenance", "Уборка квартиры", "Клининг-Руза", 350000, 20},
		{"p1", "insurance", "Страхование помещения", "АО «Демо-Страхование»", 600000, 120},
		{"p1", "commission", "Комиссия банка за обслуживание счёта", "АО «Демо-Банк»", 90000, 30},
	} {
		on := w.today.AddDays(-e.daysAgo)
		if err := w.owner.do(http.MethodPost, "/expenses", map[string]any{"propertyId": w.ids[e.prop], "category": e.category,
			"amountKop": e.kop, "paidOn": on.String(), "periodMonth": on.FirstOfMonth().String(), "description": e.description,
			"counterparty": e.counterparty, "recovery": "none"}, nil); err != nil {
			return err
		}
	}
	return nil
}
