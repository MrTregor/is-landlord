// Договор кабинета на любой странице (15 Экраны нанимателя, раздел 2): из адреса (?contract=) или действующий
// по сводке. Список договоров для выбора — из той же сводки.
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useCabinetSummary } from '@/api/queries/cabinet'

export function useCabinetContractId() {
  const route = useRoute()
  const router = useRouter()
  const queryContract = computed(() => (typeof route.query.contract === 'string' ? route.query.contract : undefined))
  const summary = useCabinetSummary(queryContract)
  const contractId = computed(() => summary.data.value?.contractId ?? undefined)
  const current = computed(() => summary.data.value?.contracts.find((c) => c.id === contractId.value))
  const contractOptions = computed(() =>
    (summary.data.value?.contracts ?? []).map((c) => ({ value: c.id, label: `№ ${c.number}, ${c.propertyAddress}` })),
  )
  function selectContract(id: string) {
    void router.replace({ query: { ...route.query, contract: id } })
  }
  /** Ссылка внутри кабинета с выбранным договором. */
  function link(path: string) {
    return queryContract.value ? { path, query: { contract: queryContract.value } } : { path }
  }
  return { summary, contractId, current, contractOptions, selectContract, link }
}
