<script setup lang="ts">
// Раскладка входа: карточка 400 px по центру, подвал — организация и ссылка на политику (13 Фронтенд, раздел 5).
import { APP_NAME } from '@/app/constants'
import { useAppInfo } from '@/api/queries/public'

const appInfo = useAppInfo()
</script>

<template>
  <div class="auth-layout">
    <main class="auth-layout__main">
      <Card class="auth-layout__card">
        <template #title>
          <div class="auth-layout__brand">
            <i class="pi pi-building" aria-hidden="true" />
            <span>{{ appInfo.data.value?.appName ?? APP_NAME }}</span>
          </div>
        </template>
        <template #content>
          <slot />
        </template>
      </Card>
    </main>
    <footer class="auth-layout__footer">
      <span v-if="appInfo.data.value?.orgShortName">{{ appInfo.data.value.orgShortName }}</span>
      <RouterLink to="/privacy">Политика обработки персональных данных</RouterLink>
    </footer>
  </div>
</template>

<style scoped>
.auth-layout {
  display: flex;
  flex-direction: column;
  min-height: 100vh;
  background: var(--app-surface-ground);
}
.auth-layout__main {
  display: flex;
  flex: 1;
  align-items: center;
  justify-content: center;
  padding: 1rem;
}
.auth-layout__card {
  width: 100%;
  max-width: 400px;
}
.auth-layout__brand {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--p-primary-color);
  font-size: 1.5rem;
}
.auth-layout__footer {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 0.25rem 1rem;
  padding: 1rem;
  color: var(--p-text-muted-color);
  font-size: 0.875rem;
}
</style>
