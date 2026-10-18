package document

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
	"landlord/backend/internal/platform/names"
	"landlord/backend/internal/tenant"
)

// Data — значения переменных и HTML-блоки для подстановки.
type Data struct {
	Vars   map[string]string
	Blocks map[string]string
}

func newData() Data { return Data{Vars: map[string]string{}, Blocks: map[string]string{}} }

// orgVars — общие переменные (09 разд. 4.1).
func (s *Service) orgVars(d Data, o sqlc.OrganizationSetting) {
	v := d.Vars
	v["today"] = s.clock.Today().Format()
	v["org.full_name"], v["org.short_name"] = o.FullName, o.ShortName
	v["org.inn"], v["org.ogrnip"], v["org.address"] = o.Inn, val(o.Ogrnip), o.Address
	v["org.phone"], v["org.email"] = val(o.Phone), o.Email
	v["org.bank_name"], v["org.bank_bik"] = val(o.BankName), val(o.BankBik)
	v["org.bank_account"], v["org.bank_corr_account"] = val(o.BankAccount), val(o.BankCorrAccount)
	v["org.signatory_name"], v["org.city"] = o.SignatoryName, o.DocumentCity
	v["org.privacy_url"] = strings.TrimRight(s.publicURL, "/") + "/privacy"
	bank := ""
	if val(o.BankAccount) != "" {
		bank = "р/с " + val(o.BankAccount)
		if val(o.BankName) != "" {
			bank += " в " + val(o.BankName)
		}
		if val(o.BankBik) != "" {
			bank += ", БИК " + val(o.BankBik)
		}
		if val(o.BankCorrAccount) != "" {
			bank += ", к/с " + val(o.BankCorrAccount)
		}
	}
	ogrnip := ""
	if val(o.Ogrnip) != "" {
		ogrnip = "ОГРНИП " + val(o.Ogrnip)
	}
	phone := ""
	if val(o.Phone) != "" {
		phone = "Тел.: " + val(o.Phone)
	}
	email := ""
	if o.Email != "" {
		email = "Эл. почта: " + o.Email
	}
	v["org.requisites"] = joinLines(o.FullName, ogrnip, "ИНН "+o.Inn, o.Address, bank, phone, email)
}

// docLabel — вид документа, удостоверяющего личность.
func docLabel(docType *string) string {
	switch val(docType) {
	case "foreign_passport":
		return "паспорт иностранного гражданина"
	case "other":
		return "документ, удостоверяющий личность,"
	default:
		return "паспорт"
	}
}

var orgForms = []struct{ short, full string }{
	{"ООО ", "Общество с ограниченной ответственностью "},
	{"ПАО ", "Публичное акционерное общество "},
	{"НАО ", "Непубличное акционерное общество "},
	{"АО ", "Акционерное общество "},
	{"ЗАО ", "Закрытое акционерное общество "},
}

func orgFullName(name string) string {
	for _, f := range orgForms {
		if strings.HasPrefix(name, f.short) {
			return f.full + strings.TrimPrefix(name, f.short)
		}
	}
	return name
}

// tenantVars — переменные нанимателя (09 разд. 4.2, 4.7). ПДн расшифровываются только для документа.
func (s *Service) tenantVars(d Data, t sqlc.Tenant) error {
	v := d.Vars
	v["tenant.phone"], v["tenant.email"] = phoneText(t.Phone), val(t.Email)
	phone, email := "", ""
	if val(t.Phone) != "" {
		phone = "Тел.: " + phoneText(t.Phone)
	}
	if val(t.Email) != "" {
		email = "Эл. почта: " + val(t.Email)
	}
	if t.Kind == tenant.KindLegal {
		org := val(t.OrgName)
		v["tenant.full_name"] = org
		v["tenant.short_name"] = org
		if val(t.SignatoryName) != "" {
			v["tenant.short_name"] = names.ShortOf(val(t.SignatoryName))
		}
		ids := []string{"ИНН " + val(t.Inn)}
		if val(t.Ogrn) != "" {
			ids = append(ids, "ОГРН "+val(t.Ogrn))
		}
		intro := orgFullName(org) + " (" + strings.Join(ids, ", ") + ")"
		if val(t.SignatoryName) != "" {
			parts := strings.Fields(val(t.SignatoryName))
			acting := "действующего"
			if len(parts) > 2 && names.IsFemale(&parts[2]) {
				acting = "действующей"
			}
			intro += " в лице " + strings.TrimSpace(names.GenitivePhrase(val(t.SignatoryPosition))+" "+
				names.GenitiveFull(val(t.SignatoryName)))
			if val(t.SignatoryBasis) != "" {
				intro += ", " + acting + " на основании " + val(t.SignatoryBasis)
			}
		}
		v["tenant.party_intro"] = intro
		innKpp := "ИНН " + val(t.Inn)
		if val(t.Kpp) != "" {
			innKpp += ", КПП " + val(t.Kpp)
		}
		ogrn, addr := "", ""
		if val(t.Ogrn) != "" {
			ogrn = "ОГРН " + val(t.Ogrn)
		}
		if val(t.LegalAddress) != "" {
			addr = "Адрес: " + val(t.LegalAddress)
		}
		v["tenant.requisites"] = joinLines(org, innKpp, ogrn, addr, phone, email)
		return nil
	}
	pdn, err := s.tenants.Decrypt(t)
	if err != nil {
		return err
	}
	full := names.Full(t.LastName, t.FirstName, t.MiddleName)
	v["tenant.full_name"] = full
	v["tenant.short_name"] = names.Short(t.LastName, t.FirstName, t.MiddleName)
	female := names.IsFemale(t.MiddleName)
	citizen, registered := "гражданин", "зарегистрированный"
	if female {
		citizen, registered = "гражданка", "зарегистрированная"
	}
	if val(t.DocType) == "" || val(t.DocType) == "passport_rf" {
		citizen += " Российской Федерации"
	}
	intro := []string{citizen + " " + full}
	if t.BirthDate != nil {
		intro = append(intro, t.BirthDate.Format()+" года рождения")
	}
	docLine := ""
	if val(pdn.DocNumber) != "" {
		doc := docLabel(t.DocType) + " " + val(pdn.DocNumber)
		if val(pdn.DocIssuedBy) != "" {
			doc += ", выдан " + val(pdn.DocIssuedBy)
			if t.DocIssuedOn != nil {
				doc += " " + t.DocIssuedOn.Format()
			}
		}
		docLine = doc
		if val(pdn.DivisionCode) != "" {
			doc += ", код подразделения " + val(pdn.DivisionCode)
		}
		intro = append(intro, doc)
	}
	regLine := ""
	if val(pdn.RegAddress) != "" {
		intro = append(intro, registered+" по адресу: "+val(pdn.RegAddress))
		regLine = "Адрес регистрации: " + val(pdn.RegAddress)
	}
	v["tenant.party_intro"] = strings.Join(intro, ", ")
	if r := []rune(docLine); len(r) > 0 {
		docLine = strings.ToUpper(string(r[0])) + string(r[1:])
	}
	v["tenant.requisites"] = joinLines(full, docLine, regLine, phone, email)

	// Бланк согласия (09 разд. 4.7): документ полностью.
	docText := ""
	if val(pdn.DocNumber) != "" {
		number := val(pdn.DocNumber)
		label := docLabel(t.DocType)
		if val(t.DocType) == "" || val(t.DocType) == "passport_rf" {
			label = "паспорт гражданина Российской Федерации"
			if f := strings.Fields(number); len(f) > 1 {
				number = strings.Join(f[:len(f)-1], " ") + " № " + f[len(f)-1]
			}
		}
		parts := []string{label + " " + number}
		if val(pdn.DocIssuedBy) != "" {
			parts = append(parts, "выдан "+val(pdn.DocIssuedBy))
		}
		if t.DocIssuedOn != nil {
			parts = append(parts, "дата выдачи "+t.DocIssuedOn.Format())
		}
		if val(pdn.DivisionCode) != "" {
			parts = append(parts, "код подразделения "+val(pdn.DivisionCode))
		}
		docText = strings.Join(parts, ", ")
	}
	v["tenant.doc_text"] = docText
	v["tenant.registration_address_text"] = val(pdn.RegAddress)
	return nil
}

// contractCtx — договор с объектом и нанимателем.
type contractCtx struct {
	c      sqlc.Contract
	p      sqlc.Property
	t      sqlc.Tenant
	terms  []sqlc.ContractTerm
	org    sqlc.OrganizationSetting
	basis  string
	kindCh string // contract_ch35 / contract_ch34
}

func (s *Service) loadContract(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (contractCtx, error) {
	var cc contractCtx
	c, err := q.GetContract(ctx, id)
	if db.IsNoRows(err) {
		return cc, errNotFound
	}
	if err != nil {
		return cc, err
	}
	cc.c, cc.basis = c, c.LegalBasis
	cc.kindCh = KindContractCh35
	if c.LegalBasis == "ch34" {
		cc.kindCh = KindContractCh34
	}
	if cc.p, err = q.GetProperty(ctx, c.PropertyID); err != nil {
		return cc, err
	}
	if cc.t, err = q.GetTenant(ctx, c.TenantID); err != nil {
		return cc, err
	}
	if cc.terms, err = q.ListTerms(ctx, c.ID); err != nil {
		return cc, err
	}
	if len(cc.terms) == 0 {
		return cc, fmt.Errorf("договор %s без условий", c.Number)
	}
	cc.org, err = s.settings.Get(ctx, q)
	return cc, err
}

// originalEnd — дата окончания договора до первого продления (№ 35, № 107): prev_end_date первого подписанного
// допсоглашения о продлении; без продлений — текущая end_date.
func originalEnd(ctx context.Context, q *sqlc.Queries, c sqlc.Contract) (date.Date, error) {
	ext, err := q.FirstExtension(ctx, c.ID)
	if db.IsNoRows(err) {
		return c.EndDate, nil
	}
	if err != nil {
		return c.EndDate, err
	}
	if ext.PrevEndDate != nil {
		return *ext.PrevEndDate, nil
	}
	return ext.EffectiveFrom.AddDays(-1), nil // миграция 0007 заполнила все продления; на случай пустого значения
}

// contractData — общие, договорные переменные и блок проживающих (09 разд. 4.1, 4.2).
// Плата, день оплаты, фиксированная часть ЖКУ и срок — первоначальные (версия 1 условий, срок до продления):
// изменения оформляются допсоглашениями (№ 35).
func (s *Service) contractData(ctx context.Context, q *sqlc.Queries, cc contractCtx) (Data, error) {
	d := newData()
	s.orgVars(d, cc.org)
	if err := s.tenantVars(d, cc.t); err != nil {
		return d, err
	}
	c, p, first := cc.c, cc.p, cc.terms[0]
	end, err := originalEnd(ctx, q, c)
	if err != nil {
		return d, err
	}
	c.EndDate = end
	v := d.Vars
	v["contract.number"] = c.Number
	v["contract.title"] = contractTitle(c.LegalBasis)
	v["contract.title_dative"] = contractTitleDative(c.LegalBasis)
	v["contract.concluded_on"] = fmtDate(c.ConcludedOn)
	v["contract.concluded_on_words"] = fmtDateWords(c.ConcludedOn)
	v["contract.start_date"], v["contract.end_date"] = c.StartDate.Format(), c.EndDate.Format()
	v["contract.first_due_date"] = c.FirstDueDate.Format()
	v["contract.term_text"] = fmt.Sprintf("с %s по %s включительно", c.StartDate.Format(), c.EndDate.Format())
	v["contract.rent_amount"] = money.Kop(first.RentKop).FormatRub()
	v["contract.rent_amount_words"] = money.ToWordsRu(money.Kop(first.RentKop))
	v["contract.payment_day"] = fmt.Sprint(first.PaymentDay)
	t := contractTerms{
		basis: c.LegalBasis, start: c.StartDate, end: c.EndDate, firstDue: c.FirstDueDate, rentKop: first.RentKop,
		depositKop: c.DepositKop, utilitiesMode: c.UtilitiesMode, utilitiesFixedKop: first.UtilitiesFixedKop,
		penaltyEnabled: c.PenaltyEnabled, penaltyRate: c.PenaltyRatePct, penaltyCap: c.PenaltyCapPct,
		penaltyGrace: int(c.PenaltyGraceDays), indexationEnabled: c.IndexationEnabled, indexationPct: c.IndexationPct,
		indexationMonths: int(c.IndexationPeriodMonths), nextIndexation: c.NextIndexationOn,
		registrationRequired: c.RegistrationRequired != nil && *c.RegistrationRequired,
		fiscalMode:           p.FiscalMode, specialTerms: val(c.SpecialTerms),
	}
	v["contract.deposit_text"] = depositText(t)
	v["contract.utilities_text"] = utilitiesText(t)
	v["contract.penalty_text"] = penaltyText(t)
	v["contract.indexation_text"] = indexationText(t)
	v["contract.registration_text"] = registrationText(t)
	v["contract.renewal_text"] = renewalText(t)
	v["contract.fiscal_text"] = fiscalText(t)
	v["contract.special_terms"] = t.specialTerms
	if t.specialTerms == "" {
		v["contract.special_terms"] = "Особые условия отсутствуют."
	}
	v["property.address"] = p.Address
	v["property.area_sqm"] = decText(p.AreaSqm)
	if p.Floor != nil {
		v["property.floor"] = fmt.Sprint(*p.Floor)
	}
	v["property.cadastral_number"] = val(p.CadastralNumber)
	v["property.ownership_basis"] = val(p.OwnershipBasis)
	v["property.description_text"] = descriptionText(p.Kind, p.Rooms)
	v["property.furnished_text"] = furnishedText(p.Furnished)
	v["property.usage_text"] = usageText(c.LegalBasis, p.Kind)

	occ, err := q.ListOccupants(ctx, c.ID)
	if err != nil {
		return d, err
	}
	rows := make([]occupantRow, 0, len(occ))
	for _, o := range occ {
		rows = append(rows, occupantRow{o.FullName, fmtDate(o.BirthDate), val(o.Relation)})
	}
	d.Blocks["occupants"] = occupantsBlock(rows)
	return d, nil
}

// actData — переменные акта (09 разд. 4.3).
func (s *Service) actData(ctx context.Context, q *sqlc.Queries, d Data, cc contractCtx, a sqlc.ContractAct) error {
	v := d.Vars
	v["act.date"], v["act.date_words"] = a.ActDate.Format(), dateWords(a.ActDate)
	v["act.keys_count"] = fmt.Sprint(a.KeysCount)
	v["act.condition_notes"] = val(a.ConditionNotes)
	if a.Kind == "return" {
		v["act.damages"] = val(a.Damages)
		if v["act.damages"] == "" {
			v["act.damages"] = "Повреждений не выявлено."
		}
	}
	var items []struct {
		Name      string  `json:"name"`
		Quantity  int     `json:"quantity"`
		Condition string  `json:"condition"`
		Note      *string `json:"note"`
	}
	if err := json.Unmarshal(a.Inventory, &items); err != nil {
		return fmt.Errorf("опись акта: %w", err)
	}
	inv := make([]inventoryRow, 0, len(items))
	for _, it := range items {
		inv = append(inv, inventoryRow{it.Name, it.Quantity, it.Condition, val(it.Note)})
	}
	d.Blocks["inventory"] = inventoryBlock(inv)

	readings, err := q.ActReadings(ctx, &a.ID)
	if err != nil {
		return err
	}
	byMeter := map[uuid.UUID]string{}
	for _, r := range readings {
		byMeter[r.MeterID] = decText(r.Value)
	}
	meters, err := q.ActivePropertyMeters(ctx, cc.c.PropertyID)
	if err != nil {
		return err
	}
	rows := make([]meterRow, 0, len(meters))
	for _, m := range meters {
		rows = append(rows, meterRow{meterTitles[m.Type], val(m.SerialNumber), byMeter[m.ID]})
	}
	d.Blocks["meter_readings"] = metersBlock(rows)
	return nil
}

// amendmentData — переменные допсоглашения (09 разд. 4.4).
func amendmentData(d Data, cc contractCtx, a sqlc.ContractAmendment) {
	r := rolesOf(cc.basis)
	v := d.Vars
	v["amendment.number"] = fmt.Sprint(a.Number)
	v["amendment.signed_on_words"] = fmtDateWords(a.SignedOn)
	v["amendment.effective_from"] = a.EffectiveFrom.Format()
	var changes []string
	if a.NewRentKop != nil {
		what := "размер платы за наём"
		if cc.basis == "ch34" {
			what = "размер арендной платы"
		}
		changes = append(changes, fmt.Sprintf("С %s %s составляет %s в месяц.", a.EffectiveFrom.Format(), what,
			amountText(*a.NewRentKop)))
	}
	if a.NewEndDate != nil {
		changes = append(changes, fmt.Sprintf("Срок договора продлевается по %s включительно.", a.NewEndDate.Format()))
	}
	if a.NewPaymentDay != nil {
		changes = append(changes, fmt.Sprintf("Срок внесения %s — не позднее %d-го числа месяца.", r.rentGen, *a.NewPaymentDay))
	}
	if a.NewUtilitiesFixedKop != nil {
		changes = append(changes, fmt.Sprintf("Фиксированная часть компенсации ЖКУ составляет %s в месяц.",
			money.Kop(*a.NewUtilitiesFixedKop).FormatRub()))
	}
	if b := val(a.BodyText); b != "" {
		changes = append(changes, b)
	}
	for i := range changes {
		changes[i] = fmt.Sprintf("%d. %s", i+1, changes[i])
	}
	v["amendment.changes_text"] = strings.Join(changes, "\n")
}

// reconData — акт сверки за период (09 разд. 4.6; docs/open-questions.md, № 37).
// Начислено: начисления по сроку (без отменённых), возвраты и переносы переплаты в залог.
// Оплачено: проведённые платежи по дате поступления. Удержание из залога — в обеих колонках (на сальдо не влияет).
// Сальдо — начислено минус оплачено. Сам залог (внесение, возврат) — отдельный учёт: только строка остатка.
func reconData(ctx context.Context, q *sqlc.Queries, d Data, cc contractCtx, number string, from, to date.Date) error {
	type item struct {
		on            date.Date
		order         int
		doc           string
		charged, paid int64
	}
	var items []item
	charges, err := q.ReconciliationCharges(ctx, sqlc.ReconciliationChargesParams{ContractID: cc.c.ID, PeriodTo: to})
	if err != nil {
		return err
	}
	for _, c := range charges {
		items = append(items, item{c.OnDate, 0, c.Title, c.AmountKop, 0})
	}
	pays, err := q.ReconciliationPayments(ctx, sqlc.ReconciliationPaymentsParams{ContractID: cc.c.ID, PeriodTo: to})
	if err != nil {
		return err
	}
	for _, p := range pays {
		doc := "Оплата"
		if p.Method == "deposit_offset" {
			doc = "Зачёт обеспечительного платежа"
		}
		if val(p.ReceiptNumber) != "" {
			doc += ", квитанция " + val(p.ReceiptNumber)
		}
		items = append(items, item{p.OnDate, 1, doc, 0, p.AmountKop})
	}
	refunds, err := q.ReconciliationRefunds(ctx, sqlc.ReconciliationRefundsParams{ContractID: cc.c.ID, PeriodTo: to})
	if err != nil {
		return err
	}
	for _, r := range refunds {
		items = append(items, item{r.OnDate, 2, "Возврат переплаты", r.AmountKop, 0})
	}
	transfers, err := q.ReconciliationAdvanceTransfers(ctx, sqlc.ReconciliationAdvanceTransfersParams{ContractID: cc.c.ID, PeriodTo: to})
	if err != nil {
		return err
	}
	for _, tr := range transfers {
		items = append(items, item{tr.OnDate, 2, "Перенос переплаты в обеспечительный платёж", tr.AmountKop, 0})
	}
	holds, err := q.ReconciliationWithholdings(ctx, sqlc.ReconciliationWithholdingsParams{ContractID: cc.c.ID, PeriodTo: to})
	if err != nil {
		return err
	}
	for _, h := range holds {
		doc := "Удержание из обеспечительного платежа"
		if val(h.Reason) != "" {
			doc += ": " + val(h.Reason)
		}
		items = append(items, item{h.OnDate, 3, doc, h.AmountKop, h.AmountKop})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if c := items[i].on.Compare(items[j].on); c != 0 {
			return c < 0
		}
		return items[i].order < items[j].order
	})
	var opening, turnover int64
	rows := []reconRow{}
	for _, it := range items {
		if it.on.Before(from) {
			opening += it.charged - it.paid
			continue
		}
		turnover += it.charged - it.paid
		rows = append(rows, reconRow{it.on.Format(), it.doc, it.charged, it.paid})
	}
	v := d.Vars
	v["recon.number"] = number
	v["recon.period_from"], v["recon.period_to"] = from.Format(), to.Format()
	v["recon.opening_text"] = saldoText(from, cc.basis, opening)
	v["recon.closing_text"] = saldoText(to, cc.basis, opening+turnover)
	deposit, err := q.DepositBalanceOn(ctx, sqlc.DepositBalanceOnParams{ContractID: cc.c.ID, OnDate: to})
	if err != nil {
		return err
	}
	v["recon.deposit_text"] = fmt.Sprintf("Остаток обеспечительного платежа на %s: %s.", to.Format(), money.Kop(deposit).FormatRub())
	d.Blocks["reconciliation"] = reconBlock(rows)
	return nil
}

// consentData — бланк согласия (09 разд. 4.7).
func (s *Service) consentData(d Data, t sqlc.Tenant, o sqlc.OrganizationSetting) error {
	s.orgVars(d, o)
	if err := s.tenantVars(d, t); err != nil {
		return err
	}
	v := d.Vars
	v["consent.purpose"] = o.PdnConsentPurpose
	v["consent.data_list"] = "фамилия, имя, отчество; номер телефона; адрес электронной почты"
	years := int64(o.PdnRetentionYears)
	v["consent.term_text"] = fmt.Sprintf("до достижения цели или отзыва согласия, но не более %d %s после окончания "+
		"последнего договора", years, money.Plural(years, "года", "лет", "лет"))
	return nil
}
