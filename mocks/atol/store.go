package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Статусы документа АТОЛ.
const (
	StatusWait = "wait"
	StatusDone = "done"
	StatusFail = "fail"
)

// APIError — ошибка в ответах АТОЛ: { code, text, type }.
type APIError struct {
	Code int    `json:"code"`
	Text string `json:"text"`
	Type string `json:"type"`
}

// Attempt — попытка доставки колбэка.
type Attempt struct {
	At     time.Time `json:"at"`
	Result string    `json:"result"`
}

// Requisites — фискальные реквизиты документа (поле payload отчёта, 08 разд. 3.3).
type Requisites struct {
	FnNumber                string    `json:"fnNumber"`
	ShiftNumber             int       `json:"shiftNumber"`
	FiscalReceiptNumber     int       `json:"fiscalReceiptNumber"`
	FiscalDocumentNumber    int64     `json:"fiscalDocumentNumber"`
	FiscalDocumentAttribute int64     `json:"fiscalDocumentAttribute"`
	EcrRegistrationNumber   string    `json:"ecrRegistrationNumber"`
	ReceiptDatetime         time.Time `json:"receiptDatetime"`
}

// Doc — фискальный документ (чек прихода или возврата прихода).
type Doc struct {
	UUID        string          `json:"uuid"`
	ExternalID  string          `json:"externalId"`
	Group       string          `json:"group"`
	Operation   string          `json:"operation"` // sell или sell_refund
	Request     json.RawMessage `json:"request"`
	Canonical   string          `json:"canonical"` // тело в каноническом виде: сравнение повторов external_id
	TotalKop    int64           `json:"totalKop"`
	CallbackURL string          `json:"callbackUrl"`
	Status      string          `json:"status"`
	CreatedAt   time.Time       `json:"createdAt"`
	DueAt       time.Time       `json:"dueAt"`    // момент перехода в итоговый статус
	WillFail    bool            `json:"willFail"` // переключатель «Следующий документ завершится ошибкой»
	Error       *APIError       `json:"error"`
	Requisites  *Requisites     `json:"requisites"`
	Callbacks   []Attempt       `json:"callbacks"`
	CallbackOff bool            `json:"callbackOff"` // колбэк не отправлялся («Не отправлять колбэки»)
}

// State — всё состояние эмулятора (сохраняется в STATE_FILE после каждого изменения).
type State struct {
	FirstDay    string         `json:"firstDay"`    // дата первого запуска по Москве (номер смены — номер дня с неё)
	DocSeq      int64          `json:"docSeq"`      // последний номер фискального документа
	DocNumbers  int64          `json:"docNumbers"`  // счётчик uuid документов
	Shifts      map[string]int `json:"shifts"`      // номер смены → чеков в смене
	FailNext    bool           `json:"failNext"`    // следующий документ завершится ошибкой
	DelayMs     *int           `json:"delayMs"`     // задержка обработки (nil — PROCESS_DELAY_MS)
	NoCallbacks bool           `json:"noCallbacks"` // не отправлять колбэки
	Docs        []*Doc         `json:"docs"`
}

// firstDocumentNumber — первый номер фискального документа (08 разд. 4: растёт от 1000).
const firstDocumentNumber = 1000

// Store — хранилище в памяти с сохранением в файл.
type Store struct {
	mu   sync.Mutex
	file string
	st   State
}

// NewStore читает состояние из файла (если он есть).
func NewStore(file string) (*Store, error) {
	s := &Store{file: file, st: State{DocSeq: firstDocumentNumber - 1, Shifts: map[string]int{}}}
	if file == "" {
		return s, nil
	}
	b, err := os.ReadFile(file)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, err
	}
	if len(b) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(b, &s.st); err != nil {
		return nil, err
	}
	if s.st.DocSeq < firstDocumentNumber-1 {
		s.st.DocSeq = firstDocumentNumber - 1
	}
	if s.st.Shifts == nil {
		s.st.Shifts = map[string]int{}
	}
	return s, nil
}

// Init запоминает день первого запуска и записывает файл состояния (ошибка записи — эмулятор не запускается).
func (s *Store) Init(now time.Time) error {
	return s.With(func(st *State) bool {
		if st.FirstDay == "" {
			st.FirstDay = now.In(moscow).Format(time.DateOnly)
		}
		return true
	})
}

// With выполняет fn под блокировкой и сохраняет состояние, если fn вернула true.
func (s *Store) With(fn func(st *State) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !fn(&s.st) {
		return nil
	}
	return s.saveLocked()
}

// Read выполняет fn под блокировкой без сохранения.
func (s *Store) Read(fn func(st *State)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.st)
}

func (s *Store) saveLocked() error {
	if s.file == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.file), 0o755); err != nil {
		return err
	}
	tmp := s.file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.file)
}

// Find — документ по uuid.
func (st *State) Find(uuid string) *Doc {
	for _, d := range st.Docs {
		if d.UUID == uuid {
			return d
		}
	}
	return nil
}

// LastByExternal — последний документ группы с данным external_id.
func (st *State) LastByExternal(group, externalID string) *Doc {
	for i := len(st.Docs) - 1; i >= 0; i-- {
		if d := st.Docs[i]; d.Group == group && d.ExternalID == externalID {
			return d
		}
	}
	return nil
}

// NextUUID — идентификатор документа в виде uuid (детерминированный счётчик, без внешних зависимостей).
func (st *State) NextUUID() string {
	st.DocNumbers++
	h := strconv.FormatInt(st.DocNumbers, 16)
	for len(h) < 12 {
		h = "0" + h
	}
	return "a7010000-0000-4000-8000-" + h
}

// shiftNumber — номер смены: номер дня (по Москве) с первого запуска, начиная с 1.
func (st *State) shiftNumber(now time.Time) int {
	first, err := time.ParseInLocation(time.DateOnly, st.FirstDay, moscow)
	if err != nil {
		return 1
	}
	day := now.In(moscow)
	today := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, moscow)
	return int(today.Sub(first)/(24*time.Hour)) + 1
}

// copyDoc — копия документа для чтения вне блокировки.
func copyDoc(d *Doc) Doc {
	c := *d
	c.Callbacks = append([]Attempt(nil), d.Callbacks...)
	if d.Error != nil {
		e := *d.Error
		c.Error = &e
	}
	if d.Requisites != nil {
		r := *d.Requisites
		c.Requisites = &r
	}
	return c
}
