<script setup lang="ts">
// Маскированное значение ПДн и кнопка «Показать»: раскрытие пишется в журнал (tenant.pdn_view).
defineProps<{ masked: string | null; revealed?: string | null; loading?: boolean }>()
const emit = defineEmits<{ reveal: [] }>()
</script>

<template>
  <span class="pdn">
    <span v-if="revealed" class="pdn__value">{{ revealed }}</span>
    <span v-else-if="masked" class="pdn__value tabular-nums">{{ masked }}</span>
    <span v-else class="hint">не указано</span>
    <Button
      v-if="masked && !revealed"
      text
      size="small"
      icon="pi pi-eye"
      label="Показать"
      :loading="loading"
      @click="emit('reveal')"
    />
  </span>
</template>

<style scoped>
.pdn {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.25rem 0.5rem;
}
</style>
