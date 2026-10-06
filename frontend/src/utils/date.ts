// Даты: модель — строка YYYY-MM-DD; Date ↔ строка — в локальном времени, без сдвига часового пояса.
import dayjs from 'dayjs'
import 'dayjs/locale/ru'
import customParseFormat from 'dayjs/plugin/customParseFormat'
import isoWeek from 'dayjs/plugin/isoWeek'

dayjs.extend(customParseFormat)
dayjs.extend(isoWeek)
dayjs.locale('ru')

const ISO = 'YYYY-MM-DD'

/** Date → «2026-10-16» (по местному календарю, без UTC-сдвига). */
export function toIsoDate(d: Date): string {
  return dayjs(d).format(ISO)
}

/** «2026-10-16» → Date (полночь по местному времени). */
export function fromIsoDate(s: string): Date {
  const d = dayjs(s, ISO, true)
  if (!d.isValid()) throw new Error(`Неверная дата: ${s}`)
  return d.toDate()
}

/** «16.10.2026». */
export function formatDate(iso: string | null | undefined): string {
  return iso ? dayjs(iso, ISO).format('DD.MM.YYYY') : ''
}

/** «16 октября 2026» — в заголовках. */
export function formatDateLong(iso: string): string {
  return dayjs(iso, ISO).format('D MMMM YYYY')
}

/** «октябрь 2026». */
export function formatMonth(iso: string): string {
  return dayjs(iso, ISO).format('MMMM YYYY')
}

/** «15–31 марта 2026» в пределах месяца, иначе «01.10.2026 – 30.11.2026». */
export function formatPeriod(fromIso: string, toIso: string): string {
  const from = dayjs(fromIso, ISO)
  const to = dayjs(toIso, ISO)
  if (from.year() === to.year() && from.month() === to.month()) {
    return `${from.date()}–${to.format('D MMMM YYYY')}`
  }
  return `${from.format('DD.MM.YYYY')} – ${to.format('DD.MM.YYYY')}`
}

/** «19:30» по Москве из момента RFC 3339. */
export function formatTime(instant: string): string {
  return new Intl.DateTimeFormat('ru-RU', { timeZone: 'Europe/Moscow', hour: '2-digit', minute: '2-digit' }).format(
    new Date(instant),
  )
}

/** «16.10.2026 19:30» по Москве. */
export function formatDateTime(instant: string): string {
  return new Intl.DateTimeFormat('ru-RU', {
    timeZone: 'Europe/Moscow',
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
    .format(new Date(instant))
    .replace(',', '')
}
