<script setup lang="ts">
// Пустой список: пояснение и основная кнопка (13 Фронтенд, раздел 3).
defineProps<{ text: string; actionLabel?: string; icon?: string }>()
const emit = defineEmits<{ action: [] }>()
</script>

<template>
  <div class="empty">
    <i :class="icon ?? 'pi pi-inbox'" class="empty__icon" aria-hidden="true" />
    <p>{{ text }}</p>
    <Button v-if="actionLabel" :label="actionLabel" icon="pi pi-plus" @click="emit('action')" />
  </div>
</template>

<style scoped>
.empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.5rem;
  padding: 2.5rem 1rem;
  color: var(--p-text-muted-color);
  text-align: center;
}
.empty__icon {
  font-size: 2rem;
}
</style>
