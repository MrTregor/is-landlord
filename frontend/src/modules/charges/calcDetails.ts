// Подробности расчёта начисления в человекочитаемом виде (14 Экраны собственника, раздел 5).
import { formatDate } from '@/utils/date'
import { meterTypeShort } from '@/utils/labels'
import { formatKop, formatKopNumber } from '@/utils/money'
import type { components } from '@/api/schema'

type MeterType = components['schemas']['MeterType']

const num = (v: unknown) => (typeof v === 'number' ? v : Number(v))
const dec = (v: unknown) =>
  String(v ?? '')
    .replace(/(\.\d*?)0+$/, '$1')
    .replace(/\.$/, '')
    .replace('.', ',')

/** Плата: «30 000,00 × 17 / 31 дн.» или «полный месяц, 30 000,00». */
export function rentFormula(d: Record<string, unknown>): string | null {
  if (typeof d.rent !== 'number' || typeof d.days !== 'number' || typeof d.daysInMonth !== 'number') return null
  if (d.days === d.daysInMonth) return `полный месяц: ${formatKopNumber(d.rent)}`
  return `${formatKopNumber(d.rent)} × ${d.days} / ${d.daysInMonth} дн.`
}

export interface UtilityLine {
  type: string
  prev: string
  prevDate: string
  value: string
  consumption: string
  tariff: string
  amount: string
  missing: boolean
}

/** Строки счётчиков начисления ЖКУ. */
export function utilityLines(d: Record<string, unknown>): UtilityLine[] {
  const lines = Array.isArray(d.lines) ? (d.lines as Record<string, unknown>[]) : []
  return lines.map((l) => ({
    type: meterTypeShort[l.type as MeterType] ?? String(l.type),
    prev: dec(l.prev),
    prevDate: formatDate(String(l.prevDate ?? '')),
    value: dec(l.value),
    consumption: dec(l.consumption),
    tariff: dec(l.tariff),
    amount: formatKop(num(l.amount)),
    missing: l.missing === true,
  }))
}

/** Отрезки пеней: «09.10–16.10: 8 дн. × 11 500,00 × 0,1 % = 92,00». */
export function penaltySegments(d: Record<string, unknown>): string[] {
  const segs = Array.isArray(d.segments) ? (d.segments as Record<string, unknown>[]) : []
  const rate = dec(d.ratePct)
  return segs.map((s) => {
    const from = formatDate(String(s.from)).slice(0, 5)
    const to = formatDate(String(s.to)).slice(0, 5)
    return `${from}–${to}: ${s.days} дн. × ${formatKopNumber(num(s.base))} × ${rate} % = ${formatKopNumber(num(s.amount))}`
  })
}
