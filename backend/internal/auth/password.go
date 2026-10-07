package auth

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"landlord/backend/internal/platform/apperr"
)

// PasswordRuleMessage — требования к паролю (12 Безопасность и ПДн, раздел 1).
const PasswordRuleMessage = "Пароль должен быть не короче 10 символов и содержать буквы и цифры"

// ValidatePassword: не короче 10 символов, хотя бы одна буква и одна цифра, не совпадает с e-mail.
func ValidatePassword(field, password, email string) error {
	var letter, digit bool
	for _, r := range password {
		switch {
		case unicode.IsLetter(r):
			letter = true
		case unicode.IsDigit(r):
			digit = true
		}
	}
	if utf8.RuneCountInString(password) < 10 || !letter || !digit {
		return apperr.Validation(apperr.FieldError{Field: field, Message: PasswordRuleMessage})
	}
	if strings.EqualFold(strings.TrimSpace(password), strings.TrimSpace(email)) {
		return apperr.Validation(apperr.FieldError{Field: field, Message: "Пароль не должен совпадать с e-mail"})
	}
	return nil
}

// NormalizeEmail — e-mail без пробелов в нижнем регистре.
func NormalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }
