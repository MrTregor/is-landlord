<script setup lang="ts">
// Заявки нанимателя (15 Экраны нанимателя, раздел 6.1): «Сообщить о неисправности» и карточки заявок.
import { useCabinetRequests } from '@/api/queries/cabinet'
import StatusTag from '@/components/StatusTag.vue'
import { formatDateTime } from '@/utils/date'
import { requestCategoryLabels, requestStatusLabels, requestStatusSeverity } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import { useCabinetContractId } from '../useContract'

const { summary, contractId, contractOptions, selectContract, link } = useCabinetContractId()
const requests = useCabinetRequests(contractId)
</script>

<template>
  <div class="requests">
    <h1 class="title">Заявки</h1>
    <Select
      v-if="contractOptions.length > 1"
      :model-value="contractId"
      :options="contractOptions"
      option-label="label"
      option-value="value"
      aria-label="Договор"
      fluid
      @update:model-value="selectContract"
    />
    <Message v-if="summary.isError.value || requests.isError.value" severity="error" :closable="false">
      {{ parseProblem(summary.error.value ?? requests.error.value).message }}
    </Message>
    <Message v-else-if="summary.data.value && !contractId" severity="info" :closable="false"
      >Договоров пока нет.</Message
    >
    <template v-else>
      <Button
        as="router-link"
        :to="link('/cabinet/requests/new')"
        label="Сообщить о неисправности"
        icon="pi pi-wrench"
        class="wide"
      />
      <Skeleton v-if="requests.isPending.value" height="6rem" />
      <p v-else-if="!requests.data.value?.length" class="hint">Заявок пока нет</p>
      <RouterLink
        v-for="r in requests.data.value ?? []"
        :key="r.id"
        :to="link(`/cabinet/requests/${r.id}`)"
        class="card"
      >
        <div class="card__head">
          <strong>{{ r.title }}</strong>
          <StatusTag :label="requestStatusLabels[r.status]" :severity="requestStatusSeverity[r.status]" />
        </div>
        <div class="hint">
          {{ r.number }} · {{ requestCategoryLabels[r.category] }} · {{ formatDateTime(r.createdAt) }}
        </div>
      </RouterLink>
    </template>
  </div>
</template>

<style scoped>
.requests {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}
.title {
  font-size: 1.25rem;
  margin: 0;
}
.wide {
  width: 100%;
  min-height: 44px;
}
.card {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  min-height: 44px;
  padding: 0.75rem 1rem;
  background: var(--p-content-background);
  border: 1px solid var(--p-content-border-color);
  border-radius: 8px;
  color: inherit;
  text-decoration: none;
}
.card__head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 0.75rem;
  overflow-wrap: anywhere;
}
</style>
