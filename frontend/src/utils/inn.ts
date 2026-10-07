// Проверка ИНН по контрольным числам (алгоритм ФНС) для 10 и 12 знаков.

const W10 = [2, 4, 10, 3, 5, 9, 4, 6, 8]
const W11 = [7, 2, 4, 10, 3, 5, 9, 4, 6, 8]
const W12 = [3, 7, 2, 4, 10, 3, 5, 9, 4, 6, 8]

function check(digits: number[], weights: number[]): number {
  const sum = weights.reduce((acc, w, i) => acc + w * (digits[i] ?? 0), 0)
  return (sum % 11) % 10
}

/** ИНН верен: 10 цифр (организация) или 12 цифр (физлицо, ИП) с верными контрольными числами. */
export function isValidInn(inn: string): boolean {
  if (!/^\d{10}(\d{2})?$/.test(inn)) return false
  const d = [...inn].map(Number)
  if (d.length === 10) return check(d, W10) === d[9]
  return check(d, W11) === d[10] && check(d, W12) === d[11]
}
