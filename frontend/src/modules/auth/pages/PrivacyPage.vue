<script setup lang="ts">
// Политика обработки ПДн — открыта без входа (12 Безопасность и ПДн, раздел 5).
import { useQuery } from '@tanstack/vue-query'
import { api, unwrap } from '@/api/client'
import { APP_NAME } from '@/app/constants'

const policy = useQuery({
  queryKey: ['public', 'privacy-policy'],
  queryFn: () => unwrap(api.GET('/public/privacy-policy')),
})
</script>

<template>
  <main class="privacy">
    <RouterLink to="/login" class="privacy__brand"
      ><i class="pi pi-building" aria-hidden="true" /> {{ APP_NAME }}</RouterLink
    >
    <Skeleton v-if="policy.isPending.value" height="20rem" />
    <Message v-else-if="policy.isError.value" severity="error" :closable="false">
      Не удалось загрузить политику. Обновите страницу.
    </Message>
    <article v-else>
      <!-- eslint-disable-next-line vue/no-v-html -- текст политики очищается сервером (bluemonday) -->
      <div v-html="policy.data.value?.html" />
      <p class="privacy__version">Версия {{ policy.data.value?.version }}</p>
    </article>
  </main>
</template>

<style scoped>
.privacy {
  max-width: 800px;
  margin: 0 auto;
  padding: 1.5rem 1rem 3rem;
  background: var(--p-content-background);
  min-height: 100vh;
  line-height: 1.5;
}
.privacy__brand {
  display: inline-flex;
  gap: 0.5rem;
  margin-bottom: 1rem;
  font-weight: 600;
  text-decoration: none;
}
.privacy__version {
  color: var(--p-text-muted-color);
}
</style>
