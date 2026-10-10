// Расписание и итоги плановых задач для экрана «Служебное».

/** «5 0 * * *» → «ежедневно в 00:05»; «каждые N минут»; «15 * * * *» → «каждый час в :15». */
export function cronText(cron: string): string {
  const [min = '', hour = ''] = cron.split(' ')
  if (min.startsWith('*/')) return `каждые ${min.slice(2)} минут`
  if (hour === '*') return `каждый час в :${min.padStart(2, '0')}`
  return `ежедневно в ${hour.padStart(2, '0')}:${min.padStart(2, '0')}`
}

const statTitles: Record<string, string> = {
  contracts: 'договоров',
  created: 'создано',
  charges: 'начислений проверено',
  finished: 'завершено',
  released: 'освобождено объектов',
  rented: 'сдано объектов',
}

/** Итоги запуска: «договоров 12, создано 8, ошибок 1». */
export function statsText(stats: Record<string, unknown>): string {
  const parts = Object.entries(stats)
    .filter(([k, v]) => k !== 'errors' && typeof v === 'number')
    .map(([k, v]) => `${statTitles[k] ?? k} ${v}`)
  const errors = Array.isArray(stats.errors) ? stats.errors.length : 0
  if (errors) parts.push(`ошибок ${errors}`)
  return parts.join(', ')
}
