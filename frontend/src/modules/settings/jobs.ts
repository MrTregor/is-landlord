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
  anonymized: 'обезличено нанимателей',
  sessions: 'сессий удалено',
  tokens: 'токенов удалено',
  loginAttempts: 'попыток входа удалено',
  files: 'файлов удалено',
  underpaymentsClosed: 'недоплат закрыто',
  reminders: 'напоминаний об оплате',
  overdue: 'писем о просрочке',
  letters: 'писем',
  sent: 'отправлено',
  already_sent: 'уже отправлено ранее',
  disabled: 'письмо выключено',
  empty: 'нет новостей',
  payments: 'платежей проверено',
  polls: 'запросов в кассу',
  sends: 'отправок в кассу',
  waiting: 'чеков в обработке',
  queued: 'чеков в очереди',
  stuckLetters: 'писем о зависших чеках',
}

/** Итоги запуска: «договоров 12, создано 8, ошибок 1». Ключи — счётчики jobs.Stats задач backend. */
export function statsText(stats: Record<string, unknown>): string {
  const parts = Object.entries(stats)
    .filter(([k, v]) => k !== 'errors' && typeof v === 'number')
    .map(([k, v]) => `${statTitles[k] ?? k} ${v}`)
  const errors = Array.isArray(stats.errors) ? stats.errors.length : 0
  if (errors) parts.push(`ошибок ${errors}`)
  return parts.join(', ')
}

/** Размер файла: «512 Б», «12,4 КБ», «1,5 МБ». */
export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} Б`
  const units = ['КБ', 'МБ', 'ГБ']
  let v = bytes / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${(Math.round(v * 10) / 10).toString().replace('.', ',')} ${units[i]}`
}
