package reconciliation

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/google/uuid"

	"landlord/backend/internal/api"
	"landlord/backend/internal/file"
	"landlord/backend/internal/payment"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
)

// maxStatementBytes — предел размера выписки (06 разд. 12).
const maxStatementBytes = 2 << 20

// bom — метка порядка байтов UTF-8.
const bom = "\xEF\xBB\xBF"

// Handler — методы API выписок банка (06 разд. 12).
type Handler struct {
	svc *Service
}

// NewHandler создаёт обработчик.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func statementDTO(r sqlc.ListBankStatementsRow) api.BankStatementDTO {
	return api.BankStatementDTO{
		Id: r.ID, FileId: r.FileID, FileName: r.FileName, Format: api.StatementFormat(r.Format), AccountNumber: r.AccountNumber,
		DateFrom: apiconv.FromDatePtr(r.DateFrom), DateTo: apiconv.FromDatePtr(r.DateTo), LinesTotal: int(r.LinesTotal),
		LinesIncoming: int(r.LinesIncoming), LinesMatched: int(r.LinesMatched), LinesUnmatched: int(r.LinesUnmatched),
		LinesDuplicate: int(r.LinesDuplicate), UploadedAt: r.UploadedAt, UploadedBy: r.UploadedByName,
	}
}

func (h *Handler) statement(ctx context.Context, id uuid.UUID) (api.BankStatementDTO, error) {
	r, err := h.svc.db.Queries().GetBankStatementRow(ctx, id)
	if db.IsNoRows(err) {
		return api.BankStatementDTO{}, apperr.NotFound()
	}
	if err != nil {
		return api.BankStatementDTO{}, err
	}
	return statementDTO(sqlc.ListBankStatementsRow(r)), nil
}

// ListBankStatements — GET /bank-statements.
func (h *Handler) ListBankStatements(ctx context.Context, req api.ListBankStatementsRequestObject) (api.ListBankStatementsResponseObject, error) {
	paging := apiconv.NewPaging(req.Params.Page, req.Params.PageSize)
	q := h.svc.db.Queries()
	rows, err := q.ListBankStatements(ctx, sqlc.ListBankStatementsParams{Lim: paging.Limit(), Off: paging.Offset()})
	if err != nil {
		return nil, err
	}
	n, err := q.CountBankStatements(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]api.BankStatementDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, statementDTO(r))
	}
	return api.ListBankStatements200JSONResponse{Items: items, Total: int(n), Page: paging.Page, PageSize: paging.PageSize}, nil
}

// UploadBankStatement — POST /bank-statements (multipart: file).
func (h *Handler) UploadBankStatement(ctx context.Context, req api.UploadBankStatementRequestObject) (
	api.UploadBankStatementResponseObject, error,
) {
	var (
		name string
		data []byte
		got  bool
	)
	for {
		part, err := req.Body.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, apperr.BadRequest("Неверный формат загрузки")
		}
		if part.FormName() == "file" {
			name = part.FileName()
			// На байт больше предела, чтобы распознать превышение.
			if data, err = io.ReadAll(io.LimitReader(part, maxStatementBytes+1)); err != nil {
				return nil, apperr.BadRequest("Не удалось прочитать файл")
			}
			got = true
		}
		_ = part.Close()
	}
	switch {
	case !got || len(data) == 0:
		return nil, apperr.Validation(apperr.FieldError{Field: "file", Message: "Выберите файл выписки"})
	case len(data) > maxStatementBytes:
		return nil, apperr.Validation(apperr.FieldError{Field: "file", Message: "Файл больше 2 МБ"})
	}
	id, err := h.svc.Upload(ctx, name, data)
	if err != nil {
		return nil, err
	}
	d, err := h.statement(ctx, id)
	return api.UploadBankStatement201JSONResponse(d), err
}

// csvTemplate — образец CSV (08 разд. 7.2).
var csvTemplate = strings.Join([]string{
	CSVHeader,
	"05.10.2026;28 000,00;Смирнова Анна Сергеевна;;40817810100000000001;Оплата по договору № 2026-001 за октябрь 2026;125",
	"06.10.2026;45000.00;ООО «Ромашка»;5075123457;40702810400000000002;Арендная плата за октябрь 2026, без НДС;318",
}, "\r\n") + "\r\n"

// GetBankStatementCsvTemplate — GET /bank-statements/csv-template (UTF-8 с BOM — открывается в Excel).
func (h *Handler) GetBankStatementCsvTemplate(_ context.Context, _ api.GetBankStatementCsvTemplateRequestObject) (
	api.GetBankStatementCsvTemplateResponseObject, error,
) {
	body := bom + csvTemplate
	return api.GetBankStatementCsvTemplate200TextcsvResponse{
		Body: strings.NewReader(body), ContentLength: int64(len(body)),
		Headers: api.GetBankStatementCsvTemplate200ResponseHeaders{
			ContentDisposition: apiconv.Ptr(file.Disposition("Образец выписки.csv", true)),
		},
	}, nil
}

// GetBankStatement — GET /bank-statements/{id}: выписка со строками.
func (h *Handler) GetBankStatement(ctx context.Context, req api.GetBankStatementRequestObject) (api.GetBankStatementResponseObject, error) {
	st, err := h.statement(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	var status *string
	if req.Params.LineStatus != nil {
		status = apiconv.Ptr(string(*req.Params.LineStatus))
	}
	q := h.svc.db.Queries()
	rows, err := q.StatementLines(ctx, sqlc.StatementLinesParams{StatementID: req.Id, Status: status})
	if err != nil {
		return nil, err
	}
	out := api.BankStatementDetail{
		Id: st.Id, FileId: st.FileId, FileName: st.FileName, Format: st.Format, AccountNumber: st.AccountNumber,
		DateFrom: st.DateFrom, DateTo: st.DateTo, LinesTotal: st.LinesTotal, LinesIncoming: st.LinesIncoming,
		LinesMatched: st.LinesMatched, LinesUnmatched: st.LinesUnmatched, LinesDuplicate: st.LinesDuplicate,
		UploadedAt: st.UploadedAt, UploadedBy: st.UploadedBy, Lines: make([]api.BankLineDTO, 0, len(rows)),
	}
	for _, r := range rows {
		d, err := payment.BankLineDTO(ctx, q, sqlc.BankLineRowRow(r))
		if err != nil {
			return nil, err
		}
		out.Lines = append(out.Lines, d)
	}
	return api.GetBankStatement200JSONResponse(out), nil
}

func (h *Handler) line(ctx context.Context, id uuid.UUID) (api.BankLineDTO, error) {
	q := h.svc.db.Queries()
	r, err := q.BankLineRow(ctx, id)
	if err != nil {
		return api.BankLineDTO{}, err
	}
	return payment.BankLineDTO(ctx, q, r)
}

// MarkLineSamePayment — POST /bank-statement-lines/{id}/same-payment.
func (h *Handler) MarkLineSamePayment(ctx context.Context, req api.MarkLineSamePaymentRequestObject) (
	api.MarkLineSamePaymentResponseObject, error,
) {
	if err := h.svc.SamePayment(ctx, req.Id, req.Body.PaymentId); err != nil {
		return nil, err
	}
	d, err := h.line(ctx, req.Id)
	return api.MarkLineSamePayment200JSONResponse(d), err
}

// MarkLineDifferentPayment — POST /bank-statement-lines/{id}/different-payment.
func (h *Handler) MarkLineDifferentPayment(ctx context.Context, req api.MarkLineDifferentPaymentRequestObject) (
	api.MarkLineDifferentPaymentResponseObject, error,
) {
	if err := h.svc.DifferentPayment(ctx, req.Id); err != nil {
		return nil, err
	}
	d, err := h.line(ctx, req.Id)
	return api.MarkLineDifferentPayment200JSONResponse(d), err
}
