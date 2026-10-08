<script setup lang="ts">
// Ввод суммы в рублях; v-model — копейки (number). Строка разбирается без parseFloat (utils/money).
import { ref, watch } from 'vue'
import { formatKopNumber, parseRubToKop } from '@/utils/money'

const model = defineModel<number | null>({ required: true })
const props = defineProps<{ inputId?: string; invalid?: boolean; allowNegative?: boolean }>()
const emit = defineEmits<{ error: [message: string | null]; blur: [] }>()

const text = ref('')
const focused = ref(false)

function show(v: number | null) {
  text.value = v == null ? '' : formatKopNumber(v)
}
watch(
  model,
  (v) => {
    if (!focused.value) show(v)
  },
  { immediate: true },
)

function commit() {
  focused.value = false
  const s = text.value.trim()
  if (s === '') {
    model.value = null
    emit('error', null)
    emit('blur')
    return
  }
  try {
    const kop = parseRubToKop(s.replace('−', '-'))
    if (kop < 0 && !props.allowNegative) throw new Error('Сумма не может быть отрицательной')
    model.value = kop
    show(kop)
    emit('error', null)
  } catch (e) {
    emit('error', e instanceof Error ? e.message : 'Неверная сумма')
  }
  emit('blur')
}
</script>

<template>
  <InputGroup>
    <InputText
      :id="inputId"
      v-model="text"
      inputmode="decimal"
      :invalid="invalid"
      class="money-input"
      @focus="focused = true"
      @blur="commit"
      @keydown.enter="commit"
    />
    <InputGroupAddon>₽</InputGroupAddon>
  </InputGroup>
</template>

<style scoped>
.money-input {
  text-align: right;
  font-variant-numeric: tabular-nums;
}
</style>
