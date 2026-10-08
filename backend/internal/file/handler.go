package file

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"landlord/backend/internal/api"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/db/sqlc"
)

// Handler — методы API файлов.
type Handler struct {
	svc *Service
}

// NewHandler создаёт обработчик.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// UploadFile — multipart: file, category.
func (h *Handler) UploadFile(ctx context.Context, req api.UploadFileRequestObject) (api.UploadFileResponseObject, error) {
	var (
		category string
		name     string
		data     []byte
		gotFile  bool
	)
	for {
		part, err := req.Body.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, apperr.BadRequest("Неверный формат загрузки")
		}
		switch part.FormName() {
		case "category":
			b, _ := io.ReadAll(io.LimitReader(part, 64))
			category = string(b)
		case "file":
			name = part.FileName()
			// Читается на байт больше предела, чтобы сервис распознал превышение.
			data, err = io.ReadAll(io.LimitReader(part, h.svc.max+1))
			if err != nil {
				return nil, apperr.BadRequest("Не удалось прочитать файл")
			}
			gotFile = true
		}
		_ = part.Close()
	}
	if !gotFile {
		return nil, apperr.Validation(apperr.FieldError{Field: "file", Message: "Выберите файл"})
	}
	f, err := h.svc.Upload(ctx, name, category, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return api.UploadFile201JSONResponse(DTO(f)), nil
}

// GetFile — содержимое с сохранённым типом (inline или attachment при ?download=1).
func (h *Handler) GetFile(ctx context.Context, req api.GetFileRequestObject) (api.GetFileResponseObject, error) {
	f, fh, err := h.svc.Open(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	download := req.Params.Download != nil && *req.Params.Download == 1
	return Response{File: fh, Name: f.OriginalName, ContentType: f.ContentType, Size: f.SizeBytes, Download: download}, nil
}

// Response — выдача файла: подходит для любого метода, отдающего содержимое (файлы, PDF документов).
type Response struct {
	File        *os.File
	Name        string
	ContentType string
	Size        int64
	Download    bool
}

// Disposition — заголовок Content-Disposition с именем на русском (RFC 5987).
func Disposition(name string, download bool) string {
	kind := "inline"
	if download {
		kind = "attachment"
	}
	return fmt.Sprintf(`%s; filename="file"; filename*=UTF-8''%s`, kind, url.PathEscape(name))
}

func (r Response) write(w http.ResponseWriter) error {
	defer func() { _ = r.File.Close() }()
	ct := r.ContentType
	if _, _, err := mime.ParseMediaType(ct); err != nil {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.FormatInt(r.Size, 10))
	w.Header().Set("Content-Disposition", Disposition(r.Name, r.Download))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, err := io.Copy(w, r.File)
	return err
}

// VisitGetFileResponse реализует ответ GET /files/{id}.
func (r Response) VisitGetFileResponse(w http.ResponseWriter) error { return r.write(w) }

// DTO — файл для API.
func DTO(f sqlc.File) api.FileDTO {
	return api.FileDTO{Id: f.ID, Name: f.OriginalName, ContentType: f.ContentType, SizeBytes: f.SizeBytes, Url: URL(f.ID)}
}
