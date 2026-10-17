<script setup lang="ts">
// Карточка объекта → «Заявки» (14 Экраны собственника, раздел 2.3): заявки объекта и «Новая заявка».
import { ref } from 'vue'
import { useRequests } from '@/api/queries/maintenance'
import RequestDialog from '@/modules/requests/components/RequestDialog.vue'
import RequestsTable from '@/modules/requests/components/RequestsTable.vue'

const props = defineProps<{ propertyId: string }>()
const page = ref(1)
const pageSize = ref(25)
const list = useRequests(() => ({ propertyId: props.propertyId, page: page.value, pageSize: pageSize.value }))
const dialog = ref(false)

function onPage(p: number, r: number) {
  page.value = p
  pageSize.value = r
}
</script>

<template>
  <div>
    <div class="actions actions--start">
      <Button label="Новая заявка" icon="pi pi-plus" @click="dialog = true" />
    </div>
    <RequestsTable
      :items="list.data.value?.items ?? []"
      :total="list.data.value?.total ?? 0"
      :loading="list.isFetching.value && !list.data.value"
      :first="(page - 1) * pageSize"
      :rows="pageSize"
      hide-property
      @page="onPage"
    />
    <RequestDialog v-model:visible="dialog" :property-id="propertyId" />
  </div>
</template>
