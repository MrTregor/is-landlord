<script setup lang="ts">
// Настройки → Шаблоны писем (14 Экраны собственника, раздел 16, вкладка 4): Название, Кому, Тема, Включён.
import { useRouter } from 'vue-router'
import { useEmailTemplates, type EmailTemplateItem } from '@/api/queries/notify'
import StatusTag from '@/components/StatusTag.vue'
import { emailRecipientLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'

const router = useRouter()
const list = useEmailTemplates()
const t = (x: unknown) => x as EmailTemplateItem
</script>

<template>
  <div>
    <Skeleton v-if="list.isPending.value" height="10rem" />
    <Message v-else-if="list.isError.value" severity="error" :closable="false">
      {{ parseProblem(list.error.value).message }}
    </Message>
    <DataTable
      v-else
      :value="list.data.value"
      data-key="code"
      size="small"
      row-hover
      class="clickable"
      @row-click="(e) => router.push(`/settings/email-templates/${t(e.data).code}`)"
    >
      <Column header="Название">
        <template #body="{ data }">
          <RouterLink :to="`/settings/email-templates/${t(data).code}`">{{ t(data).name }}</RouterLink>
        </template>
      </Column>
      <Column header="Кому">
        <template #body="{ data }">{{ emailRecipientLabels[t(data).recipient] }}</template>
      </Column>
      <Column header="Тема">
        <template #body="{ data }">{{ t(data).subject }}</template>
      </Column>
      <Column header="Включён">
        <template #body="{ data }">
          <StatusTag :label="t(data).enabled ? 'Да' : 'Нет'" :severity="t(data).enabled ? 'success' : 'secondary'" />
        </template>
      </Column>
    </DataTable>
  </div>
</template>

<style scoped>
.clickable :deep(tbody tr) {
  cursor: pointer;
}
</style>
