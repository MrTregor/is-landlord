// Состав повторной оплаты (15 разд. 3; docs/open-questions.md, № 54): прежний состав — подсказка, оплачиваются
// только начисления, открытые сейчас; если открытых не осталось — сообщение вместо оплаты.
import type { components } from '@/api/schema'

type S = components['schemas']

export type RetryPlan = { chargeIds: string[] } | { message: string }

export function retryPlan(
  prev: Pick<S['CabinetPaymentStatus'], 'purpose' | 'chargeIds'>,
  now: S['CabinetSummary'],
): RetryPlan {
  if (prev.purpose === 'deposit') {
    return now.depositToPayKop > 0 ? { chargeIds: [] } : { message: 'Обеспечительный платёж уже внесён' }
  }
  const open = new Set(now.lines.map((l) => l.chargeId))
  const chargeIds = prev.chargeIds.filter((id) => open.has(id))
  return chargeIds.length ? { chargeIds } : { message: 'Начисления уже оплачены' }
}
