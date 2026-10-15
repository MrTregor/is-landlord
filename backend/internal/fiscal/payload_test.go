package fiscal

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"landlord/backend/internal/platform/date"
)

var msk = time.FixedZone("MSK", 3*3600)

// example — чек прихода из примера 08 разд. 3.2 (юрлицо ООО «Ромашка», УСН «доходы»).
func example() Input {
	return Input{
		Operation: "sell", ExternalID: "0199f1b2-0000-7000-8000-000000000001",
		Timestamp: time.Date(2026, 10, 16, 16, 30, 0, 0, time.UTC), Location: msk,
		CallbackURL:  "http://server:8080/api/v1/integrations/atol/callback",
		Client:       Client{Email: "tenant@example.ru", Phone: "+7 900 000-00-00", Legal: true, Name: "ООО «Ромашка»", INN: "5075000000"},
		CompanyEmail: "owner@example.ru", CompanyINN: "507500363778", TaxRegime: "usn_income", PaymentAddress: "https://landlord.example",
		Items:       []Item{{Name: "Арендная плата за октябрь 2026, договор № 2026-011", SumKop: 4500000}},
		PaymentType: PaymentCashless, TotalKop: 4500000,
	}
}

// Запрос совпадает с примером 08 разд. 3.2: числа — рубли с двумя знаками, количество 1.000, без float (FR-06.2).
func TestBuildExample_FR_06_2(t *testing.T) {
	b, err := Build(example())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"external_id":"0199f1b2-0000-7000-8000-000000000001","timestamp":"16.10.2026 19:30:00",` +
		`"service":{"callback_url":"http://server:8080/api/v1/integrations/atol/callback"},` +
		`"receipt":{"client":{"email":"tenant@example.ru","name":"ООО «Ромашка»","inn":"5075000000"},` +
		`"company":{"email":"owner@example.ru","sno":"usn_income","inn":"507500363778","payment_address":"https://landlord.example"},` +
		`"items":[{"name":"Арендная плата за октябрь 2026, договор № 2026-011","price":45000.00,"quantity":1.000,"sum":45000.00,` +
		`"measure":0,"payment_method":"full_payment","payment_object":4,"vat":{"type":"none"}}],` +
		`"payments":[{"type":1,"sum":45000.00}],"total":45000.00}}`
	if string(b) != want {
		t.Fatalf("запрос:\n%s\nожидалось:\n%s", b, want)
	}
}

// Покупатель: e-mail или телефон (что-то одно); ФИО физлица не передаётся (08 разд. 3.2, 12 разд. 5, NFR-Sec-03).
func TestBuildClient_NFR_Sec_03(t *testing.T) {
	in := example()
	in.Client = Client{Email: "", Phone: "8 (900) 123-45-67", Name: "Иванов Иван Иванович"}
	b, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"client":{"phone":"+79001234567"}`) {
		t.Fatalf("физлицо без e-mail: %s", b)
	}
	in.Client = Client{Email: "a@b.ru", Phone: "+79001234567", Name: "Иванов Иван Иванович"}
	b, _ = Build(in)
	if !strings.Contains(string(b), `"client":{"email":"a@b.ru"}`) {
		t.Fatalf("физлицо с e-mail: %s", b)
	}
	in.Client = Client{Name: "Иванов"}
	_, err = Build(in)
	var de *DataError
	if !errors.As(err, &de) || de.Text != "У нанимателя нет e-mail или телефона для отправки чека" {
		t.Fatalf("нет контактов: %v", err)
	}
}

func TestBuildCompanyRules(t *testing.T) {
	for regime, sno := range map[string]string{"usn_income": "usn_income", "usn_income_expense": "usn_income_outcome", "osn": "osn", "psn": "patent"} {
		in := example()
		in.TaxRegime = regime
		b, err := Build(in)
		if err != nil || !strings.Contains(string(b), `"sno":"`+sno+`"`) {
			t.Fatalf("%s → %s: %s %v", regime, sno, b, err)
		}
	}
	in := example()
	in.VatPayer = true
	if _, err := Build(in); err == nil || err.Error() != "Чеки с НДС не поддерживаются" {
		t.Fatalf("НДС: %v", err)
	}
	in = example()
	in.TaxRegime = "npd"
	var de *DataError
	if _, err := Build(in); !errors.As(err, &de) {
		t.Fatalf("НПД: %v", err)
	}
}

// Σ позиций = total = Σ payments = сумма платежа; аванс — отдельной позицией со способом расчёта «аванс»;
// название обрезается до 128 символов.
func TestBuildItemsAndAdvance(t *testing.T) {
	in := example()
	long := strings.Repeat("я", 140)
	in.Items = []Item{{Name: long, SumKop: 2800001}, {Name: "Аванс по договору № 2026-011", SumKop: 199999, Advance: true}}
	in.TotalKop = 3000000
	in.PaymentType = PaymentPrepaid
	b, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	var req struct {
		Receipt struct {
			Items []struct {
				Name          string      `json:"name"`
				Sum           json.Number `json:"sum"`
				PaymentMethod string      `json:"payment_method"`
			} `json:"items"`
			Payments []struct {
				Type int         `json:"type"`
				Sum  json.Number `json:"sum"`
			} `json:"payments"`
			Total json.Number `json:"total"`
		} `json:"receipt"`
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	if err := dec.Decode(&req); err != nil {
		t.Fatal(err)
	}
	r := req.Receipt
	if len([]rune(r.Items[0].Name)) != 128 || r.Items[0].Sum != "28000.01" || r.Items[0].PaymentMethod != "full_payment" ||
		r.Items[1].Sum != "1999.99" || r.Items[1].PaymentMethod != "advance" || r.Total != "30000.00" ||
		r.Payments[0].Type != 2 || r.Payments[0].Sum != "30000.00" {
		t.Fatalf("позиции: %+v", r)
	}
	in.TotalKop = 3000001
	if _, err := Build(in); err == nil || err.Error() != "Сумма позиций чека не совпадает с суммой платежа" {
		t.Fatalf("суммы: %v", err)
	}
}

func TestItemName(t *testing.T) {
	oct := date.New(2026, 10, 1)
	cases := []struct {
		kind, title string
		month       *date.Date
		want        string
	}{
		{"rent", "Арендная плата, октябрь 2026", &oct, "Арендная плата за октябрь 2026, договор № 2026-011"},
		{"rent", "Плата за наём, 15–31 октября 2026", &oct, "Плата за наём за октябрь 2026, договор № 2026-011"},
		{"utilities", "Компенсация ЖКУ, октябрь 2026", &oct, "Компенсация ЖКУ за октябрь 2026, договор № 2026-011"},
		{"penalty", "Пени по начислению «Плата за наём, сентябрь 2026»", nil, "Пени по договору № 2026-011"},
		{"damage", "Ремонт двери", nil, "Возмещение ущерба по договору № 2026-011"},
		{"other", "Уборка", nil, "Уборка, договор № 2026-011"},
	}
	for _, c := range cases {
		if got := ItemName(c.kind, c.title, c.month, "2026-011"); got != c.want {
			t.Errorf("%s %q: %q, ожидалось %q", c.kind, c.title, got, c.want)
		}
	}
}

func TestPhone(t *testing.T) {
	for in, want := range map[string]string{"+7 (900) 123-45-67": "+79001234567", "89001234567": "+79001234567",
		"9001234567": "+79001234567", "12345": "", "": ""} {
		if got := Phone(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}
