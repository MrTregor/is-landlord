// Package fiscal — чеки (FR-06.2, FR-06.4): формирование чека прихода при проведении платежа и чека возврата
// прихода при возврате переплаты (облачная касса АТОЛ Онлайн), ручной чек НПД, отправка (fiscal.send), опрос отчёта
// (fiscal.poll), колбэк кассы и сверка зависших чеков (fiscal.reconcile). Правила — docs/spec/05 разд. 11,
// 08 разд. 3, 07 Фоновые задачи.
package fiscal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/billing/calc"
	"landlord/backend/internal/notify"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/atol"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
	"landlord/backend/internal/platform/names"
	"landlord/backend/internal/settings"
)

// Режимы чека объекта (properties.fiscal_mode), поставщики и статусы чека.
const (
	ModeNone      = "none"
	ModeCloudKKT  = "cloud_kkt"
	ModeNpdManual = "npd_manual"

	ProviderAtol = "atol"
	ProviderNpd  = "npd_manual"

	StatusQueued         = "queued"
	StatusWait           = "wait"
	StatusDone           = "done"
	StatusFail           = "fail"
	StatusManualRequired = "manual_required"
)

// Интервалы опроса отчёта (07 Фоновые задачи, fiscal.poll): первую минуту — каждые 5 с (12 раз), затем — каждые
// 5 минут; через 24 часа без ответа — ошибка «Нет ответа от кассы».
const (
	pollFast       = 5 * time.Second
	pollSlow       = 5 * time.Minute
	fastPhaseSec   = 60
	noAnswerSec    = 24 * 60 * 60
	errNoAnswer    = "Нет ответа от кассы"
	npdURLPrefix   = "https://lknpd.nalog.ru/"
	firstPollDelay = 5
)

// Sender — API кассы (platform/atol.Client).
type Sender interface {
	Send(ctx context.Context, operation string, body []byte) (atol.Response, error)
	Report(ctx context.Context, uuid string) (atol.Report, error)
}

// Jobs — постановка задач River в транзакции и немедленный запуск запланированной задачи (river.Client).
type Jobs interface {
	notify.Inserter
	JobRetryTx(ctx context.Context, tx pgx.Tx, id int64) (*rivertype.JobRow, error)
}

// Config — адреса для запроса чека.
type Config struct {
	CallbackURL string // ATOL_CALLBACK_URL
	PublicURL   string // APP_PUBLIC_URL — адрес расчётов (company.payment_address)
}

// Service — чеки.
type Service struct {
	db     *db.DB
	audit  *audit.Service
	clock  clock.Clock
	notify *notify.Service
	jobs   Jobs
	atol   Sender
	cfg    Config
}

// NewService создаёт сервис чеков.
func NewService(database *db.DB, au *audit.Service, clk clock.Clock, nt *notify.Service, jb Jobs, sender Sender, cfg Config) *Service {
	return &Service{db: database, audit: au, clock: clk, notify: nt, jobs: jb, atol: sender, cfg: cfg}
}

// ---------------------------------------------------------------- Создание чека (05 разд. 11)

// ForPayment — чек прихода по проведённому платежу «за начисления» (в том числе зачёту из залога) по режиму объекта:
// облачная касса — чек в очереди и задача fiscal.send; НПД — чек «нужно сформировать вручную»; иначе чека нет.
// Вызывается в транзакции проведения платежа; повтор ничего не создаёт.
func (s *Service) ForPayment(ctx context.Context, tx *db.Tx, p sqlc.Payment, c sqlc.Contract) error {
	if p.Status != "succeeded" || p.Purpose != "charges" {
		return nil
	}
	prop, err := tx.Q.GetProperty(ctx, c.PropertyID)
	if err != nil {
		return err
	}
	provider, status, ok := byMode(prop.FiscalMode)
	if !ok {
		return nil
	}
	return s.create(ctx, tx, sqlc.InsertFiscalReceiptParams{
		PaymentID: &p.ID, Operation: atol.OperationSell, Provider: provider, Status: status, TotalKop: p.AmountKop,
	})
}

// ForRefund — чек возврата прихода по возврату переплаты, если по платежу есть чек (тот же поставщик).
func (s *Service) ForRefund(ctx context.Context, tx *db.Tx, r sqlc.Refund) error {
	pf, err := tx.Q.PaymentFiscalReceipt(ctx, &r.PaymentID)
	if db.IsNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	status := StatusQueued
	if pf.Provider == ProviderNpd {
		status = StatusManualRequired
	}
	return s.create(ctx, tx, sqlc.InsertFiscalReceiptParams{
		RefundID: &r.ID, Operation: atol.OperationSellRefund, Provider: pf.Provider, Status: status, TotalKop: r.AmountKop,
	})
}

func byMode(mode string) (provider, status string, ok bool) {
	switch mode {
	case ModeCloudKKT:
		return ProviderAtol, StatusQueued, true
	case ModeNpdManual:
		return ProviderNpd, StatusManualRequired, true
	}
	return "", "", false
}

func (s *Service) create(ctx context.Context, tx *db.Tx, params sqlc.InsertFiscalReceiptParams) error {
	params.Now = s.clock.Now()
	f, err := tx.Q.InsertFiscalReceipt(ctx, params)
	if db.IsNoRows(err) {
		return nil
	}
	if err != nil || f.Provider != ProviderAtol {
		return err
	}
	_, err = s.jobs.InsertTx(ctx, tx.Tx, SendArgs{FiscalReceiptID: f.ID}, nil)
	return err
}

// ---------------------------------------------------------------- Данные чека

// receiptContext — платёж, договор и наниматель чека (для возврата — исходного платежа).
func receiptContext(ctx context.Context, q *sqlc.Queries, f sqlc.FiscalReceipt) (sqlc.FiscalPaymentContextRow, error) {
	paymentID := f.PaymentID
	if paymentID == nil {
		r, err := q.GetRefund(ctx, *f.RefundID)
		if err != nil {
			return sqlc.FiscalPaymentContextRow{}, err
		}
		paymentID = &r.PaymentID
	}
	return q.FiscalPaymentContext(ctx, *paymentID)
}

// ItemName — название позиции чека по начислению (08 разд. 3.2): «{вид} за {месяц ГГГГ}, договор № {номер}»;
// пени и возмещение ущерба — «Пени по договору № …», «Возмещение ущерба по договору № …».
func ItemName(kind, title string, month *date.Date, number string) string {
	switch kind {
	case "penalty":
		return "Пени по договору № " + number
	case "damage":
		return "Возмещение ущерба по договору № " + number
	}
	if month != nil && (kind == "rent" || kind == "utilities") {
		prefix, _, _ := strings.Cut(title, ", ")
		return prefix + " за " + calc.MonthName(*month) + ", договор № " + number
	}
	return title + ", договор № " + number
}

// paymentType — payments.type по способу и источнику платежа.
func paymentType(method, source string) int {
	switch {
	case source == "deposit_offset":
		return PaymentPrepaid
	case method == "cash":
		return PaymentCash
	}
	return PaymentCashless
}

// build — запрос чека по текущим данным (распределения платежа, наниматель, организация).
func (s *Service) build(ctx context.Context, q *sqlc.Queries, f sqlc.FiscalReceipt) ([]byte, error) {
	pc, err := receiptContext(ctx, q, f)
	if err != nil {
		return nil, err
	}
	org, err := q.GetOrganizationSettings(ctx)
	if db.IsNoRows(err) {
		org, err = settings.Defaults(), nil
	}
	if err != nil {
		return nil, err
	}
	in := Input{
		Operation: f.Operation, ExternalID: f.ID.String(), Timestamp: s.clock.Now(), Location: s.clock.Location(),
		CallbackURL: s.cfg.CallbackURL, CompanyEmail: org.Email, CompanyINN: org.Inn, TaxRegime: org.TaxRegime,
		VatPayer: org.VatPayer, PaymentAddress: s.cfg.PublicURL, PaymentType: paymentType(pc.Method, pc.Source),
		TotalKop: f.TotalKop,
	}
	if pc.AnonymizedAt == nil {
		in.Client = Client{Email: val(pc.TenantEmail), Phone: val(pc.TenantPhone), Legal: pc.TenantKind == "legal",
			Name: val(pc.OrgName), INN: val(pc.TenantInn)}
	}
	if f.Operation == atol.OperationSellRefund {
		in.Items = []Item{{Name: "Возврат переплаты по договору № " + pc.ContractNumber, SumKop: f.TotalKop, Advance: true}}
		return Build(in)
	}
	allocs, err := q.PaymentAllocations(ctx, pc.PaymentID)
	if err != nil {
		return nil, err
	}
	rest := f.TotalKop
	for _, a := range allocs {
		if a.ReversedAt != nil {
			continue
		}
		in.Items = append(in.Items, Item{Name: ItemName(a.ChargeKind, a.ChargeTitle, a.PeriodMonth, pc.ContractNumber), SumKop: a.AmountKop})
		rest -= a.AmountKop
	}
	if rest > 0 {
		in.Items = append(in.Items, Item{Name: "Аванс по договору № " + pc.ContractNumber, SumKop: rest, Advance: true})
	}
	return Build(in)
}

func val(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ---------------------------------------------------------------- Отправка (fiscal.send)

// Send — отправка чека в кассу (07, fiscal.send): запрос формируется один раз и хранится (повтор задачи отправляет то
// же тело — касса вернёт тот же документ); ответ wait → uuid документа и опрос отчёта через 5 с; ошибка проверки
// данных (в системе или в кассе) — сразу fail без повторов и письмо собственнику. Сбой сети — ошибка (задача повторится).
func (s *Service) Send(ctx context.Context, id uuid.UUID) error {
	var body []byte
	var operation string
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		f, err := tx.Q.GetFiscalReceiptForUpdate(ctx, id)
		if err != nil || f.Status != StatusQueued {
			return err
		}
		fr := receiptOf(f)
		operation = f.Operation
		if f.RequestPayload != nil {
			body = f.RequestPayload
			return nil
		}
		b, err := s.build(ctx, tx.Q, fr)
		var de *DataError
		if errors.As(err, &de) {
			return s.markFail(ctx, tx, fr, nil, de.Text, nil, 1)
		}
		if err != nil {
			return err
		}
		body = b
		return tx.Q.SetFiscalPayload(ctx, sqlc.SetFiscalPayloadParams{ID: id, RequestPayload: b})
	})
	if db.IsNoRows(err) {
		return river.JobCancel(err)
	}
	if err != nil || body == nil {
		return err
	}
	resp, err := s.atol.Send(ctx, operation, body)
	if err != nil {
		return fmt.Errorf("касса: %w", err)
	}
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		f, err := tx.Q.GetFiscalReceiptForUpdate(ctx, id)
		if err != nil || f.Status != StatusQueued {
			return err
		}
		fr := receiptOf(f)
		if resp.Error == nil && resp.UUID != nil && *resp.UUID != "" && resp.Status != atol.StatusFail {
			if err := tx.Q.SetFiscalWait(ctx, sqlc.SetFiscalWaitParams{ID: id, ExternalUuid: resp.UUID, LastResponse: resp.Raw}); err != nil {
				return err
			}
			at, err := tx.Q.DBTimeAfter(ctx, firstPollDelay)
			if err != nil {
				return err
			}
			_, err = s.jobs.InsertTx(ctx, tx.Tx, PollArgs{FiscalReceiptID: id}, &river.InsertOpts{ScheduledAt: at})
			return err
		}
		code, text := "", "Касса не приняла чек"
		if resp.Error != nil {
			code, text = fmt.Sprint(resp.Error.Code), resp.Error.Text
		}
		return s.markFail(ctx, tx, fr, &code, text, resp.Raw, 1)
	})
}

// receiptOf — строка чека без вычисляемых полей.
func receiptOf(r sqlc.GetFiscalReceiptForUpdateRow) sqlc.FiscalReceipt {
	return sqlc.FiscalReceipt{
		ID: r.ID, PaymentID: r.PaymentID, RefundID: r.RefundID, Operation: r.Operation, Provider: r.Provider, Status: r.Status,
		ExternalUuid: r.ExternalUuid, RequestPayload: r.RequestPayload, LastResponse: r.LastResponse, TotalKop: r.TotalKop,
		FiscalReceiptNumber: r.FiscalReceiptNumber, ShiftNumber: r.ShiftNumber, ReceiptDatetime: r.ReceiptDatetime,
		FnNumber: r.FnNumber, FiscalDocumentNumber: r.FiscalDocumentNumber, FiscalDocumentAttribute: r.FiscalDocumentAttribute,
		OfdReceiptUrl: r.OfdReceiptUrl, NpdReceiptUrl: r.NpdReceiptUrl, ErrorCode: r.ErrorCode, ErrorText: r.ErrorText,
		Attempts: r.Attempts, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// ---------------------------------------------------------------- Опрос отчёта (fiscal.poll)

// Poll — отчёт о документе (07, fiscal.poll): done → реквизиты и письмо нанимателю fiscal_receipt; fail → ошибка и
// письмо собственнику; wait или сбой связи → повтор через 5 с (первую минуту) или 5 мин, через 24 часа — fail
// «Нет ответа от кассы». Возвращает паузу до следующего опроса (0 — опрос закончен).
func (s *Service) Poll(ctx context.Context, id uuid.UUID) (time.Duration, error) {
	f, err := s.db.Queries().GetFiscalReceipt(ctx, id)
	if db.IsNoRows(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if f.Status != StatusWait || f.ExternalUuid == nil {
		return 0, nil
	}
	rep, callErr := s.atol.Report(ctx, *f.ExternalUuid)
	var next time.Duration
	err = s.db.WithTx(ctx, func(tx *db.Tx) error {
		l, err := tx.Q.GetFiscalReceiptForUpdate(ctx, id)
		if err != nil || l.Status != StatusWait || l.ExternalUuid == nil || *l.ExternalUuid != *f.ExternalUuid {
			return err
		}
		fr := receiptOf(l)
		switch {
		case callErr == nil && rep.Status == atol.StatusDone && rep.Payload != nil:
			return s.markDone(ctx, tx, fr, rep)
		case callErr == nil && rep.Status == atol.StatusFail:
			code, text := "", "Касса не сформировала чек"
			if rep.Error != nil {
				code, text = fmt.Sprint(rep.Error.Code), rep.Error.Text
			}
			return s.markFail(ctx, tx, fr, &code, text, rep.Raw, 0)
		case l.AgeSeconds >= noAnswerSec:
			return s.markFail(ctx, tx, fr, nil, errNoAnswer, nil, 0)
		case l.AgeSeconds < fastPhaseSec:
			next = pollFast
		default:
			next = pollSlow
		}
		return nil
	})
	return next, err
}

// markDone — реквизиты чека, письмо нанимателю fiscal_receipt (10 Уведомления), журнал fiscal.done.
func (s *Service) markDone(ctx context.Context, tx *db.Tx, f sqlc.FiscalReceipt, rep atol.Report) error {
	p := rep.Payload
	params := sqlc.SetFiscalDoneParams{
		ID: f.ID, LastResponse: rep.Raw, FiscalReceiptNumber: &p.FiscalReceiptNumber, ShiftNumber: &p.ShiftNumber,
		FnNumber: &p.FnNumber, FiscalDocumentNumber: &p.FiscalDocumentNumber, FiscalDocumentAttribute: &p.FiscalDocumentAttribute,
	}
	if at, err := atol.ParseTimestamp(p.ReceiptDatetime, s.clock.Location()); err == nil {
		params.ReceiptDatetime = &at
	}
	if p.OfdReceiptURL != "" {
		params.OfdReceiptUrl = &p.OfdReceiptURL
	}
	done, err := tx.Q.SetFiscalDone(ctx, params)
	if err != nil {
		return err
	}
	pc, err := receiptContext(ctx, tx.Q, done)
	if err != nil {
		return err
	}
	if err := s.audit.Record(ctx, tx.Q, audit.Entry{
		Action: "fiscal.done", EntityType: "fiscal_receipt", EntityID: f.ID.String(),
		Summary: fmt.Sprintf("Договор № %s: %s на %s сформирован, ФД %d", pc.ContractNumber, operationTitle(f.Operation),
			money.Kop(f.TotalKop).Format(), p.FiscalDocumentNumber),
		Before: map[string]any{"status": f.Status},
		After: map[string]any{"status": StatusDone, "fiscalDocumentNumber": p.FiscalDocumentNumber,
			"fiscalDocumentAttribute": p.FiscalDocumentAttribute, "fnNumber": p.FnNumber},
	}); err != nil {
		return err
	}
	if done.OfdReceiptUrl == nil {
		return nil
	}
	to, vars, err := tenantMail(ctx, tx.Q, pc)
	if err != nil {
		return err
	}
	vars["receipt_url"] = *done.OfdReceiptUrl
	vars["amount"] = money.Kop(f.TotalKop).FormatRub()
	return s.notify.Queue(ctx, tx, notify.Email{
		Code: "fiscal_receipt", To: to, Vars: vars, DedupeKey: "fiscal_receipt:" + f.ID.String(),
		RelatedType: "fiscal_receipt", RelatedID: &f.ID,
	})
}

// markFail — ошибка чека (attemptsInc = 1 — ошибка получена при отправке), журнал fiscal.fail, письмо собственникам
// owner_fiscal_error (05 разд. 11).
func (s *Service) markFail(ctx context.Context, tx *db.Tx, f sqlc.FiscalReceipt, code *string, text string, raw []byte,
	attemptsInc int32,
) error {
	if code != nil && *code == "" {
		code = nil
	}
	failed, err := tx.Q.SetFiscalFail(ctx, sqlc.SetFiscalFailParams{
		ID: f.ID, ErrorCode: code, ErrorText: &text, LastResponse: raw, AttemptsInc: attemptsInc,
	})
	if err != nil {
		return err
	}
	pc, err := receiptContext(ctx, tx.Q, failed)
	if err != nil {
		return err
	}
	if err := s.audit.Record(ctx, tx.Q, audit.Entry{
		Action: "fiscal.fail", EntityType: "fiscal_receipt", EntityID: f.ID.String(),
		Summary: fmt.Sprintf("Договор № %s: %s на %s не сформирован: %s", pc.ContractNumber, operationTitle(f.Operation),
			money.Kop(f.TotalKop).Format(), text),
		Before: map[string]any{"status": f.Status},
		After:  map[string]any{"status": StatusFail, "errorCode": code, "errorText": text, "attempts": failed.Attempts},
	}); err != nil {
		return err
	}
	owners, err := tx.Q.ActiveOwnerUsers(ctx)
	if err != nil {
		return err
	}
	for _, o := range owners {
		if err := s.notify.Queue(ctx, tx, notify.Email{
			Code: "owner_fiscal_error", To: o.Email, RelatedType: "fiscal_receipt", RelatedID: &f.ID,
			DedupeKey: fmt.Sprintf("owner_fiscal_error:%s:%d:%s", f.ID, failed.Attempts, o.ID),
			Vars: map[string]string{
				"amount": money.Kop(f.TotalKop).FormatRub(), "error_text": text, "contract.number": pc.ContractNumber,
				"link": s.notify.Link("/fiscal-receipts"),
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func operationTitle(op string) string {
	if op == atol.OperationSellRefund {
		return "чек возврата прихода"
	}
	return "чек прихода"
}

// tenantMail — адрес нанимателя и общие переменные писем (10 Уведомления, раздел 1).
func tenantMail(ctx context.Context, q *sqlc.Queries, pc sqlc.FiscalPaymentContextRow) (string, map[string]string, error) {
	c, err := q.GetContract(ctx, pc.ContractID)
	if err != nil {
		return "", nil, err
	}
	p, err := q.GetProperty(ctx, c.PropertyID)
	if err != nil {
		return "", nil, err
	}
	to := ""
	if pc.TenantEmail != nil && pc.AnonymizedAt == nil {
		to = *pc.TenantEmail
	}
	greeting := "Уважаемые коллеги"
	if pc.TenantKind != "legal" {
		greeting = names.Greeting(pc.FirstName, pc.MiddleName)
	}
	return to, map[string]string{
		"tenant.greeting": greeting, "contract.number": pc.ContractNumber, "property.address": p.Address, "property.code": p.Code,
	}, nil
}

// ---------------------------------------------------------------- Колбэк кассы (08 разд. 3.4)

// Callback — колбэк АТОЛ: тело не источник данных; по uuid ищется чек, событие сохраняется один раз (uuid:status)
// и ставится fiscal.poll (запланированный опрос запускается сразу). Неизвестный uuid — ничего не делается.
func (s *Service) Callback(ctx context.Context, body map[string]any) error {
	id, _ := body["uuid"].(string)
	status, _ := body["status"].(string)
	if id == "" {
		return nil
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		f, err := tx.Q.FiscalReceiptByUUID(ctx, &id)
		result := "processed"
		switch {
		case db.IsNoRows(err):
			result = "ignored"
		case err != nil:
			return err
		}
		if _, err := tx.Q.InsertAtolWebhookEvent(ctx, sqlc.InsertAtolWebhookEventParams{
			EventKey: id + ":" + status, Payload: payload, Now: s.clock.Now(), Result: &result,
		}); err != nil {
			if db.IsNoRows(err) {
				return nil // повтор колбэка (NFR-Rel-02)
			}
			return err
		}
		if result != "processed" || f.Status != StatusWait {
			return nil
		}
		return s.pollNow(ctx, tx, f.ID)
	})
}

// pollNow — опрос отчёта немедленно: новая задача или запуск уже запланированной.
func (s *Service) pollNow(ctx context.Context, tx *db.Tx, id uuid.UUID) error {
	res, err := s.jobs.InsertTx(ctx, tx.Tx, PollArgs{FiscalReceiptID: id}, nil)
	if err != nil {
		return err
	}
	if res.UniqueSkippedAsDuplicate && res.Job != nil && res.Job.State == rivertype.JobStateScheduled {
		_, err = s.jobs.JobRetryTx(ctx, tx.Tx, res.Job.ID)
	}
	return err
}

// ---------------------------------------------------------------- Действия собственника (14 разд. 13)

// errCancelled — платёж чека отменён: чек больше не нужен.
func checkPaymentActive(ctx context.Context, q *sqlc.Queries, f sqlc.FiscalReceipt) error {
	pc, err := receiptContext(ctx, q, f)
	if err != nil {
		return err
	}
	if pc.PaymentStatus != "succeeded" {
		return apperr.BusinessRule("Платёж отменён — чек по нему не формируется")
	}
	return nil
}

// Retry — повтор чека с ошибкой: запрос формируется заново, задача fiscal.send.
func (s *Service) Retry(ctx context.Context, id uuid.UUID) error {
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		l, err := tx.Q.GetFiscalReceiptForUpdate(ctx, id)
		if db.IsNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		f := receiptOf(l)
		if f.Status != StatusFail || f.Provider != ProviderAtol {
			return apperr.Conflict(apperr.CodeConflictState, "Повторить можно только чек с ошибкой")
		}
		if err := checkPaymentActive(ctx, tx.Q, f); err != nil {
			return err
		}
		if _, err := tx.Q.RequeueFiscal(ctx, id); err != nil {
			return err
		}
		if _, err := s.jobs.InsertTx(ctx, tx.Tx, SendArgs{FiscalReceiptID: id}, nil); err != nil {
			return err
		}
		pc, err := receiptContext(ctx, tx.Q, f)
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "fiscal.retry", EntityType: "fiscal_receipt", EntityID: id.String(),
			Summary: fmt.Sprintf("Договор № %s: повторная отправка: %s на %s", pc.ContractNumber, operationTitle(f.Operation),
				money.Kop(f.TotalKop).Format()),
			Before: map[string]any{"status": f.Status, "errorText": f.ErrorText}, After: map[string]any{"status": StatusQueued},
		})
	})
}

// Manual — ссылка на чек «Мой налог» для чека НПД: manual_required → done.
func (s *Service) Manual(ctx context.Context, id uuid.UUID, link string) error {
	link = strings.TrimSpace(link)
	if !strings.HasPrefix(link, npdURLPrefix) || len(link) == len(npdURLPrefix) {
		return apperr.Validation(apperr.FieldError{Field: "npdReceiptUrl",
			Message: "Укажите ссылку на чек из «Мой налог»: https://lknpd.nalog.ru/…"})
	}
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		l, err := tx.Q.GetFiscalReceiptForUpdate(ctx, id)
		if db.IsNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		f := receiptOf(l)
		if f.Status != StatusManualRequired {
			return apperr.Conflict(apperr.CodeConflictState, "Ссылка указывается только для чека, который нужно сформировать вручную")
		}
		if err := checkPaymentActive(ctx, tx.Q, f); err != nil {
			return err
		}
		if _, err := tx.Q.SetFiscalManual(ctx, sqlc.SetFiscalManualParams{ID: id, NpdReceiptUrl: &link}); err != nil {
			return err
		}
		pc, err := receiptContext(ctx, tx.Q, f)
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "fiscal.manual", EntityType: "fiscal_receipt", EntityID: id.String(),
			Summary: fmt.Sprintf("Договор № %s: указан чек НПД на %s", pc.ContractNumber, money.Kop(f.TotalKop).Format()),
			Before:  map[string]any{"status": f.Status}, After: map[string]any{"status": StatusDone, "npdReceiptUrl": link},
		})
	})
}
