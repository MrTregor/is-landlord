// Телефоны: хранение +7XXXXXXXXXX, вывод «+7 (916) 123-45-67».

/** Нормализует телефон в любом виде в +7XXXXXXXXXX; null — не российский номер из 10 цифр. */
export function normalizePhone(input: string): string | null {
  let digits = input.replace(/\D/g, '')
  if (digits.length === 11 && (digits.startsWith('7') || digits.startsWith('8'))) digits = digits.slice(1)
  return digits.length === 10 ? `+7${digits}` : null
}

/** «+7 (916) 123-45-67». */
export function formatPhone(phone: string | null | undefined): string {
  if (!phone) return ''
  const n = normalizePhone(phone)
  if (!n) return phone
  return `+7 (${n.slice(2, 5)}) ${n.slice(5, 8)}-${n.slice(8, 10)}-${n.slice(10, 12)}`
}
