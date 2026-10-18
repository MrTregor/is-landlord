package file

import "testing"

// Имя загруженного файла: расширение по типу содержимого дописывается, только если его нет (FR-07.2 — выписка CSV).
func TestCleanName_FR07_2(t *testing.T) {
	for _, c := range []struct{ name, ext, want string }{
		{"vypiska.csv", "txt", "vypiska.csv"},
		{"vypiska.txt", "txt", "vypiska.txt"},
		{"kl_to_1c", "txt", "kl_to_1c.txt"},
		{"фото.jpeg", "jpg", "фото.jpeg"},
		{`C:\scan\akt.pdf`, "pdf", "akt.pdf"},
		{"akt", "pdf", "akt.pdf"},
	} {
		if got := cleanName(c.name, c.ext); got != c.want {
			t.Errorf("cleanName(%q, %q) = %q, want %q", c.name, c.ext, got, c.want)
		}
	}
}
