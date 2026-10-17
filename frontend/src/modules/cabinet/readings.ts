// Показания счётчиков в кабинете (15 Экраны нанимателя, раздел 5; 05 разд. 6.2): ввод, расход на лету и
// предупреждение «расход заметно выше обычного». Значения — строки API «112.4», считаются в тысячных долях целыми
// числами (без parseFloat).

/** Ввод нанимателя → значение для API «112.4» или null, если это не число до 6 знаков до запятой и 3 после. */
export function parseReading(input: string): string | null {
  const s = input.trim().replace(/\s/g, '').replace(',', '.')
  if (!/^\d{1,6}(\.\d{0,3})?$/.test(s)) return null
  const [int, frac = ''] = s.split('.')
  const f = frac.replace(/0+$/, '')
  return `${String(Number(int))}${f ? '.' + f : ''}`
}

/** «112.4» → 112400 (тысячные). */
export function toMilli(value: string): number {
  const neg = value.startsWith('-')
  const [int, frac = ''] = value.replace('-', '').split('.')
  const n = Number(int) * 1000 + Number(frac.padEnd(3, '0').slice(0, 3))
  return neg ? -n : n
}

/** 112400 → «112,400». */
export function formatMilli(milli: number): string {
  const sign = milli < 0 ? '-' : ''
  const abs = Math.abs(milli)
  return `${sign}${Math.floor(abs / 1000)},${String(abs % 1000).padStart(3, '0')}`
}

/** «112.4» → «112,400». */
export function formatReading(value: string | null | undefined): string {
  return value ? formatMilli(toMilli(value)) : ''
}

/** Расход в тысячных: значение минус предыдущее. */
export function consumption(prev: string | null | undefined, value: string): number | null {
  return prev ? toMilli(value) - toMilli(prev) : null
}

/** Расход больше трёх средних за последние месяцы (как на сервере, calc.HighConsumption). */
export function isHigh(milli: number, previous: number[]): boolean {
  if (!previous.length) return false
  const sum = previous.reduce((a, v) => a + v, 0)
  return milli * previous.length > 3 * sum
}

/** Расходы последних месяцев (до 3) по принятым показаниям истории (новые первыми) и предыдущему значению. */
export function recentConsumptions(history: { value: string; status: string }[], limit = 3): number[] {
  const accepted = history.filter((h) => h.status === 'accepted').map((h) => toMilli(h.value))
  const out: number[] = []
  for (let i = 0; i + 1 < accepted.length && out.length < limit; i++) out.push(accepted[i]! - accepted[i + 1]!)
  return out
}
