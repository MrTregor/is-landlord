<script setup lang="ts">
// Ввод даты; модель — строка YYYY-MM-DD (без сдвига часового пояса).
// «Сегодня» — дата системы (utils/today): PrimeVue берёт дату компьютера, поэтому в режиме demo со сдвигом даты
// подсветка дня и месяц, на котором открывается пустое поле, подставляются здесь.
import { computed, useTemplateRef } from 'vue'
import { fromIsoDate, toIsoDate } from '@/utils/date'
import { isShifted, today, todayIso } from '@/utils/today'

const model = defineModel<string | null>({ required: true })
defineProps<{ inputId?: string; invalid?: boolean; maxDate?: Date; minDate?: Date; placeholder?: string }>()
const emit = defineEmits<{ blur: [] }>()

const value = computed({
  get: () => (model.value ? fromIsoDate(model.value) : null),
  set: (d: Date | Date[] | (Date | null)[] | null | undefined) => {
    model.value = d instanceof Date ? toIsoDate(d) : null
  },
})

const picker = useTemplateRef<{ currentMonth: number; currentYear: number }>('picker')
function onShow() {
  if (model.value || !isShifted() || !picker.value) return
  const t = today()
  picker.value.currentMonth = t.getMonth()
  picker.value.currentYear = t.getFullYear()
}

const pad = (n: number) => String(n).padStart(2, '0')
/** День календаря PrimeVue (месяц с нуля) — сегодня по дате системы. */
function isToday(d: { day: number; month: number; year: number }): boolean {
  return `${d.year}-${pad(d.month + 1)}-${pad(d.day)}` === todayIso()
}
</script>

<template>
  <DatePicker
    ref="picker"
    v-model="value"
    :input-id="inputId"
    date-format="dd.mm.yy"
    show-icon
    icon-display="input"
    :invalid="invalid"
    :max-date="maxDate"
    :min-date="minDate"
    :placeholder="placeholder ?? 'дд.мм.гггг'"
    :panel-class="isShifted() ? 'date-input--shifted' : undefined"
    fluid
    @show="onShow"
    @hide="emit('blur')"
    @blur="emit('blur')"
  >
    <template #date="{ date }">
      <span :class="{ 'date-input__today': isShifted() && isToday(date) }">{{ date.day }}</span>
    </template>
  </DatePicker>
</template>
