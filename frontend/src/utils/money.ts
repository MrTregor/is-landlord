// Деньги — целые копейки (number в безопасном диапазоне). Дробные вычисления и parseFloat запрещены:
// строки разбираются посимвольно (13 Фронтенд, раздел 3).

const NBSP = ' '

/** Наибольшая сумма, которую можно хранить в number без потери точности, коп. */
export const MAX_KOP = Number.MAX_SAFE_INTEGER

function groupThousands(digits: string): string {
  return digits.replace(/\B(?=(\d{3})+(?!\d))/g, NBSP)
}

/** «28 000,00» (неразрывные пробелы между разрядами), отрицательные — со знаком «−». */
export function formatKopNumber(kop: number): string {
  if (!Number.isSafeInteger(kop)) throw new Error('Сумма должна быть целым числом копеек')
  const sign = kop < 0 ? '−' : ''
  const abs = Math.abs(kop)
  const rub = Math.trunc(abs / 100)
  const cents = abs % 100
  return `${sign}${groupThousands(String(rub))},${String(cents).padStart(2, '0')}`
}

/** «28 000,00 ₽» — формат вывода денег (13 Фронтенд, раздел 8). */
export function formatKop(kop: number): string {
  return `${formatKopNumber(kop)}${NBSP}₽`
}

/** Строка для поля ввода: «28000,50». */
export function kopToRubString(kop: number): string {
  if (!Number.isSafeInteger(kop)) throw new Error('Сумма должна быть целым числом копеек')
  const sign = kop < 0 ? '-' : ''
  const abs = Math.abs(kop)
  return `${sign}${Math.trunc(abs / 100)},${String(abs % 100).padStart(2, '0')}`
}

/**
 * Разбор суммы в рублях в копейки без дробной арифметики.
 * Разряды могут разделяться пробелами; дробная часть — через запятую или точку, не более 2 знаков.
 * «28 000,50» → 2800050, «0,1» → 10, «1.234,5» → ошибка.
 */
export function parseRubToKop(input: string): number {
  const s = input.replace(/[\s  ]/g, '')
  const m = /^(-?)(\d+)(?:[.,](\d{1,2}))?$/.exec(s)
  if (!m) throw new Error('Введите сумму в рублях, например 28 000,50')
  const [, sign, rubPart = '', centsPart = ''] = m
  const rub = BigInt(rubPart)
  const cents = BigInt(centsPart.padEnd(2, '0') || '0')
  const total = rub * 100n + cents
  if (total > BigInt(MAX_KOP)) throw new Error('Слишком большая сумма')
  const kop = Number(total)
  return sign === '-' ? -kop : kop
}
