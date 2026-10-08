package files

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectBySignature(t *testing.T) {
	cases := []struct {
		name string
		head []byte
		want Kind
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10}, KindJPEG},
		{"png", []byte("\x89PNG\r\n\x1a\n...."), KindPNG},
		{"webp", []byte("RIFF\x00\x00\x00\x00WEBPVP8 "), KindWebP},
		{"pdf", []byte("%PDF-1.7\n"), KindPDF},
		{"csv", []byte("Дата;Сумма;Плательщик\n"), KindText},
		{"1c", []byte("1CClientBankExchange\r\n\xc2\xe5\xf0\xf1\xe8\xff"), KindText},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k, err := Detect(c.head)
			require.NoError(t, err)
			assert.Equal(t, c.want, k)
		})
	}
}

// SVG и HTML запрещены, тип определяется не по расширению (12 Безопасность и ПДн, раздел 9).
func TestDetectRejectsMarkupAndBinary(t *testing.T) {
	for _, head := range [][]byte{
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`),
		[]byte("\xEF\xBB\xBF  <html><script>alert(1)</script>"),
		[]byte("<?xml version=\"1.0\"?>"),
		{0x4D, 0x5A, 0x90, 0x00}, // исполняемый файл
		{},
	} {
		_, err := Detect(head)
		assert.ErrorIs(t, err, ErrUnsupported, string(head))
	}
}

func TestSaveOpenRemove(t *testing.T) {
	s := NewStore(t.TempDir())
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{1}, 100)...)

	st, err := s.Save(bytes.NewReader(png), 1024, now, Kind.IsImage)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(st.StorageKey, "2026/10/"))
	assert.True(t, strings.HasSuffix(st.StorageKey, ".png"))
	assert.Equal(t, int64(len(png)), st.Size)
	assert.Len(t, st.SHA256, 32)

	f, err := s.Open(st.StorageKey)
	require.NoError(t, err)
	got, _ := io.ReadAll(f)
	_ = f.Close()
	assert.Equal(t, png, got)

	require.NoError(t, s.Remove(st.StorageKey))
	require.NoError(t, s.Remove(st.StorageKey))
	_, err = s.Open(st.StorageKey)
	assert.Error(t, err)

	_, err = s.Save(bytes.NewReader(png), 50, now, nil)
	assert.ErrorIs(t, err, ErrTooLarge)
	_, err = s.Save(strings.NewReader("%PDF-1.4"), 1024, now, Kind.IsImage)
	assert.ErrorIs(t, err, ErrUnsupported, "PDF вместо фото")
}

func TestPathCannotEscapeDir(t *testing.T) {
	s := NewStore("/data/files")
	assert.NotContains(t, s.path("../../etc/passwd"), "..")
}
