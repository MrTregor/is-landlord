package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/notify"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/jobs"
	"landlord/backend/internal/platform/money"
	"landlord/backend/internal/platform/names"
	"landlord/backend/internal/platform/reqctx"
	"landlord/backend/internal/platform/tbank"
)

// Online — настройки онлайн-оплаты (08 разд. 1).
type Online struct {
	Client          *tbank.Client
	NotificationURL string // TBANK_NOTIFICATION_URL
	PublicURL       string // APP_PUBLIC_URL
	Log             *slog.Logger
}

// SourceOnline — источник и способ онлайн-оплаты.
const SourceOnline = "online"

// paymentLinkTTL — срок действия ссылки на оплату (08 разд. 1.3).
const paymentLinkTTL = 24 * time.Hour

// errUnavailable — ответ клиенту при недоступности банка (08 разд. 1.3).
func errUnavailable(cause error) error {
	return apperr.BadGateway("Платёжный сервис недоступен. Попробуйте позже").Wrap(cause)
}

// OnlineInput — оплата из кабинета (06 разд. 15, POST /cabinet/payments).
type OnlineInput struct {
	ContractID uuid.UUID
	Purpose    string
	ChargeIDs  []uuid.UUID
}

// OnlineStarted — платёж и ссылка на страницу оплаты.
type OnlineStarted struct {
	PaymentID  uuid.UUID
	PaymentURL string
}

// TenantContract — договор нанимателя (чужой или черновик → 404, 06 разд. 15).
func TenantContract(ctx context.Context, q *sqlc.Queries, contractID uuid.UUID) (sqlc.Contract, error) {
	a := reqctx.ActorFrom(ctx)
	c, err := q.GetContract(ctx, contractID)
	if db.IsNoRows(err) || (err == nil && (a == nil || a.TenantID == nil || c.TenantID != *a.TenantID || c.Status == "draft")) {
		return c, apperr.NotFound()
	}
	return c, err
}

// payable — начисления к оплате: выбранные (или все с долгом, кроме начисляемых пеней) и их сумма.
func payable(open []sqlc.Charge, ids []uuid.UUID) ([]uuid.UUID, int64, error) {
	var chosen []uuid.UUID
	var total int64
	for _, ch := range open {
		if ch.PenaltyState != nil && *ch.PenaltyState == "accruing" {
			continue
		}
		if len(ids) == 0 || slices.Contains(ids, ch.ID) {
			chosen = append(chosen, ch.ID)
			total += ch.AmountKop - ch.PaidKop
		}
	}
	if len(ids) > 0 && len(chosen) != len(ids) {
		return nil, 0, apperr.Validation(apperr.FieldError{Field: "chargeIds", Message: "Начисление уже оплачено или недоступно"})
	}
	return chosen, total, nil
}

// StartOnline — начало онлайн-оплаты (08 разд. 1.2–1.3): платёж pending (OrderId = id платежа), запрос Init,
// ссылка на страницу оплаты. Неистёкшая начатая оплата с той же суммой и составом возвращается повторно.
func (s *Service) StartOnline(ctx context.Context, in OnlineInput) (OnlineStarted, error) {
	if in.Purpose != PurposeCharges && in.Purpose != PurposeDeposit {
		return OnlineStarted{}, apperr.Validation(apperr.FieldError{Field: "purpose", Message: "Выберите назначение платежа"})
	}
	var p sqlc.Payment
	var c sqlc.Contract
	var reused bool
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		var err error
		if c, err = TenantContract(ctx, tx.Q, in.ContractID); err != nil {
			return err
		}
		if c, err = lockContract(ctx, tx, c.ID); err != nil {
			return err
		}
		requested := []uuid.UUID{}
		var amount int64
		if in.Purpose == PurposeDeposit {
			tot, err := tx.Q.DepositTotals(ctx, c.ID)
			if err != nil {
				return err
			}
			amount = max(0, c.DepositKop-tot.ReceivedKop)
			if amount <= 0 {
				return apperr.BusinessRule("Обеспечительный платёж внесён полностью")
			}
		} else {
			open, err := s.billing.OpenCharges(ctx, tx, c.ID)
			if err != nil {
				return err
			}
			if requested, amount, err = payable(open, in.ChargeIDs); err != nil {
				return err
			}
			if amount <= 0 {
				return apperr.BusinessRule("Задолженности к оплате нет")
			}
		}
		slices.SortFunc(requested, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
		prev, err := tx.Q.ReusablePendingPayment(ctx, sqlc.ReusablePendingPaymentParams{
			ContractID: c.ID, Purpose: in.Purpose, AmountKop: amount, Now: ptr(s.clock.Now()), ChargeIds: requested,
		})
		switch {
		case err == nil:
			p, reused = prev, true
			return nil
		case !db.IsNoRows(err):
			return err
		}
		if p, err = tx.Q.InsertPendingPayment(ctx, sqlc.InsertPendingPaymentParams{
			ContractID: c.ID, Purpose: in.Purpose, AmountKop: amount, RequestedChargeIds: requested, CreatedBy: actorID(ctx),
			Now: s.clock.Now(),
		}); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "payment.create", EntityType: "payment", EntityID: p.ID.String(),
			Summary: fmt.Sprintf("Договор № %s: наниматель начал онлайн-оплату %s", c.Number, money.Kop(amount).Format()),
			After:   map[string]any{"purpose": in.Purpose, "amountKop": amount, "chargeIds": requested, "source": SourceOnline},
		})
	})
	if err != nil {
		return OnlineStarted{}, err
	}
	if reused {
		return OnlineStarted{PaymentID: p.ID, PaymentURL: *p.PaymentUrl}, nil
	}
	return s.initOnline(ctx, c, p)
}

// initOnline — запрос Init; при ошибке платёж failed с кодом ошибки банка (08 разд. 1.3).
func (s *Service) initOnline(ctx context.Context, c sqlc.Contract, p sqlc.Payment) (OnlineStarted, error) {
	t, err := s.db.Queries().GetTenant(ctx, c.TenantID)
	if err != nil {
		return OnlineStarted{}, err
	}
	desc := "Оплата по договору № " + c.Number
	if p.Purpose == PurposeDeposit {
		desc = "Обеспечительный платёж по договору № " + c.Number
	}
	email := ""
	if t.Email != nil && t.AnonymizedAt == nil {
		email = *t.Email
	}
	result := strings.TrimRight(s.online.PublicURL, "/") + "/cabinet/payments/" + p.ID.String() + "/result"
	due := s.clock.Now().Add(paymentLinkTTL).In(s.clock.Location()).Truncate(time.Second)
	resp, callErr := s.online.Client.Init(ctx, tbank.InitRequest{
		Amount: p.AmountKop, OrderID: p.ID.String(), Description: desc, NotificationURL: s.online.NotificationURL,
		SuccessURL: result, FailURL: result, RedirectDueDate: due, Email: email, ContractNumber: c.Number,
	})
	if callErr != nil || !resp.Success || resp.PaymentID == "" || resp.PaymentURL == "" {
		code := resp.ErrorCode
		if callErr != nil {
			code = "NETWORK"
		}
		if code == "" {
			code = "UNKNOWN"
		}
		cause := callErr
		if cause == nil {
			cause = fmt.Errorf("ответ Init: %s %s", resp.ErrorCode, resp.Message)
		}
		return s.failInit(ctx, c, p, code, cause)
	}
	err = s.db.Queries().SetPaymentInit(ctx, sqlc.SetPaymentInitParams{
		ID: p.ID, ProviderPaymentID: &resp.PaymentID, PaymentUrl: &resp.PaymentURL, ExpiresAt: &due,
	})
	if code, _, ok := db.ConstraintError(err); ok && code == db.CodeUniqueViolation {
		// Банк вернул PaymentId, уже сохранённый у другого платежа (например, эмулятор очищен): оплата не начинается.
		return s.failInit(ctx, c, p, "DUPLICATE_PAYMENT_ID", err)
	}
	if err != nil {
		return OnlineStarted{}, err
	}
	return OnlineStarted{PaymentID: p.ID, PaymentURL: resp.PaymentURL}, nil
}

// failInit — оплата не начата: платёж failed с кодом банка, клиенту 502 (08 разд. 1.3).
func (s *Service) failInit(ctx context.Context, c sqlc.Contract, p sqlc.Payment, code string, cause error) (OnlineStarted, error) {
	s.logWarn("Init отклонён", slog.String("paymentId", p.ID.String()), slog.String("errorCode", code))
	if err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		if err := tx.Q.SetPaymentFailed(ctx, sqlc.SetPaymentFailedParams{ID: p.ID, ProviderStatus: &code}); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "payment.fail", EntityType: "payment", EntityID: p.ID.String(),
			Summary: fmt.Sprintf("Договор № %s: онлайн-оплата не начата, ответ банка %s", c.Number, code),
			After:   map[string]any{"status": "failed", "providerStatus": code},
		})
	}); err != nil {
		return OnlineStarted{}, err
	}
	return OnlineStarted{}, errUnavailable(cause)
}

func (s *Service) logWarn(msg string, attrs ...any) {
	if s.online.Log != nil {
		s.online.Log.Warn(msg, attrs...)
	}
}

// ---------------------------------------------------------------- Уведомления (08 разд. 1.4–1.5)

// NotificationArgs — задача tbank.process_notification.
type NotificationArgs struct {
	WebhookEventID uuid.UUID `json:"webhookEventId"`
}

// Kind — вид задачи River.
func (NotificationArgs) Kind() string { return "tbank.process_notification" }

// InsertOpts — очередь integrations, 10 попыток (07 Фоновые задачи).
func (NotificationArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueIntegrations, MaxAttempts: jobs.DefaultMaxAttempts}
}

// NotificationWorker выполняет tbank.process_notification.
type NotificationWorker struct {
	river.WorkerDefaults[NotificationArgs]
	Svc *Service
}

// Work — обработка сохранённого уведомления.
func (w *NotificationWorker) Work(ctx context.Context, job *river.Job[NotificationArgs]) error {
	return w.Svc.ProcessNotification(ctx, job.Args.WebhookEventID)
}

// ErrBadSignature — неверный терминал или подпись уведомления.
var ErrBadSignature = errors.New("неверная подпись уведомления")

// AcceptNotification — приём уведомления (08 разд. 1.4): проверка подписи; событие сохраняется один раз
// (provider, PaymentId:Status) и в той же транзакции ставится задача обработки. Повтор ничего не создаёт.
func (s *Service) AcceptNotification(ctx context.Context, fields map[string]any) error {
	// Тело уже разобрано стандартным декодером; повторная сериализация и разбор с json.Number сохраняют целые
	// числа без дробной части (суммы, PaymentId) для подписи.
	payload, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	n, err := tbank.ParseNotification(payload)
	if err != nil || !s.online.Client.Verify(n) {
		s.logWarn("уведомление Т-Банка с неверной подписью отклонено", slog.String("paymentId", n.PaymentID),
			slog.String("status", n.Status))
		return ErrBadSignature
	}
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		id, err := tx.Q.InsertWebhookEvent(ctx, sqlc.InsertWebhookEventParams{
			Provider: "tbank", EventKey: n.PaymentID + ":" + n.Status, Payload: payload, Now: s.clock.Now(),
		})
		if db.IsNoRows(err) {
			return nil // повтор уже сохранённого уведомления (NFR-Rel-02)
		}
		if err != nil {
			return err
		}
		_, err = s.jobs.InsertTx(ctx, tx.Tx, NotificationArgs{WebhookEventID: id}, nil)
		return err
	})
}

// ProcessNotification — применение уведомления к платежу (08 разд. 1.5); итог — в webhook_events.
func (s *Service) ProcessNotification(ctx context.Context, eventID uuid.UUID) error {
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		ev, err := tx.Q.GetWebhookEvent(ctx, eventID)
		if db.IsNoRows(err) {
			return river.JobCancel(fmt.Errorf("событие %s не найдено", eventID))
		}
		if err != nil {
			return err
		}
		if ev.ProcessedAt != nil {
			return nil
		}
		n, err := tbank.ParseNotification(ev.Payload)
		result, errText := "error", ""
		if err == nil {
			result, errText, err = s.applyStatus(ctx, tx, n.OrderID, n.PaymentID, n.Amount, n.Status, ev.ReceivedAt)
		} else {
			errText, err = "неверный формат уведомления", nil
		}
		if err != nil {
			return err
		}
		params := sqlc.FinishWebhookEventParams{ID: ev.ID, Result: &result, Now: ptr(s.clock.Now())}
		if errText != "" {
			params.ErrorText = &errText
		}
		return tx.Q.FinishWebhookEvent(ctx, params)
	})
}

// failStatuses — статусы банка, переводящие платёж pending в failed (08 разд. 1.5).
var failStatuses = []string{tbank.StatusRejected, tbank.StatusCanceled, tbank.StatusDeadlineExpired, tbank.StatusAuthFail}

// applyStatus — статус банка для платежа OrderId (08 разд. 1.5). Возвращает итог (processed, ignored, error) и
// текст ошибки сверки; err — только сбой БД (задача повторится).
func (s *Service) applyStatus(ctx context.Context, tx *db.Tx, orderID, providerID string, amount int64, status string,
	at time.Time,
) (string, string, error) {
	id, ok := orderUUID(orderID)
	if !ok {
		return "error", "платёж не найден", nil
	}
	p, err := tx.Q.GetPayment(ctx, id)
	if db.IsNoRows(err) {
		return "error", "платёж не найден", nil
	}
	if err != nil {
		return "", "", err
	}
	c, err := lockContract(ctx, tx, p.ContractID)
	if err != nil {
		return "", "", err
	}
	if p, err = tx.Q.GetPaymentForUpdate(ctx, id); err != nil {
		return "", "", err
	}
	if p.Source != SourceOnline || p.ProviderPaymentID == nil || *p.ProviderPaymentID != providerID || p.AmountKop != amount {
		return "error", "PaymentId или сумма не совпадают с платежом", nil
	}
	switch {
	case status == tbank.StatusConfirmed:
		if p.Status != "pending" && p.Status != "failed" {
			return "ignored", "", nil
		}
		return "processed", "", s.succeedOnline(ctx, tx, c, p, at)
	case slices.Contains(failStatuses, status):
		if p.Status != "pending" {
			return "ignored", "", nil
		}
		if err := tx.Q.SetPaymentFailed(ctx, sqlc.SetPaymentFailedParams{ID: p.ID, ProviderStatus: &status}); err != nil {
			return "", "", err
		}
		return "processed", "", s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "payment.fail", EntityType: "payment", EntityID: p.ID.String(),
			Summary: fmt.Sprintf("Договор № %s: онлайн-оплата %s не прошла (%s)", c.Number, money.Kop(p.AmountKop).Format(), status),
			Before:  map[string]any{"status": p.Status}, After: map[string]any{"status": "failed", "providerStatus": status},
		})
	case p.Status == "succeeded" && (status == tbank.StatusRefunded || status == tbank.StatusPartialRefunded):
		// Возврат через банк система сама не учитывает: событие ignored и письмо собственнику
		// (docs/open-questions.md, № 57).
		return "ignored", "", s.notifyOwnersBankRefund(ctx, tx, c, p, status)
	default:
		// AUTHORIZED, FORM_SHOWED, NEW и прочие — только статус банка (у проведённого платежа не меняется:
		// поздние REJECTED, CANCELED после CONFIRMED тоже игнорируются).
		if p.Status == "pending" {
			if err := tx.Q.SetProviderStatus(ctx, sqlc.SetProviderStatusParams{ID: p.ID, ProviderStatus: &status}); err != nil {
				return "", "", err
			}
		}
		return "ignored", "", nil
	}
}

// orderUUID — OrderId уведомления как id платежа.
func orderUUID(s string) (uuid.UUID, bool) {
	id, err := uuid.Parse(s)
	return id, err == nil
}

// succeedOnline — платёж проведён (08 разд. 1.5, CONFIRMED): дата поступления — дата события по Москве, номер
// квитанции, распределение или залог, расхождения, квитанция, письмо собственнику owner_payment_received.
func (s *Service) succeedOnline(ctx context.Context, tx *db.Tx, c sqlc.Contract, p sqlc.Payment, at time.Time) error {
	on := date.In(at, s.clock.Location())
	number, err := receiptNumber(ctx, tx.Q, on)
	if err != nil {
		return err
	}
	confirmed := tbank.StatusConfirmed
	np, err := tx.Q.SucceedPayment(ctx, sqlc.SucceedPaymentParams{ID: p.ID, ReceivedOn: &on, ReceiptNumber: &number, ProviderStatus: &confirmed})
	if err != nil {
		return err
	}
	res, err := s.settle(ctx, tx, c, np)
	if err != nil {
		return err
	}
	if err := s.audit.Record(ctx, tx.Q, audit.Entry{
		Action: "payment.succeed", EntityType: "payment", EntityID: p.ID.String(),
		Summary: fmt.Sprintf("Договор № %s: онлайн-оплата %s проведена, квитанция %s", c.Number, money.Kop(p.AmountKop).Format(), number),
		Before:  map[string]any{"status": p.Status},
		After: map[string]any{"status": "succeeded", "receivedOn": on.String(), "receiptNumber": number,
			"advanceKop": int64(res.advance)},
	}); err != nil {
		return err
	}
	if p.Purpose == PurposeDeposit {
		if err := s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "deposit.receipt", EntityType: "contract", EntityID: c.ID.String(),
			Summary: fmt.Sprintf("Договор № %s: внесён залог %s онлайн", c.Number, money.Kop(p.AmountKop).Format()),
			After:   map[string]any{"amountKop": p.AmountKop, "opDate": on.String(), "paymentId": p.ID},
		}); err != nil {
			return err
		}
	}
	return s.notifyOwners(ctx, tx, c, np)
}

// notifyOwners — письмо собственникам owner_payment_received (10 Уведомления, раздел 3).
func (s *Service) notifyOwners(ctx context.Context, tx *db.Tx, c sqlc.Contract, p sqlc.Payment) error {
	t, err := tx.Q.GetTenant(ctx, c.TenantID)
	if err != nil {
		return err
	}
	short := names.Short(t.LastName, t.FirstName, t.MiddleName)
	if t.Kind == "legal" && t.OrgName != nil {
		short = *t.OrgName
	}
	owners, err := tx.Q.ActiveOwnerUsers(ctx)
	if err != nil {
		return err
	}
	for _, o := range owners {
		if err := s.notify.Queue(ctx, tx, notify.Email{
			Code: "owner_payment_received", To: o.Email, DedupeKey: "owner_payment:" + p.ID.String() + ":" + o.ID.String(),
			RelatedType: "payment", RelatedID: &p.ID,
			Vars: map[string]string{
				"tenant.short_name": short, "amount": money.Kop(p.AmountKop).Format(), "received_on": p.ReceivedOn.Format(),
				"method_title": methodTitle(p.Method), "contract.number": c.Number, "link": s.notify.Link("/payments/" + p.ID.String()),
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

// notifyOwnersBankRefund — письмо собственникам owner_bank_refund о возврате проведённого онлайн-платежа банком.
func (s *Service) notifyOwnersBankRefund(ctx context.Context, tx *db.Tx, c sqlc.Contract, p sqlc.Payment, status string) error {
	t, err := tx.Q.GetTenant(ctx, c.TenantID)
	if err != nil {
		return err
	}
	short := names.Short(t.LastName, t.FirstName, t.MiddleName)
	if t.Kind == "legal" && t.OrgName != nil {
		short = *t.OrgName
	}
	title := "полный возврат"
	if status == tbank.StatusPartialRefunded {
		title = "частичный возврат"
	}
	owners, err := tx.Q.ActiveOwnerUsers(ctx)
	if err != nil {
		return err
	}
	for _, o := range owners {
		if err := s.notify.Queue(ctx, tx, notify.Email{
			Code: "owner_bank_refund", To: o.Email, RelatedType: "payment", RelatedID: &p.ID,
			DedupeKey: "owner_bank_refund:" + p.ID.String() + ":" + status + ":" + o.ID.String(),
			Vars: map[string]string{
				"tenant.short_name": short, "amount": money.Kop(p.AmountKop).Format(), "received_on": p.ReceivedOn.Format(),
				"receipt_number": valStr(p.ReceiptNumber), "status_title": title, "contract.number": c.Number,
				"link": s.notify.Link("/payments/" + p.ID.String()),
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func methodTitle(m string) string {
	switch m {
	case "online":
		return "онлайн-оплата"
	case MethodBankTransfer:
		return "перевод на счёт"
	case MethodCash:
		return "наличные"
	}
	return "зачёт из залога"
}

// ---------------------------------------------------------------- Проверка статуса (08 разд. 1.6)

// CheckStatus — статус платежа pending в банке (GetState) и его применение тем же кодом, что и уведомления
// (applyStatus: договор и платёж под FOR UPDATE), поэтому одновременные уведомление и проверка не задваивают
// проведение и квитанцию. Журнал пишется, только если статус платежа изменился (проведён или не прошёл).
func (s *Service) CheckStatus(ctx context.Context, paymentID uuid.UUID) error {
	p, err := s.db.Queries().GetPayment(ctx, paymentID)
	if db.IsNoRows(err) {
		return apperr.NotFound()
	}
	if err != nil {
		return err
	}
	if p.Source != SourceOnline || p.Status != "pending" || p.ProviderPaymentID == nil {
		return apperr.Conflict(apperr.CodeConflictState, "Статус проверяется только у незавершённой онлайн-оплаты")
	}
	st, err := s.online.Client.GetState(ctx, *p.ProviderPaymentID)
	if err != nil {
		return errUnavailable(err)
	}
	if !st.Success {
		return errUnavailable(fmt.Errorf("ответ GetState: %s %s", st.ErrorCode, st.Message))
	}
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		_, _, err := s.applyStatus(ctx, tx, p.ID.String(), *p.ProviderPaymentID, p.AmountKop, st.Status, s.clock.Now())
		return err
	})
}

// ExpirePending — задача payments.expire_pending (07): у онлайн-платежей pending с истёкшей ссылкой — GetState;
// оплачен — проводится, иначе failed (DEADLINE_EXPIRED). Отбор — по условию, а не по часу запуска: пропущенный
// запуск (без догоняющего) подберёт следующий (docs/open-questions.md, № 58).
func (s *Service) ExpirePending(ctx context.Context, st *jobs.Stats) error {
	list, err := s.db.Queries().ExpiredPendingPayments(ctx, ptr(s.clock.Now()))
	if err != nil {
		return err
	}
	for _, p := range list {
		st.Inc("payments")
		status := tbank.StatusDeadlineExpired
		if p.ProviderPaymentID != nil {
			resp, err := s.online.Client.GetState(ctx, *p.ProviderPaymentID)
			if err != nil {
				st.Fail("платёж "+p.ID.String(), err)
				continue
			}
			if resp.Success && resp.Status == tbank.StatusConfirmed {
				status = tbank.StatusConfirmed
			}
		}
		err := s.db.WithTx(ctx, func(tx *db.Tx) error {
			if p.ProviderPaymentID == nil {
				return tx.Q.SetPaymentFailed(ctx, sqlc.SetPaymentFailedParams{ID: p.ID, ProviderStatus: &status})
			}
			_, _, err := s.applyStatus(ctx, tx, p.ID.String(), *p.ProviderPaymentID, p.AmountKop, status, s.clock.Now())
			return err
		})
		if err != nil {
			st.Fail("платёж "+p.ID.String(), err)
			continue
		}
		st.Inc(strings.ToLower(status))
	}
	return nil
}

// ExpireJobName — задача payments.expire_pending (07 Фоновые задачи).
const ExpireJobName = "payments.expire_pending"

// ExpireArgs — аргументы payments.expire_pending.
type ExpireArgs struct{ jobs.DailyArgs }

// Kind — вид задачи River.
func (ExpireArgs) Kind() string { return ExpireJobName }

// InsertOpts — очередь integrations, уникальность по часу и запуску.
func (ExpireArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueIntegrations, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// ExpireHourly — описание задачи: каждый час в :15.
func ExpireHourly() jobs.Daily {
	return jobs.Daily{Name: ExpireJobName, Minute: 15, Hourly: true, Args: func(a jobs.DailyArgs) river.JobArgs { return ExpireArgs{a} }}
}

// ExpireWorker выполняет payments.expire_pending.
type ExpireWorker struct {
	river.WorkerDefaults[ExpireArgs]
	Svc *Service
}

// Work — запуск задачи с записью job_runs и итогом в журнале.
func (w *ExpireWorker) Work(ctx context.Context, job *river.Job[ExpireArgs]) error {
	_, err := w.Svc.RunExpirePending(ctx, job.Args.DailyArgs)
	return err
}

// RunExpirePending — payments.expire_pending с записью job_runs (07).
func (s *Service) RunExpirePending(ctx context.Context, args jobs.DailyArgs) (*jobs.Stats, error) {
	if args.BusinessDate.IsZero() {
		args.BusinessDate = s.clock.Today()
	}
	return jobs.RunDaily(ctx, s.db, s.clock, ExpireJobName, args, func(ctx context.Context, st *jobs.Stats) error {
		if err := s.ExpirePending(ctx, st); err != nil {
			return err
		}
		return s.db.WithTx(ctx, func(tx *db.Tx) error {
			return s.audit.Record(ctx, tx.Q, audit.Entry{
				Action: "job." + ExpireJobName, EntityType: "job", EntityID: ExpireJobName,
				Summary: fmt.Sprintf("Плановая задача %s: проверено платежей %d, ошибок %d", ExpireJobName,
					st.Counts["payments"], len(st.Errors)),
				After: map[string]any{"businessDate": args.BusinessDate.String(), "stats": st.Counts, "errors": len(st.Errors)},
			})
		})
	})
}
