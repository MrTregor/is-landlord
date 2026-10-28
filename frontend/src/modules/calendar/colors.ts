import type { components } from '@/api/schema'

type Kind = components['schemas']['CalendarEventKind']

// Цвета видов событий (14 разд. 11): сроки — синий, просрочки — красный, договоры и уведомления — фиолетовый,
// регистрация и индексация — оранжевый, показы — зелёный, ремонты и поверка — серо-коричневый, прочее — серый.
// Передаются FullCalendar свойством события `color`: элементы событий рисует сам календарь, scoped-стили Vue
// до них не доходят (иначе фон прозрачный, а текст белый — события не видны).
export const calendarKindColors: Record<Kind, string> = {
  payment_due: '#2563eb',
  overdue: '#dc2626',
  contract_end: '#7c3aed',
  contract_end_notice: '#7c3aed',
  registration: '#ea580c',
  indexation: '#ea580c',
  showing: '#16a34a',
  repair: '#8b7355',
  meter_verification: '#8b7355',
  custom: '#6b7280',
}
