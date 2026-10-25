<script setup lang="ts">
// Диалог «Ввести в действие» (14 Экраны собственника, раздел 4.4).
import { computed, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import { api, unwrap } from '@/api/client'
import { useContractMutations, type ContractDetail } from '@/api/queries/contracts'
import { useTenant } from '@/api/queries/tenants'
import DateInput from '@/components/DateInput.vue'
import { parseProblem } from '@/utils/problem'
import { today, todayIso } from '@/utils/today'

const props = defineProps<{ contract: ContractDetail }>()
const visible = defineModel<boolean>('visible', { required: true })
const emit = defineEmits<{ activated: [contract: ContractDetail] }>()
const toast = useToast()
const m = useContractMutations(() => props.contract.id)
const tenant = useTenant(() => props.contract.tenant.id)

const concludedOn = ref<string | null>(todayIso())
const invite = ref(true)
const error = ref<string | null>(null)
const canInvite = computed(() => !!props.contract.tenant.email && tenant.data.value && !tenant.data.value.user)

watch(visible, (v) => {
  if (v) {
    concludedOn.value = todayIso()
    invite.value = true
    error.value = null
  }
})

async function submit() {
  error.value = null
  if (!concludedOn.value) {
    error.value = 'Укажите дату подписания'
    return
  }
  try {
    const d = await m.activate.mutateAsync({
      concludedOn: concludedOn.value,
      inviteTenant: !!canInvite.value && invite.value,
    })
    visible.value = false
    const charges = await unwrap(api.GET('/charges', { params: { query: { contractId: d.id, pageSize: 1 } } }))
    toast.add({
      severity: 'success',
      summary: `Договор введён в действие. Создано начислений: ${charges.total}`,
      life: 4000,
    })
    emit('activated', d)
  } catch (err) {
    error.value = parseProblem(err).message
  }
}
</script>

<template>
  <Dialog
    v-model:visible="visible"
    modal
    header="Ввести договор в действие"
    :style="{ width: '520px' }"
    :breakpoints="{ '575px': '100vw' }"
  >
    <form class="form" novalidate @submit.prevent="submit">
      <template v-if="contract.activationBlockers.length">
        <Message severity="error" :closable="false">
          Договор пока нельзя ввести в действие:
          <ul class="list">
            <li v-for="b in contract.activationBlockers" :key="b">{{ b }}</li>
          </ul>
        </Message>
      </template>
      <div class="field">
        <label for="a-concluded">Дата подписания *</label>
        <DateInput v-model="concludedOn" input-id="a-concluded" :max-date="today()" />
      </div>
      <div v-if="canInvite" class="field field--check">
        <Checkbox v-model="invite" input-id="a-invite" binary />
        <label for="a-invite">Пригласить нанимателя в личный кабинет</label>
      </div>
      <p class="hint">
        Объект станет «Сдан» с даты начала (или «Забронирован», если договор начнётся позже), в календарь добавятся
        сроки договора, будет создан черновик акта приёма-передачи.
      </p>
      <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
      <div class="actions">
        <Button type="button" label="Отмена" outlined @click="visible = false" />
        <Button
          type="submit"
          label="Ввести в действие"
          icon="pi pi-check"
          :disabled="!contract.canActivate"
          :loading="m.activate.isPending.value"
        />
      </div>
    </form>
  </Dialog>
</template>

<style scoped>
.list {
  margin: 0.5rem 0 0;
  padding-left: 1.25rem;
}
</style>
