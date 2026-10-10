<script setup lang="ts">
// Вкладка «Начисления» карточки объекта (14 Экраны собственника, раздел 2.3, вкладка 3).
import { computed, ref } from 'vue'
import type { ChargesQuery } from '@/api/queries/charges'
import ChargeDrawer from '@/modules/charges/components/ChargeDrawer.vue'
import ChargesTable from '@/modules/charges/components/ChargesTable.vue'

const props = defineProps<{ propertyId: string }>()
const page = ref(1)
const pageSize = ref(25)
const sort = ref<ChargesQuery['sort']>()
const query = computed<ChargesQuery>(() => ({
  propertyId: props.propertyId,
  page: page.value,
  pageSize: pageSize.value,
  sort: sort.value,
}))
const opened = ref<string | null>(null)
</script>

<template>
  <div>
    <ChargesTable
      :query="query"
      @page="
        (p, s) => {
          page = p
          pageSize = s
        }
      "
      @sort="(s) => (sort = s as ChargesQuery['sort'])"
      @open="(id) => (opened = id)"
    />
    <ChargeDrawer v-model:charge-id="opened" />
  </div>
</template>
