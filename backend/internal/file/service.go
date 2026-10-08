// Package file — загрузка и выдача файлов (06 API, раздел 6; 12 Безопасность и ПДн, разделы 3 и 9).
package file

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"landlord/backend/internal/config"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/files"
	"landlord/backend/internal/platform/reqctx"
)

// Категории файлов.
const (
	CategoryPropertyPhoto = "property_photo"
	CategoryRequestPhoto  = "request_photo"
	CategoryExpenseDoc    = "expense_doc"
	CategoryConsentScan   = "consent_scan"
	CategorySignedScan    = "signed_scan"
	CategoryBankStatement = "bank_statement"
	CategoryOther         = "other"
)

// rule — допустимые типы и подсказка для ошибки.
type rule struct {
	allowed func(files.Kind) bool
	message string
}

func imageOrPDF(k files.Kind) bool { return k.IsImage() || k == files.KindPDF }

var rules = map[string]rule{
	CategoryPropertyPhoto: {files.Kind.IsImage, "Допустимы изображения JPEG, PNG или WebP"},
	CategoryRequestPhoto:  {files.Kind.IsImage, "Допустимы изображения JPEG, PNG или WebP"},
	CategoryExpenseDoc:    {imageOrPDF, "Допустимы PDF и изображения JPEG, PNG, WebP"},
	CategoryConsentScan:   {imageOrPDF, "Допустимы PDF и изображения JPEG, PNG, WebP"},
	CategorySignedScan:    {imageOrPDF, "Допустимы PDF и изображения JPEG, PNG, WebP"},
	CategoryOther:         {imageOrPDF, "Допустимы PDF и изображения JPEG, PNG, WebP"},
	CategoryBankStatement: {files.Kind.IsText, "Выписка — текстовый файл в формате 1С или CSV"},
}

// Service — файлы.
type Service struct {
	db    *db.DB
	store *files.Store
	clock clock.Clock
	max   int64
}

// NewService создаёт сервис файлов в каталоге FILES_DIR с пределом MAX_UPLOAD_MB.
func NewService(database *db.DB, cfg *config.Config, clk clock.Clock) *Service {
	return &Service{db: database, store: files.NewStore(cfg.FilesDir), clock: clk, max: int64(cfg.MaxUploadMB) << 20}
}

// Store — хранилище (для модулей, формирующих файлы).
func (s *Service) Store() *files.Store { return s.store }

// Upload сохраняет загруженный файл. Наниматель загружает только фото к заявкам.
func (s *Service) Upload(ctx context.Context, name, category string, r io.Reader) (sqlc.File, error) {
	a := reqctx.ActorFrom(ctx)
	if a == nil {
		return sqlc.File{}, apperr.Unauthorized()
	}
	if a.Role == reqctx.RoleTenant && category != CategoryRequestPhoto {
		return sqlc.File{}, apperr.Forbidden()
	}
	rl, ok := rules[category]
	if !ok {
		return sqlc.File{}, apperr.Validation(apperr.FieldError{Field: "category", Message: "Эта категория недоступна для загрузки"})
	}
	limit := s.max
	if category == CategoryBankStatement {
		limit = min(limit, 2<<20) // выписка — не больше 2 МБ (06 API, раздел 12)
	}
	st, err := s.store.Save(r, limit, s.clock.Now(), rl.allowed)
	switch {
	case errors.Is(err, files.ErrTooLarge):
		return sqlc.File{}, apperr.Validation(apperr.FieldError{Field: "file",
			Message: fmt.Sprintf("Файл больше %d МБ", limit>>20)})
	case errors.Is(err, files.ErrUnsupported):
		return sqlc.File{}, apperr.Validation(apperr.FieldError{Field: "file", Message: rl.message})
	case err != nil:
		return sqlc.File{}, err
	}
	f, err := s.db.Queries().InsertFile(ctx, sqlc.InsertFileParams{
		StorageKey: st.StorageKey, OriginalName: cleanName(name, st.Kind.Ext), ContentType: st.Kind.ContentType,
		SizeBytes: st.Size, Sha256: st.SHA256, Category: category, UploadedBy: &a.UserID, CreatedAt: s.clock.Now(),
	})
	if err != nil {
		_ = s.store.Remove(st.StorageKey)
		return sqlc.File{}, err
	}
	return f, nil
}

// Open — файл для выдачи с проверкой прав нанимателя (чужой файл — 404).
func (s *Service) Open(ctx context.Context, id uuid.UUID) (sqlc.File, *os.File, error) {
	q := s.db.Queries()
	f, err := q.GetFile(ctx, id)
	if db.IsNoRows(err) {
		return f, nil, apperr.NotFound()
	}
	if err != nil {
		return f, nil, err
	}
	if a := reqctx.ActorFrom(ctx); a == nil || a.Role != reqctx.RoleOwner {
		if a == nil || a.TenantID == nil {
			return f, nil, apperr.NotFound()
		}
		allowed, err := q.TenantCanReadFile(ctx, sqlc.TenantCanReadFileParams{FileID: id, UserID: &a.UserID, TenantID: *a.TenantID})
		if err != nil {
			return f, nil, err
		}
		if !allowed {
			return f, nil, apperr.NotFound()
		}
	}
	fh, err := s.store.Open(f.StorageKey)
	if err != nil {
		return f, nil, apperr.NotFoundDetail("Файл недоступен").Wrap(err)
	}
	return f, fh, nil
}

// Get — сведения о файле (без проверки прав, для модулей).
func (s *Service) Get(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (sqlc.File, error) {
	f, err := q.GetFile(ctx, id)
	if db.IsNoRows(err) {
		return f, apperr.Validation(apperr.FieldError{Field: "fileId", Message: "Файл не найден"})
	}
	return f, err
}

// URL — адрес выдачи файла.
func URL(id uuid.UUID) string { return "/api/v1/files/" + id.String() }

// cleanName — имя файла для отображения: без пути и управляющих символов, с расширением по типу.
func cleanName(name, ext string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == '"' || r == '/' {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." {
		name = "файл"
	}
	for utf8.RuneCountInString(name) > 200 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	lower := strings.ToLower(name)
	hasExt := strings.HasSuffix(lower, "."+ext) || ext == "jpg" && strings.HasSuffix(lower, ".jpeg")
	if !hasExt {
		name += "." + ext
	}
	return name
}
