// Предпросмотр распределения платежа на клиенте (14 разд. 6.2) по правилам 05 разд. 10.3; окончательный
// результат считает сервер. Суммы — целые копейки.
import type { Charge } from '@/api/queries/charges'

export interface PreviewLine {
  charge: Charge
  amountKop: number
}

const KIND_RANK: Record<string, number> = { rent: 1, utilities: 2, damage: 3, other: 4, penalty: 5 }

/** Можно погашать: есть остаток, есть срок (начисляемые пени без срока не погашаются). */
function payable(c: Charge): boolean {
  return (
    c.outstandingKop > 0 &&
    !!c.dueDate &&
    c.status !== 'cancelled' &&
    !(c.kind === 'penalty' && c.penaltyState === 'accruing')
  )
}

function compare(a: Charge, b: Charge): number {
  if (a.dueDate !== b.dueDate) return (a.dueDate ?? '') < (b.dueDate ?? '') ? -1 : 1
  const ra = KIND_RANK[a.kind] ?? 9
  const rb = KIND_RANK[b.kind] ?? 9
  if (ra !== rb) return ra - rb
  return a.createdAt < b.createdAt ? -1 : a.createdAt > b.createdAt ? 1 : 0
}

/** Порядок погашения: выбранные, затем основной долг, затем пени final и manual. */
export function allocationOrder(charges: Charge[], requested: string[] = []): Charge[] {
  const req = new Set(requested)
  const open = charges.filter(payable)
  const first = open.filter((c) => req.has(c.id)).sort(compare)
  const main = open.filter((c) => !req.has(c.id) && c.kind !== 'penalty').sort(compare)
  const penalties = open.filter((c) => !req.has(c.id) && c.kind === 'penalty').sort(compare)
  return [...first, ...main, ...penalties]
}

/** Распределение суммы: строки и остаток (аванс). */
export function previewAllocation(
  amountKop: number,
  charges: Charge[],
  requested: string[] = [],
): { lines: PreviewLine[]; advanceKop: number } {
  let avail = amountKop
  const lines: PreviewLine[] = []
  for (const c of allocationOrder(charges, requested)) {
    if (avail <= 0) break
    const amt = Math.min(avail, c.outstandingKop)
    lines.push({ charge: c, amountKop: amt })
    avail -= amt
  }
  return { lines, advanceKop: Math.max(avail, 0) }
}
