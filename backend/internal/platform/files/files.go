// Package files — файлы на диске в FILES_DIR: определение типа по сигнатуре (не по расширению),
// случайное имя, контрольная сумма (12 Безопасность и ПДн, раздел 9; 06 API, раздел 6).
package files

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Kind — тип содержимого, определённый по сигнатуре.
type Kind struct {
	ContentType string
	Ext         string
}

// Известные типы.
var (
	KindJPEG = Kind{"image/jpeg", "jpg"}
	KindPNG  = Kind{"image/png", "png"}
	KindWebP = Kind{"image/webp", "webp"}
	KindPDF  = Kind{"application/pdf", "pdf"}
	KindText = Kind{"text/plain; charset=utf-8", "txt"}
	KindCSV  = Kind{"text/csv", "csv"}
)

// ErrUnsupported — тип файла не распознан или запрещён (SVG, HTML и т. п.).
var ErrUnsupported = errors.New("тип файла не поддерживается")

// Detect определяет тип по первым байтам. Текст — без нулевых байтов и не разметка (HTML/SVG/XML запрещены).
func Detect(head []byte) (Kind, error) {
	switch {
	case bytes.HasPrefix(head, []byte{0xFF, 0xD8, 0xFF}):
		return KindJPEG, nil
	case bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n")):
		return KindPNG, nil
	case len(head) >= 12 && bytes.Equal(head[:4], []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WEBP")):
		return KindWebP, nil
	case bytes.HasPrefix(head, []byte("%PDF-")):
		return KindPDF, nil
	}
	if len(head) == 0 || bytes.IndexByte(head, 0) >= 0 {
		return Kind{}, ErrUnsupported
	}
	trimmed := bytes.TrimLeft(bytes.TrimPrefix(head, []byte("\xEF\xBB\xBF")), " \t\r\n")
	if bytes.HasPrefix(trimmed, []byte("<")) {
		return Kind{}, ErrUnsupported
	}
	// Текст в UTF-8 или однобайтовой кодировке (выписки 1С в Windows-1251).
	return KindText, nil
}

// IsImage — JPEG, PNG или WebP.
func (k Kind) IsImage() bool { return k == KindJPEG || k == KindPNG || k == KindWebP }

// IsText — текстовый файл.
func (k Kind) IsText() bool { return k == KindText || k == KindCSV }

// Stored — сохранённый файл.
type Stored struct {
	StorageKey string
	Size       int64
	SHA256     []byte
	Kind       Kind
}

// Store — каталог файлов.
type Store struct {
	dir string
}

// NewStore создаёт хранилище в каталоге FILES_DIR (каталог создаётся при необходимости).
func NewStore(dir string) *Store { return &Store{dir: dir} }

// ErrTooLarge — превышен предельный размер.
var ErrTooLarge = errors.New("файл слишком большой")

// Save читает содержимое (не больше maxBytes), определяет тип и сохраняет под случайным именем ГГГГ/ММ/<uuid>.<ext>.
// Если тип не входит в allowed — ErrUnsupported.
func (s *Store) Save(r io.Reader, maxBytes int64, now time.Time, allowed func(Kind) bool) (*Stored, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrTooLarge
	}
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	kind, err := Detect(head)
	if err != nil {
		return nil, err
	}
	if allowed != nil && !allowed(kind) {
		return nil, ErrUnsupported
	}
	key := fmt.Sprintf("%04d/%02d/%s.%s", now.Year(), int(now.Month()), uuid.Must(uuid.NewV7()), kind.Ext)
	path := s.path(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	return &Stored{StorageKey: key, Size: int64(len(data)), SHA256: sum[:], Kind: kind}, nil
}

// Open открывает файл по ключу хранения.
func (s *Store) Open(key string) (*os.File, error) { return os.Open(s.path(key)) }

// Remove удаляет файл с диска (отсутствие файла — не ошибка).
func (s *Store) Remove(key string) error {
	err := os.Remove(s.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) path(key string) string {
	clean := filepath.Clean("/" + strings.ReplaceAll(key, "\\", "/"))
	return filepath.Join(s.dir, filepath.FromSlash(clean))
}
