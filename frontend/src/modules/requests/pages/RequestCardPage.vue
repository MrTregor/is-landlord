<script setup lang="ts">
// Карточка заявки (14 Экраны собственника, раздел 9.2): слева — описание, фото, лента событий и комментарий; справа —
// статус и переходы (05 разд. 14.1), приоритет, исполнитель, плановая дата (сохраняются сразу), «По вине нанимателя»;
// «Затраты по заявке» с «Добавить затрату».
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useToast } from 'primevue/usetoast'
import { useRequest, useRequestMutations, type RequestDetail } from '@/api/queries/maintenance'
import type { components } from '@/api/schema'
import DateInput from '@/components/DateInput.vue'
import MoneyText from '@/components/MoneyText.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import ExpenseDialog from '@/modules/expenses/components/ExpenseDialog.vue'
import { formatDate, formatDateTime } from '@/utils/date'
import {
  expenseRecoveryLabels,
  options,
  requestCategoryLabels,
  requestPriorityLabels,
  requestStatusLabels,
  requestStatusSeverity,
} from '@/utils/labels'
import { parseProblem } from '@/utils/problem'

type Status = components['schemas']['RequestStatus']
type Priority = components['schemas']['RequestPriority']

const route = useRoute()
const toast = useToast()
const id = computed(() => route.params.id as string)
const req = useRequest(id)
const m = useRequestMutations()
const r = computed(() => req.data.value)

// Поля управления — сохраняются сразу при изменении.
const priority = ref<Priority>('normal')
const contractor = ref('')
const plannedDate = ref<string | null>(null)
const tenantFault = ref(false)
watch(
  r,
  (d) => {
    if (!d) return
    priority.value = d.priority
    contractor.value = d.contractor ?? ''
    plannedDate.value = d.plannedDate
    tenantFault.value = d.tenantFault
  },
  { immediate: true },
)
const closed = computed(() => r.value?.status === 'closed')

async function savePatch() {
  const d = r.value
  if (!d) return
  const next = {
    priority: priority.value,
    contractor: contractor.value.trim() || null,
    plannedDate: plannedDate.value,
    tenantFault: tenantFault.value,
  }
  if (
    next.priority === d.priority &&
    next.contractor === d.contractor &&
    next.plannedDate === d.plannedDate &&
    next.tenantFault === d.tenantFault
  )
    return
  try {
    await m.update.mutateAsync({ id: d.id, body: next })
    toast.add({ severity: 'success', summary: 'Сохранено', life: 2000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(e).message, life: 6000 })
  }
}

interface Move {
  to: Status
  label: string
  icon: string
  field?: 'comment' | 'resultNote'
  prompt?: string
}
const moves = computed<Move[]>(() => {
  switch (r.value?.status) {
    case 'new':
      return [
        { to: 'in_progress', label: 'Взять в работу', icon: 'pi pi-play' },
        { to: 'closed', label: 'Закрыть', icon: 'pi pi-times', field: 'comment', prompt: 'Причина закрытия *' },
      ]
    case 'in_progress':
      return [{ to: 'done', label: 'Выполнена', icon: 'pi pi-check', field: 'resultNote', prompt: 'Результат работ *' }]
    case 'done':
      return [
        { to: 'closed', label: 'Закрыть', icon: 'pi pi-lock' },
        { to: 'in_progress', label: 'Вернуть в работу', icon: 'pi pi-undo', field: 'comment', prompt: 'Комментарий *' },
      ]
    default:
      return []
  }
})
const moving = ref<Move | null>(null)
const moveText = ref('')
const moveError = ref<string | null>(null)
const moveDialog = computed({
  get: () => moving.value !== null,
  set: (v: boolean) => {
    if (!v) moving.value = null
  },
})
async function doMove(mv: Move, text?: string) {
  const d = r.value
  if (!d) return
  moveError.value = null
  if (mv.field && !text?.trim()) {
    moveError.value = mv.field === 'resultNote' ? 'Опишите результат работ' : 'Укажите комментарий'
    return
  }
  try {
    await m.transition.mutateAsync({
      id: d.id,
      body: {
        toStatus: mv.to,
        comment: mv.field === 'comment' ? text!.trim() : null,
        resultNote: mv.field === 'resultNote' ? text!.trim() : null,
      },
    })
    moving.value = null
    toast.add({ severity: 'success', summary: `Статус: ${requestStatusLabels[mv.to]}`, life: 3000 })
  } catch (e) {
    const msg = parseProblem(e).message
    if (moving.value) moveError.value = msg
    else toast.add({ severity: 'error', summary: 'Ошибка', detail: msg, life: 6000 })
  }
}
function start(mv: Move) {
  if (!mv.field) {
    void doMove(mv)
    return
  }
  moveText.value = ''
  moveError.value = null
  moving.value = mv
}

const comment = ref('')
async function sendComment() {
  const d = r.value
  if (!d || !comment.value.trim()) return
  try {
    await m.comment.mutateAsync({ id: d.id, text: comment.value.trim() })
    comment.value = ''
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(e).message, life: 6000 })
  }
}

type Ev = RequestDetail['events'][number]
function eventTitle(e: Ev) {
  if (e.kind === 'comment') return 'Комментарий'
  if (!e.fromStatus) return 'Заявка создана'
  return `${requestStatusLabels[e.fromStatus]} → ${requestStatusLabels[e.toStatus!]}`
}
const who = (e: Ev) =>
  [e.authorName, e.authorRole === 'tenant' ? 'наниматель' : e.authorRole === 'owner' ? 'собственник' : null]
    .filter(Boolean)
    .join(', ')

const expenseDialog = ref(false)
</script>

<template>
  <div>
    <RouterLink to="/requests" class="back"><i class="pi pi-arrow-left" aria-hidden="true" /> Заявки</RouterLink>
    <Skeleton v-if="req.isPending.value" height="20rem" />
    <Message v-else-if="req.isError.value" severity="error" :closable="false">
      {{ parseProblem(req.error.value).message }}
    </Message>
    <template v-else-if="r">
      <PageHeader :title="`Заявка ${r.number}: ${r.title}`" />
      <div class="layout">
        <div class="main">
          <section class="block">
            <p class="meta">
              {{ requestCategoryLabels[r.category] }} ·
              <RouterLink :to="`/properties/${r.property.id}`">{{ r.property.code }}</RouterLink>
              <template v-if="r.contract">
                · договор <RouterLink :to="`/contracts/${r.contract.id}`">№ {{ r.contract.number }}</RouterLink>
              </template>
              · создана {{ formatDateTime(r.createdAt) }} ({{
                r.authorRole === 'tenant' ? 'наниматель' : 'собственник'
              }})
            </p>
            <p class="desc">{{ r.description }}</p>
            <div v-if="r.photos.length" class="photos">
              <a v-for="p in r.photos" :key="p.id" :href="p.url" target="_blank" rel="noopener">
                <img :src="p.url" :alt="p.name" />
              </a>
            </div>
            <p v-if="r.resultNote" class="result"><b>Результат работ:</b> {{ r.resultNote }}</p>
          </section>

          <section class="block">
            <h3>Лента</h3>
            <ul class="feed">
              <li v-for="e in r.events" :key="e.id" :class="{ comment: e.kind === 'comment' }">
                <div class="feed__head">
                  <b>{{ eventTitle(e) }}</b>
                  <span class="hint"
                    >{{ formatDateTime(e.createdAt) }}<template v-if="who(e)"> · {{ who(e) }}</template></span
                  >
                </div>
                <div v-if="e.text" class="feed__text">{{ e.text }}</div>
              </li>
            </ul>
            <form v-if="!closed" class="comment" @submit.prevent="sendComment">
              <label for="rc-comment" class="sr-only">Комментарий</label>
              <Textarea
                id="rc-comment"
                v-model="comment"
                rows="2"
                auto-resize
                maxlength="4000"
                placeholder="Комментарий"
                fluid
              />
              <Button
                type="submit"
                label="Отправить"
                icon="pi pi-send"
                :disabled="!comment.trim()"
                :loading="m.comment.isPending.value"
              />
            </form>
          </section>

          <section class="block">
            <div class="block__head">
              <h3>Затраты по заявке</h3>
              <Button label="Добавить затрату" icon="pi pi-plus" size="small" outlined @click="expenseDialog = true" />
            </div>
            <DataTable :value="r.expenses" size="small" data-key="id">
              <template #empty>Затрат нет</template>
              <Column header="Дата оплаты">
                <template #body="{ data }">{{ formatDate(data.paidOn) }}</template>
              </Column>
              <Column header="Описание" field="description" />
              <Column header="Сумма">
                <template #body="{ data }"><MoneyText :kop="data.amountKop" /></template>
              </Column>
              <Column header="Возмещение">
                <template #body="{ data }">{{ expenseRecoveryLabels[data.recovery as 'none'] }}</template>
              </Column>
            </DataTable>
          </section>
        </div>

        <aside class="side">
          <section class="block">
            <h3>Статус</h3>
            <StatusTag :label="requestStatusLabels[r.status]" :severity="requestStatusSeverity[r.status]" />
            <div v-if="moves.length" class="moves">
              <Button
                v-for="mv in moves"
                :key="mv.to + mv.label"
                :label="mv.label"
                :icon="mv.icon"
                size="small"
                :outlined="mv.to === 'closed' || mv.label === 'Вернуть в работу'"
                :loading="m.transition.isPending.value && !moving"
                @click="start(mv)"
              />
            </div>
          </section>
          <section class="block fields">
            <div class="field">
              <label id="rc-priority">Приоритет</label>
              <SelectButton
                v-model="priority"
                :options="options(requestPriorityLabels)"
                option-label="label"
                option-value="value"
                :allow-empty="false"
                :disabled="closed"
                aria-labelledby="rc-priority"
                @change="savePatch"
              />
            </div>
            <div class="field">
              <label for="rc-contractor">Исполнитель</label>
              <InputText
                id="rc-contractor"
                v-model="contractor"
                maxlength="200"
                :disabled="closed"
                fluid
                @blur="savePatch"
                @keydown.enter.prevent="savePatch"
              />
            </div>
            <div class="field">
              <label for="rc-planned">Плановая дата</label>
              <DateInput v-model="plannedDate" input-id="rc-planned" @update:model-value="savePatch" />
            </div>
            <div class="field field--check">
              <ToggleSwitch
                v-model="tenantFault"
                input-id="rc-fault"
                :disabled="closed"
                @update:model-value="savePatch"
              />
              <label for="rc-fault">По вине нанимателя</label>
            </div>
            <small v-if="tenantFault" class="hint">
              Затрату по заявке можно удержать из залога или выставить нанимателю начислением.
            </small>
          </section>
        </aside>
      </div>

      <Dialog
        v-model:visible="moveDialog"
        modal
        :header="moving?.label"
        :style="{ width: '480px' }"
        :breakpoints="{ '520px': '100vw' }"
      >
        <form v-if="moving" class="form" novalidate @submit.prevent="doMove(moving, moveText)">
          <div class="field">
            <label for="rc-move">{{ moving.prompt }}</label>
            <Textarea
              id="rc-move"
              v-model="moveText"
              rows="3"
              auto-resize
              maxlength="4000"
              :invalid="!!moveError"
              fluid
            />
            <small v-if="moveError" class="error">{{ moveError }}</small>
          </div>
          <div class="actions">
            <Button type="button" label="Отмена" outlined @click="moving = null" />
            <Button type="submit" label="Сохранить" :loading="m.transition.isPending.value" />
          </div>
        </form>
      </Dialog>
      <ExpenseDialog
        v-model:visible="expenseDialog"
        :property-id="r.property.id"
        :request-id="r.id"
        :tenant-fault="r.tenantFault"
      />
    </template>
  </div>
</template>

<style scoped>
.back {
  display: inline-block;
  margin-bottom: 0.5rem;
}
.layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 300px;
  gap: 1rem;
}
@media (max-width: 960px) {
  .layout {
    grid-template-columns: 1fr;
  }
}
.block {
  margin-bottom: 1rem;
}
.block h3 {
  margin: 0 0 0.5rem;
  font-size: 1rem;
}
.block__head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 0.5rem;
}
.block__head h3 {
  margin: 0;
}
.meta,
.hint {
  color: var(--p-text-muted-color);
}
.desc {
  white-space: pre-wrap;
}
.photos {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.photos img {
  width: 120px;
  height: 90px;
  object-fit: cover;
  border-radius: 6px;
}
.feed {
  list-style: none;
  margin: 0 0 0.75rem;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.feed li {
  border-left: 3px solid var(--p-content-border-color);
  padding: 0.25rem 0.75rem;
}
.feed li.comment {
  border-left-color: var(--p-primary-color);
}
.feed__head {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  align-items: baseline;
}
.feed__text {
  white-space: pre-wrap;
}
.comment {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  align-items: flex-end;
}
.moves {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  margin-top: 0.75rem;
}
.fields {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
}
</style>
