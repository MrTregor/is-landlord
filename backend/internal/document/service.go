package document

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/microcosm-cc/bluemonday"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/file"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/pdf"
	"landlord/backend/internal/platform/reqctx"
	"landlord/backend/internal/platform/tmpl"
	"landlord/backend/internal/settings"
	"landlord/backend/internal/tenant"
)

var errNotFound = apperr.NotFound()

// Service — шаблоны документов и сформированные документы.
type Service struct {
	db        *db.DB
	audit     *audit.Service
	clock     clock.Clock
	settings  *settings.Service
	tenants   *tenant.Service
	files     *file.Service
	pdf       pdf.Converter
	publicURL string
	policy    *bluemonday.Policy
}

// NewService создаёт сервис документов.
func NewService(database *db.DB, au *audit.Service, clk clock.Clock, st *settings.Service, ts *tenant.Service,
	fs *file.Service, conv pdf.Converter, publicURL string,
) *Service {
	return &Service{db: database, audit: au, clock: clk, settings: st, tenants: ts, files: fs, pdf: conv,
		publicURL: publicURL, policy: templatePolicy()}
}

// templatePolicy — очистка HTML шаблона (12 разд. 9: bluemonday, политика UGC + таблицы):
// без <script>, on*-атрибутов и javascript:-ссылок; классы и стили базовой разметки документов сохраняются.
func templatePolicy() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowElements("table", "thead", "tbody", "tfoot", "tr", "th", "td", "colgroup", "col", "u", "s", "div", "span", "br")
	p.AllowAttrs("style").OnElements("p", "h1", "h2", "h3", "td", "th", "col", "table", "div", "span")
	p.AllowAttrs("colspan", "rowspan").OnElements("td", "th")
	p.AllowAttrs("class").Globally()
	return p
}

// Sanitize очищает HTML шаблона.
func (s *Service) Sanitize(body string) string { return strings.TrimSpace(s.policy.Sanitize(body)) }

// checkKeys — неизвестные поля и блоки → 422 «Неизвестные поля: …» (09 разд. 2).
func checkKeys(kind, body string) error {
	if unknown := tmpl.Unknown(body, Allowed(kind)); len(unknown) > 0 {
		return apperr.Validation(apperr.FieldError{Field: "bodyHtml", Message: "Неизвестные поля: " + strings.Join(unknown, ", ")})
	}
	return nil
}

// render — HTML документа: подстановка и базовые стили.
func render(title, body string, d Data) string {
	return wrap(title, tmpl.Render(body, d.Vars, d.Blocks, tmpl.Options{Empty: emptyValue}))
}

func (s *Service) convert(ctx context.Context, htmlDoc string) ([]byte, error) {
	out, err := s.pdf.Convert(ctx, htmlDoc)
	if err != nil {
		return nil, apperr.BadGateway("Сервис формирования PDF недоступен. Повторите попытку позже").Wrap(err)
	}
	return out, nil
}

func actorID(ctx context.Context) *uuid.UUID {
	if a := reqctx.ActorFrom(ctx); a != nil {
		id := a.UserID
		return &id
	}
	return nil
}

// subject — что формируется: вид, номер, название и связи документа.
type subject struct {
	kind                 string
	number               *string
	title                string
	contractID, tenantID *uuid.UUID
	actID, amendmentID   *uuid.UUID
	paymentID            *uuid.UUID
	periodFrom, periodTo *date.Date
	visibleToTenant      bool
}

// generate — подстановка в активный шаблон, PDF, файл, запись documents, замена прежнего документа и журнал
// (одна транзакция tx).
func (s *Service) generate(ctx context.Context, tx *db.Tx, sub subject, d Data) (sqlc.Document, error) {
	t, err := tx.Q.ActiveTemplate(ctx, sub.kind)
	if err != nil {
		return sqlc.Document{}, fmt.Errorf("активный шаблон %s: %w", sub.kind, err)
	}
	htmlDoc := render(sub.title, t.BodyHtml, d)
	data, err := s.convert(ctx, htmlDoc)
	if err != nil {
		return sqlc.Document{}, err
	}
	by := actorID(ctx)
	f, err := s.files.SaveGenerated(ctx, tx.Q, sub.title+".pdf", file.CategoryDocumentPDF, data, by)
	if err != nil {
		return sqlc.Document{}, err
	}
	snap, err := json.Marshal(map[string]any{"templateVersion": t.Version, "vars": d.Vars})
	if err != nil {
		return sqlc.Document{}, err
	}
	doc, err := tx.Q.InsertDocument(ctx, sqlc.InsertDocumentParams{
		Kind: sub.kind, Number: sub.number, Title: sub.title, ContractID: sub.contractID, TenantID: sub.tenantID,
		PaymentID: sub.paymentID, AmendmentID: sub.amendmentID, ActID: sub.actID, TemplateID: t.ID, FileID: f.ID,
		HtmlSnapshot: htmlDoc, DataSnapshot: snap, PeriodFrom: sub.periodFrom, PeriodTo: sub.periodTo,
		VisibleToTenant: sub.visibleToTenant, CreatedBy: by, CreatedAt: s.clock.Now(),
	})
	if err != nil {
		return doc, err
	}
	kinds := []string{sub.kind}
	if sub.kind == KindContractCh34 || sub.kind == KindContractCh35 {
		kinds = []string{KindContractCh35, KindContractCh34}
	}
	sup := sqlc.SupersedeDocumentsParams{NewID: &doc.ID, Kinds: kinds, ActID: sub.actID, AmendmentID: sub.amendmentID,
		PaymentID: sub.paymentID, PeriodFrom: sub.periodFrom, PeriodTo: sub.periodTo}
	if sub.kind == KindPdnConsent {
		sup.TenantID = sub.tenantID
	} else {
		sup.ContractID = sub.contractID
	}
	if err := tx.Q.SupersedeDocuments(ctx, sup); err != nil {
		return doc, err
	}
	err = s.audit.Record(ctx, tx.Q, audit.Entry{
		Action: "document.generate", EntityType: "document", EntityID: doc.ID.String(),
		Summary: "Сформирован документ «" + sub.title + "»",
		After:   map[string]any{"kind": sub.kind, "number": sub.number, "templateVersion": t.Version},
	})
	return doc, err
}

// ---------------------------------------------------------------- Формирование по видам

// ContractDocumentTx — документ договора по активному шаблону вида (05 разд. 4.4, п. 7; кнопка «Сформировать договор»).
func (s *Service) ContractDocumentTx(ctx context.Context, tx *db.Tx, contractID uuid.UUID) (sqlc.Document, error) {
	cc, err := s.loadContract(ctx, tx.Q, contractID)
	if err != nil {
		return sqlc.Document{}, err
	}
	d, err := s.contractData(ctx, tx.Q, cc)
	if err != nil {
		return sqlc.Document{}, err
	}
	title := contractName(cc.basis) + " № " + cc.c.Number
	if cc.c.ConcludedOn != nil {
		title += " от " + cc.c.ConcludedOn.Format()
	} else {
		title += " (проект)"
	}
	return s.generate(ctx, tx, subject{kind: cc.kindCh, number: &cc.c.Number, title: title, contractID: &cc.c.ID,
		visibleToTenant: true}, d)
}

// ContractDocument — то же в собственной транзакции.
func (s *Service) ContractDocument(ctx context.Context, contractID uuid.UUID) (sqlc.Document, error) {
	var doc sqlc.Document
	err := s.db.WithTx(ctx, func(tx *db.Tx) (err error) {
		doc, err = s.ContractDocumentTx(ctx, tx, contractID)
		return err
	})
	return doc, err
}

// ContractDocumentFresh — сформирован ли документ договора после последнего изменения черновика.
func (s *Service) ContractDocumentFresh(ctx context.Context, q *sqlc.Queries, c sqlc.Contract) (bool, error) {
	return q.ContractDocumentSince(ctx, sqlc.ContractDocumentSinceParams{ContractID: &c.ID, Since: c.UpdatedAt})
}

// ActDocumentTx — PDF акта (05 разд. 4.10: при подписании и по кнопке).
func (s *Service) ActDocumentTx(ctx context.Context, tx *db.Tx, actID uuid.UUID) (sqlc.Document, error) {
	a, err := tx.Q.GetAct(ctx, actID)
	if db.IsNoRows(err) {
		return sqlc.Document{}, errNotFound
	}
	if err != nil {
		return sqlc.Document{}, err
	}
	cc, err := s.loadContract(ctx, tx.Q, a.ContractID)
	if err != nil {
		return sqlc.Document{}, err
	}
	d, err := s.contractData(ctx, tx.Q, cc)
	if err != nil {
		return sqlc.Document{}, err
	}
	if err := s.actData(ctx, tx.Q, d, cc, a); err != nil {
		return sqlc.Document{}, err
	}
	kind, suffix, name := KindActHandover, "-АП", "Акт приёма-передачи"
	if a.Kind == "return" {
		kind, suffix, name = KindActReturn, "-АВ", "Акт возврата"
	}
	number := cc.c.Number + suffix
	title := name + " № " + number + " от " + a.ActDate.Format()
	doc, err := s.generate(ctx, tx, subject{kind: kind, number: &number, title: title, contractID: &cc.c.ID, actID: &a.ID,
		visibleToTenant: true}, d)
	if err != nil {
		return doc, err
	}
	return doc, tx.Q.SetActDocument(ctx, sqlc.SetActDocumentParams{ID: a.ID, DocumentID: &doc.ID})
}

// ActDocument — то же в собственной транзакции.
func (s *Service) ActDocument(ctx context.Context, actID uuid.UUID) (sqlc.Document, error) {
	var doc sqlc.Document
	err := s.db.WithTx(ctx, func(tx *db.Tx) (err error) {
		doc, err = s.ActDocumentTx(ctx, tx, actID)
		return err
	})
	return doc, err
}

// AmendmentDocument — PDF допсоглашения (05 разд. 8.3).
func (s *Service) AmendmentDocument(ctx context.Context, id uuid.UUID) (sqlc.Document, error) {
	var doc sqlc.Document
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		a, err := tx.Q.GetAmendment(ctx, id)
		if db.IsNoRows(err) {
			return errNotFound
		}
		if err != nil {
			return err
		}
		if a.Status == "cancelled" {
			return apperr.BusinessRule("Допсоглашение отменено — документ не формируется")
		}
		cc, err := s.loadContract(ctx, tx.Q, a.ContractID)
		if err != nil {
			return err
		}
		d, err := s.contractData(ctx, tx.Q, cc)
		if err != nil {
			return err
		}
		amendmentData(d, cc, a)
		number := fmt.Sprintf("%s/ДС-%d", cc.c.Number, a.Number)
		title := fmt.Sprintf("Дополнительное соглашение № %d к договору № %s", a.Number, cc.c.Number)
		doc, err = s.generate(ctx, tx, subject{kind: KindAmendment, number: &number, title: title, contractID: &cc.c.ID,
			amendmentID: &a.ID, visibleToTenant: true}, d)
		return err
	})
	return doc, err
}

// nextNumber — «АС-2026-001», «СГ-2026-001»: сквозной номер в году (number_sequences).
func (s *Service) nextNumber(ctx context.Context, q *sqlc.Queries, seq, prefix string) (string, error) {
	year := s.clock.Today().Year()
	n, err := q.NextSequenceValue(ctx, sqlc.NextSequenceValueParams{Name: seq, Year: int32(year)}) //nolint:gosec // год даты — четыре цифры
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%d-%03d", prefix, year, n), nil
}

// ReconciliationAct — акт сверки за период (09 разд. 4.6).
func (s *Service) ReconciliationAct(ctx context.Context, contractID uuid.UUID, from, to date.Date) (sqlc.Document, error) {
	if to.Before(from) {
		return sqlc.Document{}, apperr.Validation(apperr.FieldError{Field: "to", Message: "Конец периода раньше начала"})
	}
	var doc sqlc.Document
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		cc, err := s.loadContract(ctx, tx.Q, contractID)
		if err != nil {
			return err
		}
		if cc.c.Status == "draft" {
			return apperr.BusinessRule("Акт сверки формируется по договору, введённому в действие")
		}
		number, err := s.nextNumber(ctx, tx.Q, "reconciliation_act", "АС")
		if err != nil {
			return err
		}
		d, err := s.contractData(ctx, tx.Q, cc)
		if err != nil {
			return err
		}
		if err := reconData(ctx, tx.Q, d, cc, number, from, to); err != nil {
			return err
		}
		title := fmt.Sprintf("Акт сверки № %s за %s–%s", number, from.Format(), to.Format())
		doc, err = s.generate(ctx, tx, subject{kind: KindRecon, number: &number, title: title, contractID: &cc.c.ID,
			periodFrom: &from, periodTo: &to, visibleToTenant: true}, d)
		return err
	})
	return doc, err
}

// ConsentForm — бланк отдельного согласия на обработку ПДн (09 разд. 4.7; только физлицо).
func (s *Service) ConsentForm(ctx context.Context, tenantID uuid.UUID) (sqlc.Document, error) {
	var doc sqlc.Document
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		t, err := tx.Q.GetTenant(ctx, tenantID)
		if db.IsNoRows(err) {
			return errNotFound
		}
		if err != nil {
			return err
		}
		if t.Kind != tenant.KindIndividual {
			return apperr.BusinessRule("Бланк согласия формируется только для физического лица")
		}
		if t.AnonymizedAt != nil {
			return apperr.BusinessRule("Данные нанимателя обезличены")
		}
		o, err := s.settings.Get(ctx, tx.Q)
		if err != nil {
			return err
		}
		number, err := s.nextNumber(ctx, tx.Q, "pdn_consent", "СГ")
		if err != nil {
			return err
		}
		d := newData()
		if err := s.consentData(d, t, o); err != nil {
			return err
		}
		title := "Согласие на обработку персональных данных № " + number
		doc, err = s.generate(ctx, tx, subject{kind: KindPdnConsent, number: &number, title: title, tenantID: &t.ID,
			visibleToTenant: true}, d)
		return err
	})
	return doc, err
}

// ---------------------------------------------------------------- Документы

// Get — документ.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (sqlc.Document, error) {
	d, err := s.db.Queries().GetDocument(ctx, id)
	if db.IsNoRows(err) {
		return d, errNotFound
	}
	return d, err
}

// ContractDocuments — документы договора, новые сверху.
func (s *Service) ContractDocuments(ctx context.Context, contractID uuid.UUID) ([]sqlc.Document, error) {
	q := s.db.Queries()
	if _, err := q.GetContract(ctx, contractID); db.IsNoRows(err) {
		return nil, errNotFound
	} else if err != nil {
		return nil, err
	}
	return q.ListContractDocuments(ctx, &contractID)
}

// LastAmendmentDocuments — последний документ каждого допсоглашения.
func (s *Service) LastAmendmentDocuments(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	rows, err := s.db.Queries().LastAmendmentDocuments(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]uuid.UUID, len(rows))
	for _, r := range rows {
		if r.AmendmentID != nil {
			out[*r.AmendmentID] = r.ID
		}
	}
	return out, nil
}

// SetVisibility — видимость документа нанимателю.
func (s *Service) SetVisibility(ctx context.Context, id uuid.UUID, visible bool) (sqlc.Document, error) {
	var doc sqlc.Document
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		before, err := tx.Q.GetDocument(ctx, id)
		if db.IsNoRows(err) {
			return errNotFound
		}
		if err != nil {
			return err
		}
		if doc, err = tx.Q.SetDocumentVisibility(ctx, sqlc.SetDocumentVisibilityParams{ID: id, Visible: visible}); err != nil {
			return err
		}
		word := "скрыт от нанимателя"
		if visible {
			word = "виден нанимателю"
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "document.update", EntityType: "document", EntityID: id.String(),
			Summary: "Документ «" + doc.Title + "» " + word,
			Before:  map[string]any{"visibleToTenant": before.VisibleToTenant}, After: map[string]any{"visibleToTenant": visible},
		})
	})
	return doc, err
}

// AttachSignedScan — скан подписанного экземпляра (файл категории signed_scan).
func (s *Service) AttachSignedScan(ctx context.Context, id, fileID uuid.UUID) (sqlc.Document, error) {
	var doc sqlc.Document
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		if _, err := tx.Q.GetDocument(ctx, id); db.IsNoRows(err) {
			return errNotFound
		} else if err != nil {
			return err
		}
		f, err := s.files.Get(ctx, tx.Q, fileID)
		if err != nil {
			return err
		}
		if f.Category != file.CategorySignedScan {
			return apperr.Validation(apperr.FieldError{Field: "fileId", Message: "Загрузите скан подписанного экземпляра"})
		}
		if doc, err = tx.Q.SetDocumentSignedScan(ctx, sqlc.SetDocumentSignedScanParams{ID: id, FileID: &fileID}); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "document.update", EntityType: "document", EntityID: id.String(),
			Summary: "К документу «" + doc.Title + "» прикреплён подписанный экземпляр",
			After:   map[string]any{"signedScanFileId": fileID},
		})
	})
	return doc, err
}

// ---------------------------------------------------------------- Шаблоны

// TemplateDetail — активная версия и список версий.
type TemplateDetail struct {
	Active   sqlc.GetTemplateVersionRow
	Versions []sqlc.TemplateVersionsRow
}

func checkKind(kind string) error {
	if !IsKind(kind) {
		return errNotFound
	}
	return nil
}

// Templates — активные шаблоны по видам.
func (s *Service) Templates(ctx context.Context) ([]sqlc.ActiveTemplatesRow, error) {
	return s.db.Queries().ActiveTemplates(ctx)
}

// Template — активная версия вида и список версий.
func (s *Service) Template(ctx context.Context, kind string) (TemplateDetail, error) {
	var out TemplateDetail
	if err := checkKind(kind); err != nil {
		return out, err
	}
	q := s.db.Queries()
	t, err := q.ActiveTemplate(ctx, kind)
	if err != nil {
		return out, err
	}
	if out.Active, err = q.GetTemplateVersion(ctx, sqlc.GetTemplateVersionParams{Kind: kind, ID: t.ID}); err != nil {
		return out, err
	}
	out.Versions, err = q.TemplateVersions(ctx, kind)
	return out, err
}

// TemplateVersion — текст версии.
func (s *Service) TemplateVersion(ctx context.Context, kind string, id uuid.UUID) (sqlc.GetTemplateVersionRow, error) {
	if err := checkKind(kind); err != nil {
		return sqlc.GetTemplateVersionRow{}, err
	}
	v, err := s.db.Queries().GetTemplateVersion(ctx, sqlc.GetTemplateVersionParams{Kind: kind, ID: id})
	if db.IsNoRows(err) {
		return v, errNotFound
	}
	return v, err
}

func (s *Service) saveVersion(ctx context.Context, kind, name, body, action, summary string) (TemplateDetail, error) {
	var prev int32
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		if _, err := tx.Q.LockTemplateKind(ctx, kind); err != nil {
			return err
		}
		old, err := tx.Q.ActiveTemplate(ctx, kind)
		if err != nil {
			return err
		}
		prev = old.Version
		if err := tx.Q.DeactivateTemplates(ctx, kind); err != nil {
			return err
		}
		t, err := tx.Q.InsertTemplateVersion(ctx, sqlc.InsertTemplateVersionParams{Kind: kind, Name: name, BodyHtml: body,
			CreatedBy: actorID(ctx), CreatedAt: s.clock.Now()})
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: action, EntityType: "document_template", EntityID: t.ID.String(),
			Summary: fmt.Sprintf("%s «%s», версия %d", summary, name, t.Version),
			Before:  map[string]any{"version": prev}, After: map[string]any{"kind": kind, "version": t.Version},
		})
	})
	if err != nil {
		return TemplateDetail{}, err
	}
	return s.Template(ctx, kind)
}

// SaveTemplate — новая версия шаблона становится активной (FR-04.1). HTML очищается, ключи проверяются.
func (s *Service) SaveTemplate(ctx context.Context, kind, name, body string) (TemplateDetail, error) {
	if err := checkKind(kind); err != nil {
		return TemplateDetail{}, err
	}
	body = s.Sanitize(body)
	if body == "" {
		return TemplateDetail{}, apperr.Validation(apperr.FieldError{Field: "bodyHtml", Message: "Заполните текст шаблона"})
	}
	if err := checkKeys(kind, body); err != nil {
		return TemplateDetail{}, err
	}
	return s.saveVersion(ctx, kind, strings.TrimSpace(name), body, "template.update", "Изменён шаблон")
}

// ResetTemplate — новая версия с текстом системного шаблона.
func (s *Service) ResetTemplate(ctx context.Context, kind string) (TemplateDetail, error) {
	if err := checkKind(kind); err != nil {
		return TemplateDetail{}, err
	}
	sys, err := s.db.Queries().SystemTemplate(ctx, kind)
	if err != nil {
		return TemplateDetail{}, err
	}
	return s.saveVersion(ctx, kind, sys.Name, sys.BodyHtml, "template.reset", "Шаблон сброшен к стандартному:")
}

// Preview — PDF шаблона на демо-данных (без сохранения).
func (s *Service) Preview(ctx context.Context, kind, body string) ([]byte, error) {
	if err := checkKind(kind); err != nil {
		return nil, err
	}
	body = s.Sanitize(body)
	if err := checkKeys(kind, body); err != nil {
		return nil, err
	}
	return s.convert(ctx, render("Предпросмотр", body, examples(kind)))
}

// IsUnavailable — ошибка недоступности Gotenberg.
func IsUnavailable(err error) bool { return errors.Is(err, pdf.ErrUnavailable) }
