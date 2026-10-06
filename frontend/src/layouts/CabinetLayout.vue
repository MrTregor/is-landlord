<script setup lang="ts">
// Кабинет нанимателя (13 Фронтенд, раздел 5): сначала телефон 360–480 px; на ширине < 768 px —
// нижнее меню из 5 пунктов, на ≥ 768 px — те же пункты горизонтально под шапкой; контент до 720 px.
import { useRoute, useRouter } from 'vue-router'
import { APP_NAME } from '@/app/constants'
import { useSessionStore } from '@/app/session'
import { cabinetMenu } from './ownerMenu'

const route = useRoute()
const router = useRouter()
const session = useSessionStore()

function isActive(to: string): boolean {
  return to === '/cabinet' ? route.path === to : route.path.startsWith(to)
}

async function logout() {
  await session.logout().catch(() => undefined)
  await router.push('/login')
}
</script>

<template>
  <div class="cabinet">
    <header class="cabinet__header">
      <div class="cabinet__title">
        <i class="pi pi-building" aria-hidden="true" />
        <span>{{ APP_NAME }}</span>
      </div>
      <Button text icon="pi pi-sign-out" aria-label="Выйти" class="cabinet__touch" @click="logout" />
    </header>
    <nav class="cabinet__nav" aria-label="Разделы кабинета">
      <RouterLink
        v-for="item in cabinetMenu"
        :key="item.to"
        :to="item.to"
        class="cabinet__link"
        :class="{ 'cabinet__link--active': isActive(item.to) }"
      >
        <i :class="item.icon" aria-hidden="true" />
        <span>{{ item.label }}</span>
      </RouterLink>
    </nav>
    <main class="cabinet__content">
      <slot />
    </main>
  </div>
</template>

<style scoped>
.cabinet {
  min-height: 100vh;
  background: var(--app-surface-ground);
}
.cabinet__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0.5rem 1rem;
  background: var(--p-content-background);
  border-bottom: 1px solid var(--p-content-border-color);
}
.cabinet__title {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--p-primary-color);
  font-weight: 600;
}
.cabinet__touch {
  min-width: 44px;
  min-height: 44px;
}
.cabinet__nav {
  display: flex;
  justify-content: center;
  gap: 0.25rem;
  background: var(--p-content-background);
  border-bottom: 1px solid var(--p-content-border-color);
}
.cabinet__link {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.125rem;
  min-width: 44px;
  min-height: 44px;
  padding: 0.375rem 0.75rem;
  color: var(--p-text-muted-color);
  font-size: 0.875rem;
  text-decoration: none;
}
.cabinet__link--active {
  color: var(--p-primary-color);
}
.cabinet__content {
  max-width: 720px;
  margin: 0 auto;
  padding: 1rem 1rem 5rem;
}
@media (max-width: 767.98px) {
  .cabinet__nav {
    position: fixed;
    bottom: 0;
    left: 0;
    right: 0;
    z-index: 10;
    justify-content: space-around;
    border-top: 1px solid var(--p-content-border-color);
    border-bottom: none;
  }
  .cabinet__link {
    flex: 1;
    padding: 0.375rem 0;
    font-size: 0.75rem;
  }
}
@media (min-width: 768px) {
  .cabinet__link {
    flex-direction: row;
    gap: 0.5rem;
  }
}
</style>
