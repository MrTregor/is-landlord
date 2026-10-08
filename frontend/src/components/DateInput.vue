<script setup lang="ts">
// Ввод даты; модель — строка YYYY-MM-DD (без сдвига часового пояса).
import { computed } from 'vue'
import { fromIsoDate, toIsoDate } from '@/utils/date'

const model = defineModel<string | null>({ required: true })
defineProps<{ inputId?: string; invalid?: boolean; maxDate?: Date; minDate?: Date; placeholder?: string }>()
const emit = defineEmits<{ blur: [] }>()

const value = computed({
  get: () => (model.value ? fromIsoDate(model.value) : null),
  set: (d: Date | Date[] | (Date | null)[] | null | undefined) => {
    model.value = d instanceof Date ? toIsoDate(d) : null
  },
})
</script>

<template>
  <DatePicker
    v-model="value"
    :input-id="inputId"
    date-format="dd.mm.yy"
    show-icon
    icon-display="input"
    :invalid="invalid"
    :max-date="maxDate"
    :min-date="minDate"
    :placeholder="placeholder ?? 'дд.мм.гггг'"
    fluid
    @hide="emit('blur')"
    @blur="emit('blur')"
  />
</template>
