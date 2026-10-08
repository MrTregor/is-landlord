<script setup lang="ts">
// Сумма «28 000,00 ₽»; отрицательная — красным со знаком минус.
import { computed } from 'vue'
import { formatKop } from '@/utils/money'

const props = defineProps<{ kop: number | null | undefined; negative?: boolean }>()
const text = computed(() => (props.kop == null ? '—' : formatKop(props.kop)))
</script>

<template>
  <span class="tabular-nums" :class="{ 'money--negative': negative || (kop ?? 0) < 0 }">{{ text }}</span>
</template>

<style scoped>
.money--negative {
  color: var(--p-red-600);
}
</style>
