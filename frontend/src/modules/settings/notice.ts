// Дни уведомления об окончании договора (Настройки → Уведомления и сроки): «90, 30» → [90, 30].

/** Список дней или текст ошибки: целые числа 1–365 через запятую, без повторов, по убыванию. */
export function parseNoticeDays(text: string): number[] | string {
  const parts = text
    .split(/[,;\s]+/)
    .map((s) => s.trim())
    .filter(Boolean)
  if (!parts.length) return 'Укажите хотя бы одно число дней'
  const out: number[] = []
  for (const p of parts) {
    if (!/^\d{1,3}$/.test(p)) return `«${p}» — не целое число дней`
    const n = Number(p)
    if (n < 1 || n > 365) return 'Число дней — от 1 до 365'
    if (!out.includes(n)) out.push(n)
  }
  return out.sort((a, b) => b - a)
}
