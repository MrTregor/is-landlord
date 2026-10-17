// Быстрые периоды отчётов (14 Экраны собственника, раздел 12): «Этот месяц», «Прошлый месяц», «Этот квартал»,
// «Этот год», «Прошлый год». Даты — строки YYYY-MM-DD.
import dayjs from 'dayjs'

export interface Period {
  from: string
  to: string
}

export type PresetKey = 'month' | 'prevMonth' | 'quarter' | 'year' | 'prevYear'

export const presets: { key: PresetKey; label: string }[] = [
  { key: 'month', label: 'Этот месяц' },
  { key: 'prevMonth', label: 'Прошлый месяц' },
  { key: 'quarter', label: 'Этот квартал' },
  { key: 'year', label: 'Этот год' },
  { key: 'prevYear', label: 'Прошлый год' },
]

const fmt = (d: dayjs.Dayjs) => d.format('YYYY-MM-DD')

/** Период по быстрой кнопке относительно даты today (YYYY-MM-DD). */
export function presetPeriod(key: PresetKey, today: string): Period {
  const t = dayjs(today)
  switch (key) {
    case 'month':
      return { from: fmt(t.startOf('month')), to: fmt(t.endOf('month')) }
    case 'prevMonth': {
      const p = t.subtract(1, 'month')
      return { from: fmt(p.startOf('month')), to: fmt(p.endOf('month')) }
    }
    case 'quarter': {
      const start = t.month(Math.floor(t.month() / 3) * 3).startOf('month')
      return { from: fmt(start), to: fmt(start.add(2, 'month').endOf('month')) }
    }
    case 'year':
      return { from: fmt(t.startOf('year')), to: fmt(t.endOf('year')) }
    case 'prevYear': {
      const p = t.subtract(1, 'year')
      return { from: fmt(p.startOf('year')), to: fmt(p.endOf('year')) }
    }
  }
}
