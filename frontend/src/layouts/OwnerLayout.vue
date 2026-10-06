<script setup lang="ts">
// Рабочее место собственника (13 Фронтенд, раздел 5): меню 240 px (свёртывается до 64 px),
// верхняя панель 56 px — хлебные крошки, поиск, меню пользователя; ниже 1024 px меню — выдвижная панель.
import { computed, onBeforeUnmount, onMounted, ref, useTemplateRef } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useQuery } from '@tanstack/vue-query'
import { api, unwrap } from '@/api/client'
import { useAppInfo } from '@/api/queries/public'
import { useDashboard } from '@/api/queries/dashboard'
import { APP_NAME } from '@/app/constants'
import { useSessionStore } from '@/app/session'
import { formatDate } from '@/utils/date'
import { ownerMenu } from './ownerMenu'

const COLLAPSED_KEY = 'landlord.menuCollapsed'

const route = useRoute()
const router = useRouter()
const session = useSessionStore()
const appInfo = useAppInfo()
const dashboard = useDashboard()

const collapsed = ref(localStorage.getItem(COLLAPSED_KEY) === '1')
const drawerOpen = ref(false)
const narrow = ref(false)
let media: MediaQueryList | null = null
const onMedia = () => (narrow.value = media?.matches ?? false)

onMounted(() => {
  media = window.matchMedia('(max-width: 1023.98px)')
  onMedia()
  media.addEventListener('change', onMedia)
})
onBeforeUnmount(() => media?.removeEventListener('change', onMedia))

function toggleCollapsed() {
  collapsed.value = !collapsed.value
  localStorage.setItem(COLLAPSED_KEY, collapsed.value ? '1' : '0')
}

const items = computed(() =>
  ownerMenu.map((item) => ({
    ...item,
    count: dashboard.data.value && item.badge ? item.badge(dashboard.data.value) : 0,
    active: route.meta.section === item.section,
  })),
)

const breadcrumbs = computed(() => {
  const section = ownerMenu.find((m) => m.section === route.meta.section)
  const crumbs: { label: string; to?: string }[] = []
  if (section && section.to !== route.path) crumbs.push({ label: section.label, to: section.to })
  crumbs.push({ label: route.meta.title ?? '' })
  return crumbs
})

// Демонстрационный режим: жёлтая полоса с датой системы.
const isDemo = computed(() => appInfo.data.value?.env === 'demo')
const devClock = useQuery({
  queryKey: ['dev', 'clock'],
  queryFn: () => unwrap(api.GET('/dev/clock')),
  enabled: isDemo,
  retry: false,
})

// Поиск по договорам, нанимателям и объектам (по 5 результатов).
interface SearchHit {
  label: string
  group: string
  to: string
}
const searchText = ref<string | SearchHit>('')
const hits = ref<SearchHit[]>([])

async function search(event: { query: string }) {
  const q = event.query.trim()
  if (q.length < 2) {
    hits.value = []
    return
  }
  const query = { q, page: 1, pageSize: 5 }
  const [contracts, tenants, properties] = await Promise.allSettled([
    unwrap(api.GET('/contracts', { params: { query } })),
    unwrap(api.GET('/tenants', { params: { query } })),
    unwrap(api.GET('/properties', { params: { query } })),
  ])
  const result: SearchHit[] = []
  if (contracts.status === 'fulfilled') {
    for (const c of contracts.value.items) {
      result.push({ group: 'Договор', label: `№ ${c.number} — ${c.tenant.displayName}`, to: `/contracts/${c.id}` })
    }
  }
  if (tenants.status === 'fulfilled') {
    for (const t of tenants.value.items)
      result.push({ group: 'Наниматель', label: t.displayName, to: `/tenants/${t.id}` })
  }
  if (properties.status === 'fulfilled') {
    for (const p of properties.value.items) {
      result.push({ group: 'Объект', label: `${p.code} — ${p.address}`, to: `/properties/${p.id}` })
    }
  }
  hits.value = result
}

function openHit(event: { value: SearchHit }) {
  searchText.value = ''
  void router.push(event.value.to)
}

// Меню пользователя.
const userMenu = useTemplateRef<{ toggle: (e: Event) => void }>('userMenu')
const passwordDialog = ref(false)
const userMenuItems = [
  { label: 'Сменить пароль', icon: 'pi pi-key', command: () => (passwordDialog.value = true) },
  { label: 'Выйти', icon: 'pi pi-sign-out', command: () => logout() },
]

async function logout() {
  await session.logout().catch(() => undefined)
  await router.push('/login')
}
</script>

<template>
  <div class="owner" :class="{ 'owner--collapsed': collapsed && !narrow }">
    <div v-if="isDemo" class="owner__demo" role="status">
      Демонстрационный режим · дата системы:
      <b>{{ devClock.data.value ? formatDate(devClock.data.value.today) : '…' }}</b>
      · <RouterLink to="/settings/service">Изменить</RouterLink>
    </div>

    <aside v-if="!narrow" class="owner__sidebar" aria-label="Главное меню">
      <div class="owner__brand">
        <i class="pi pi-building" aria-hidden="true" />
        <span v-if="!collapsed">{{ APP_NAME }}</span>
      </div>
      <nav class="owner__nav">
        <RouterLink
          v-for="item in items"
          :key="item.section"
          :to="item.to"
          class="owner__link"
          :class="{ 'owner__link--active': item.active }"
          :title="collapsed ? item.label : undefined"
        >
          <i :class="item.icon" aria-hidden="true" />
          <span v-if="!collapsed" class="owner__label">{{ item.label }}</span>
          <Badge v-if="item.count > 0" :value="item.count" severity="danger" size="small" />
        </RouterLink>
      </nav>
      <Button
        text
        :icon="collapsed ? 'pi pi-angle-double-right' : 'pi pi-angle-double-left'"
        :aria-label="collapsed ? 'Развернуть меню' : 'Свернуть меню'"
        class="owner__collapse"
        @click="toggleCollapsed"
      />
    </aside>

    <Drawer v-model:visible="drawerOpen" header="Меню">
      <nav class="owner__nav">
        <RouterLink
          v-for="item in items"
          :key="item.section"
          :to="item.to"
          class="owner__link"
          :class="{ 'owner__link--active': item.active }"
          @click="drawerOpen = false"
        >
          <i :class="item.icon" aria-hidden="true" />
          <span class="owner__label">{{ item.label }}</span>
          <Badge v-if="item.count > 0" :value="item.count" severity="danger" size="small" />
        </RouterLink>
      </nav>
    </Drawer>

    <div class="owner__body">
      <header class="owner__topbar">
        <Button v-if="narrow" text icon="pi pi-bars" aria-label="Открыть меню" @click="drawerOpen = true" />
        <nav class="owner__crumbs" aria-label="Хлебные крошки">
          <template v-for="(c, i) in breadcrumbs" :key="i">
            <RouterLink v-if="c.to" :to="c.to">{{ c.label }}</RouterLink>
            <span v-else>{{ c.label }}</span>
            <i v-if="i < breadcrumbs.length - 1" class="pi pi-angle-right" aria-hidden="true" />
          </template>
        </nav>
        <AutoComplete
          v-model="searchText"
          :suggestions="hits"
          option-label="label"
          placeholder="Поиск: договор, наниматель, объект"
          class="owner__search"
          input-id="global-search"
          :delay="300"
          aria-label="Поиск по договорам, нанимателям и объектам"
          @complete="search"
          @option-select="openHit"
        >
          <template #option="{ option }">
            <small class="owner__hit-group">{{ option.group }}</small>
            <div>{{ option.label }}</div>
          </template>
        </AutoComplete>
        <Button
          text
          icon="pi pi-user"
          :label="session.user?.displayName"
          aria-haspopup="true"
          aria-controls="user-menu"
          @click="userMenu?.toggle($event)"
        />
        <Menu id="user-menu" ref="userMenu" :model="userMenuItems" popup />
        <Dialog v-model:visible="passwordDialog" modal header="Смена пароля" :style="{ width: '480px' }">
          <Message severity="info" :closable="false">Смена пароля будет доступна после этапа M1.</Message>
        </Dialog>
      </header>
      <main class="owner__content">
        <slot />
      </main>
    </div>
  </div>
</template>

<style scoped>
.owner {
  --sidebar-width: 240px;
  display: grid;
  grid-template-columns: var(--sidebar-width) 1fr;
  grid-template-rows: auto 1fr;
  min-height: 100vh;
  background: var(--app-surface-ground);
}
.owner--collapsed {
  --sidebar-width: 64px;
}
.owner__demo {
  grid-column: 1 / -1;
  padding: 0.375rem 1rem;
  background: #fde68a;
  color: #713f12;
  font-size: 0.875rem;
  text-align: center;
}
.owner__sidebar {
  display: flex;
  flex-direction: column;
  position: sticky;
  top: 0;
  height: 100vh;
  overflow-y: auto;
  background: var(--p-content-background);
  border-right: 1px solid var(--p-content-border-color);
}
.owner__brand {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  height: 56px;
  padding: 0 1.25rem;
  color: var(--p-primary-color);
  font-size: 1.125rem;
  font-weight: 600;
}
.owner__nav {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 0.5rem;
}
.owner__link {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  min-height: 40px;
  padding: 0 0.75rem;
  border-radius: var(--p-border-radius-md);
  color: var(--p-text-color);
  text-decoration: none;
}
.owner__link:hover {
  background: var(--p-content-hover-background);
}
.owner__link--active {
  background: var(--p-highlight-background);
  color: var(--p-highlight-color);
}
.owner__label {
  flex: 1;
}
.owner__collapse {
  margin-top: auto;
}
.owner__body {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.owner__topbar {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  height: 56px;
  padding: 0 1rem;
  background: var(--p-content-background);
  border-bottom: 1px solid var(--p-content-border-color);
}
.owner__crumbs {
  display: flex;
  flex: 1;
  align-items: center;
  gap: 0.5rem;
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
}
.owner__search {
  width: min(320px, 40vw);
}
.owner__hit-group {
  color: var(--p-text-muted-color);
}
.owner__content {
  width: 100%;
  max-width: 1440px;
  padding: 1.5rem;
}
@media (max-width: 1023.98px) {
  .owner {
    grid-template-columns: 1fr;
  }
  .owner__content {
    padding: 1rem;
  }
}
</style>
