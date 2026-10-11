package pdf

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Запрос к Gotenberg: index.html, footer.html и поля страницы A4 (08 разд. 6).
func TestConvertSendsDocumentAndPageSettings_FR04_1(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/forms/chromium/convert/html", r.URL.Path)
		require.NoError(t, r.ParseMultipartForm(1<<20))
		assert.Equal(t, "8.27", r.FormValue("paperWidth"))
		assert.Equal(t, "1.18", r.FormValue("marginLeft"))
		names := map[string]string{}
		for _, fh := range r.MultipartForm.File["files"] {
			f, _ := fh.Open()
			b, _ := io.ReadAll(f)
			names[fh.Filename] = string(b)
		}
		assert.Contains(t, names["index.html"], "Договор")
		assert.Contains(t, names["footer.html"], "pageNumber")
		_, _ = w.Write([]byte("%PDF-1.7 test"))
	}))
	defer srv.Close()
	out, err := New(srv.URL).Convert(context.Background(), "<p>Договор</p>")
	require.NoError(t, err)
	assert.Equal(t, "%PDF-1.7 test", string(out))
}

// Два повтора при 5xx, затем ошибка ErrUnavailable; 4xx не повторяется.
func TestConvertRetriesOn5xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := New(srv.URL)
	c.pause = 0
	_, err := c.Convert(context.Background(), "<p>x</p>")
	assert.True(t, errors.Is(err, ErrUnavailable))
	assert.Equal(t, int32(3), calls.Load())

	calls.Store(0)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer bad.Close()
	c = New(bad.URL)
	_, err = c.Convert(context.Background(), "<p>x</p>")
	assert.Error(t, err)
	assert.Equal(t, int32(1), calls.Load())
}
