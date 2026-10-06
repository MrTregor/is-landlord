<script setup lang="ts">
import { computed } from 'vue'
import { homeFor } from '@/app/guards'
import { useSessionStore } from '@/app/session'

const session = useSessionStore()
const home = computed(() => homeFor(session.role))
</script>

<template>
  <main class="not-found">
    <i class="pi pi-compass not-found__icon" aria-hidden="true" />
    <h1>Страница не найдена</h1>
    <p>Адрес набран с ошибкой или страница была удалена.</p>
    <RouterLink :to="home">
      <Button label="На главную" icon="pi pi-home" />
    </RouterLink>
  </main>
</template>

<style scoped>
.not-found {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  padding: 1rem;
  text-align: center;
}
.not-found__icon {
  font-size: 3rem;
  color: var(--p-primary-color);
}
</style>
