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

// Статусы платежа эмулятора (подмножество статусов Т-Банка).
const (
	StatusNew             = "NEW"
	StatusFormShowed      = "FORM_SHOWED"
	StatusAuthorized      = "AUTHORIZED"
	StatusConfirmed       = "CONFIRMED"
	StatusRejected        = "REJECTED"
	StatusDeadlineExpired = "DEADLINE_EXPIRED"
)

// Attempt — попытка доставки уведомления.
type Attempt struct {
	At     time.Time `json:"at"`
	Result string    `json:"result"`
}

// Notice — уведомление о смене статуса и его доставка.
type Notice struct {
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	Attempts  []Attempt `json:"attempts"`
	Delivered bool      `json:"delivered"`
	GaveUp    bool      `json:"gaveUp"`
}

// Payment — платёж эмулятора.
type Payment struct {
	PaymentID       string            `json:"paymentId"`
	TerminalKey     string            `json:"terminalKey"`
	OrderID         string            `json:"orderId"`
	Description     string            `json:"description"`
	Amount          int64             `json:"amount"`
	Status          string            `json:"status"`
	ErrorCode       string            `json:"errorCode"`
	Message         string            `json:"message,omitempty"`
	Method          string            `json:"method,omitempty"` // card или sbp
	NotificationURL string            `json:"notificationUrl"`
	SuccessURL      string            `json:"successUrl"`
	FailURL         string            `json:"failUrl"`
	RedirectDueDate time.Time         `json:"redirectDueDate"`
	Data            map[string]string `json:"data,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
	Notices         []*Notice         `json:"notices"`
}

// State — всё состояние эмулятора (сохраняется в STATE_FILE после каждого изменения).
type State struct {
	Seq      int64      `json:"seq"`
	Payments []*Payment `json:"payments"`
}

// firstPaymentID — PaymentId первого платежа (08 разд. 2.2).
const firstPaymentID = 7000000001

// Store — хранилище в памяти с сохранением в файл.
type Store struct {
	mu   sync.Mutex
	file string
	st   State
}

// NewStore читает состояние из файла (если он есть).
func NewStore(file string) (*Store, error) {
	s := &Store{file: file, st: State{Seq: firstPaymentID - 1}}
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
	if s.st.Seq < firstPaymentID-1 {
		s.st.Seq = firstPaymentID - 1
	}
	return s, nil
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

// Save записывает состояние в файл.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
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

// NextID — следующий PaymentId (строка цифр).
func (st *State) NextID() string {
	st.Seq++
	return strconv.FormatInt(st.Seq, 10)
}

// Find — платёж по PaymentId.
func (st *State) Find(id string) *Payment {
	for _, p := range st.Payments {
		if p.PaymentID == id {
			return p
		}
	}
	return nil
}

// HasOrder — есть ли платёж с таким OrderId в терминале.
func (st *State) HasOrder(terminal, order string) bool {
	for _, p := range st.Payments {
		if p.TerminalKey == terminal && p.OrderID == order {
			return true
		}
	}
	return false
}

// copyPayment — копия платежа для чтения вне блокировки.
func copyPayment(p *Payment) Payment {
	c := *p
	c.Notices = make([]*Notice, len(p.Notices))
	for i, n := range p.Notices {
		nc := *n
		nc.Attempts = append([]Attempt(nil), n.Attempts...)
		c.Notices[i] = &nc
	}
	return c
}
