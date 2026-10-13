package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// sign — подпись Token (08 разд. 1.1): поля корневого уровня, кроме Token и вложенных объектов и массивов,
// плюс Password; пары по имени ключа (побайтово); значения склеиваются без разделителей; SHA-256, hex.
// Код подписи эмулятора свой, независимый от backend.
func sign(fields map[string]any, password string) string {
	values := map[string]string{"Password": password}
	for k, v := range fields {
		if k == "Token" {
			continue
		}
		if s, ok := scalar(v); ok {
			values[k] = s
		}
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(values[k])
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// scalar — строковое значение поля для подписи; объекты, массивы и null не участвуют.
func scalar(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case json.Number:
		return x.String(), true
	case bool:
		if x {
			return "true", true
		}
		return "false", true
	case int:
		return strconv.FormatInt(int64(x), 10), true
	case int64:
		return strconv.FormatInt(x, 10), true
	default:
		return "", false
	}
}
