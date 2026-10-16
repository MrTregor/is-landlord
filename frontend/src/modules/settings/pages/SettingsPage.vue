<script setup lang="ts">
// Настройки (14 Экраны собственника, раздел 16). Выбранная вкладка — в адресе /settings/:tab.
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import PageHeader from '@/components/PageHeader.vue'
import DocumentTemplatesTab from '../components/DocumentTemplatesTab.vue'
import EmailTemplatesTab from '../components/EmailTemplatesTab.vue'
import NotificationsTab from '../components/NotificationsTab.vue'
import OrganizationTab from '../components/OrganizationTab.vue'
import PdnTab from '../components/PdnTab.vue'
import ServiceTab from '../components/ServiceTab.vue'
import TariffsTab from '../components/TariffsTab.vue'
import TaxesTab from '../components/TaxesTab.vue'
import UsersTab from '../components/UsersTab.vue'

const tabs = [
  { value: 'organization', label: 'Организация', component: OrganizationTab },
  { value: 'taxes', label: 'Налоги и фискализация', component: TaxesTab },
  { value: 'notifications', label: 'Уведомления и сроки', component: NotificationsTab },
  { value: 'email-templates', label: 'Шаблоны писем', component: EmailTemplatesTab },
  { value: 'document-templates', label: 'Шаблоны документов', component: DocumentTemplatesTab },
  { value: 'tariffs', label: 'Тарифы', component: TariffsTab },
  { value: 'users', label: 'Пользователи', component: UsersTab },
  { value: 'pdn', label: 'ПДн', component: PdnTab },
  { value: 'service', label: 'Служебное', component: ServiceTab },
] as const

const route = useRoute()
const router = useRouter()
const active = computed({
  get: () => {
    const t = String(route.params.tab ?? '')
    return tabs.some((x) => x.value === t) ? t : 'organization'
  },
  set: (v: string | number) => void router.replace(`/settings/${v}`),
})
</script>

<template>
  <div>
    <PageHeader title="Настройки" />
    <Tabs v-model:value="active" scrollable>
      <TabList>
        <Tab v-for="t in tabs" :key="t.value" :value="t.value">{{ t.label }}</Tab>
      </TabList>
      <TabPanels>
        <TabPanel v-for="t in tabs" :key="t.value" :value="t.value">
          <component :is="t.component" />
        </TabPanel>
      </TabPanels>
    </Tabs>
  </div>
</template>
