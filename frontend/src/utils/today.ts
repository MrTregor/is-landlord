// «Сегодня» на клиенте — дата системы: в режиме demo она сдвинута (/dev/clock, ставит OwnerLayout), иначе — дата
// компьютера. Значения реактивные: ограничения календарей в шаблонах обновляются после сдвига даты.
import { ref } from 'vue'
import { fromIsoDate, toIsoDate } from './date'

const systemToday = ref<string | null>(null)

/** Дата системы в режиме demo; null — дата компьютера. */
export function setSystemToday(iso: string | null) {
  systemToday.value = iso
}

/** Сегодня, «2026-10-16». */
export function todayIso(): string {
  return systemToday.value ?? toIsoDate(new Date())
}

/** Сегодня, полночь по местному времени — для max-date календаря. */
export function today(): Date {
  return fromIsoDate(todayIso())
}

/** Дата системы отличается от даты компьютера — календарю нужна своя подсветка «сегодня». */
export function isShifted(): boolean {
  return systemToday.value !== null && systemToday.value !== toIsoDate(new Date())
}
