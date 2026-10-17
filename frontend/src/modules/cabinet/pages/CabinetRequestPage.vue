<script setup lang="ts">
// Заявка нанимателя (15 Экраны нанимателя, раздел 6.3): статус, описание, фото, лента, комментарий;
// для выполненной — «Подтверждаю выполнение» и «Не устранено» (с комментарием).
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useToast } from 'primevue/usetoast'
import { useCabinetRequest, useCabinetRequestMutations } from '@/api/queries/cabinet'
import type { components } from '@/api/schema'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate, formatDateTime } from '@/utils/date'
import { requestCategoryLabels, requestStatusLabels, requestStatusSeverity } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import { useCabinetContractId } from '../useContract'

type Ev = components['schemas']['RequestEventDTO']

const route = useRoute()
const toast = useToast()
const { link } = useCabinetContractId()
const id = computed(() => route.params.id as string)
const req = useCabinetRequest(id)
const r = computed(() => req.data.value)
const m = useCabinetRequestMutations()

function eventTitle(e: Ev) {
  if (e.kind === 'comment') return 'Комментарий'
  if (!e.fromStatus) return 'Заявка создана'
  return `Статус: ${requestStatusLabels[e.toStatus!]}`
}
const who = (e: Ev) => (e.authorRole === 'tenant' ? 'Вы' : e.authorRole === 'owner' ? 'Собственник' : '')

const comment = ref('')
async function sendComment() {
  const text = comment.value.trim()
  if (!text || !r.value) return
  try {
    await m.comment.mutateAsync({ id: r.value.id, text })
    comment.value = ''
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(e).message, life: 6000 })
  }
}

async function confirm() {
  if (!r.value) return
  try {
    await m.confirm.mutateAsync(r.value.id)
    toast.add({ severity: 'success', summary: 'Спасибо! Заявка закрыта', life: 3000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(e).message, life: 6000 })
  }
}

const reopenDialog = ref(false)
const reopenText = ref('')
const reopenError = ref<string | null>(null)
function openReopen() {
  reopenText.value = ''
  reopenError.value = null
  reopenDialog.value = true
}
async function reopen() {
  if (!r.value) return
  if (!reopenText.value.trim()) {
    reopenError.value = 'Опишите, что не устранено'
    return
  }
  try {
    await m.reopen.mutateAsync({ id: r.value.id, comment: reopenText.value.trim() })
    reopenDialog.value = false
    toast.add({ severity: 'success', summary: 'Заявка возвращена в работу', life: 3000 })
  } catch (e) {
    reopenError.value = parseProblem(e).message
  }
}
</script>

<template>
  <div class="request">
    <RouterLink :to="link('/cabinet/requests')" class="back">
      <i class="pi pi-arrow-left" aria-hidden="true" /> Заявки
    </RouterLink>
    <Skeleton v-if="req.isPending.value" height="10rem" />
    <Message v-else-if="req.isError.value" severity="error" :closable="false">
      {{ parseProblem(req.error.value).message }}
    </Message>
    <template v-else-if="r">
      <header>
        <div class="hint">{{ r.number }} · {{ requestCategoryLabels[r.category] }}</div>
        <h1 class="title">{{ r.title }}</h1>
        <StatusTag :label="requestStatusLabels[r.status]" :severity="requestStatusSeverity[r.status]" class="status" />
        <p v-if="r.plannedDate" class="hint">Плановая дата работ: {{ formatDate(r.plannedDate) }}</p>
      </header>
      <section class="card">
        <p class="text">{{ r.description }}</p>
        <div v-if="r.photos.length" class="photos">
          <a v-for="p in r.photos" :key="p.id" :href="p.url" target="_blank" rel="noopener">
            <img :src="p.url" :alt="p.name" />
          </a>
        </div>
      </section>
      <div v-if="r.status === 'done'" class="done">
        <Button
          label="Подтверждаю выполнение"
          icon="pi pi-check"
          class="wide"
          :loading="m.confirm.isPending.value"
          @click="confirm"
        />
        <Button label="Не устранено" icon="pi pi-undo" outlined class="wide" @click="openReopen" />
      </div>
      <section class="card">
        <h2>Ход работ</h2>
        <ul class="feed">
          <li v-for="e in r.events" :key="e.id">
            <div class="feed__head">
              <b>{{ eventTitle(e) }}</b>
              <span class="hint">{{ who(e) }} · {{ formatDateTime(e.createdAt) }}</span>
            </div>
            <p v-if="e.text" class="text">{{ e.text }}</p>
          </li>
        </ul>
        <form v-if="r.status !== 'closed'" class="comment-form" @submit.prevent="sendComment">
          <label for="cr-comment">Комментарий</label>
          <Textarea id="cr-comment" v-model="comment" rows="2" auto-resize maxlength="4000" fluid />
          <Button
            type="submit"
            label="Отправить"
            :disabled="!comment.trim()"
            :loading="m.comment.isPending.value"
            class="wide"
          />
        </form>
      </section>
    </template>

    <Dialog
      v-model:visible="reopenDialog"
      modal
      header="Не устранено"
      :style="{ width: '480px' }"
      :breakpoints="{ '520px': '100vw' }"
    >
      <form class="comment-form" novalidate @submit.prevent="reopen">
        <label for="cr-reopen">Комментарий *</label>
        <Textarea id="cr-reopen" v-model="reopenText" rows="3" auto-resize maxlength="4000" fluid />
        <small v-if="reopenError" class="error">{{ reopenError }}</small>
        <Button type="submit" label="Вернуть в работу" class="wide" :loading="m.reopen.isPending.value" />
      </form>
    </Dialog>
  </div>
</template>

<style scoped>
.request {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
.back {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  min-height: 44px;
}
.title {
  font-size: 1.25rem;
  margin: 0.25rem 0 0.5rem;
  overflow-wrap: anywhere;
}
.status {
  font-size: 1rem;
}
header p {
  margin: 0.5rem 0 0;
}
.card {
  background: var(--p-content-background);
  border: 1px solid var(--p-content-border-color);
  border-radius: 8px;
  padding: 0.75rem 1rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  min-width: 0;
}
.card h2 {
  margin: 0;
  font-size: 1rem;
  color: var(--p-text-muted-color);
}
.text {
  margin: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.photos {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.photos img {
  width: 88px;
  height: 88px;
  object-fit: cover;
  border-radius: 6px;
}
.done {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.feed {
  list-style: none;
  margin: 0;
  padding: 0;
}
.feed li {
  padding: 0.5rem 0;
  border-bottom: 1px solid var(--p-content-border-color);
}
.feed__head {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 0.25rem 0.75rem;
}
.comment-form {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.error {
  color: var(--p-red-600);
}
.wide {
  width: 100%;
  min-height: 44px;
}
</style>
