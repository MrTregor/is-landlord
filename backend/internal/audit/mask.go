package audit

import (
	"encoding/json"
	"strings"
)

// Masked — значение, которым заменяются персональные данные в журнале.
const Masked = "***"

// Поля с персональными данными (12 Безопасность и ПДн, раздел 5): ФИО, дата рождения, документы, адрес,
// телефон, e-mail, ФИО проживающих — в любом написании (snake_case и camelCase).
var piiKeys = map[string]bool{
	"lastname": true, "firstname": true, "middlename": true, "birthdate": true,
	"registrationaddress": true, "phone": true, "email": true, "toemail": true,
	"fullname": true, "displayname": true, "signatoryname": true, "password": true, "passwordhash": true,
}

func isPII(key string) bool {
	k := strings.ToLower(strings.ReplaceAll(key, "_", ""))
	return piiKeys[k] || strings.HasPrefix(k, "doc") && k != "documentid" && k != "documentcity"
}

// Mask превращает значение в JSON-объект и заменяет поля с ПДн на «***» (null остаётся null).
// Возвращает nil для nil.
func Mask(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	return json.Marshal(maskValue(generic))
}

func maskValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if isPII(k) && val != nil {
				t[k] = Masked
				continue
			}
			t[k] = maskValue(val)
		}
		return t
	case []any:
		for i := range t {
			t[i] = maskValue(t[i])
		}
		return t
	default:
		return v
	}
}
