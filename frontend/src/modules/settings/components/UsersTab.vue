<script setup lang="ts">
// Настройки → Пользователи (14 Экраны собственника, раздел 16.7): собственники и наниматели с учётной записью.
import { computed, ref } from 'vue'
import { useForm } from 'vee-validate'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { useUserMutations, useUsers } from '@/api/queries/settings'
import type { components } from '@/api/schema'
import { useSessionStore } from '@/app/session'
import { formatDateTime } from '@/utils/date'
import { userRoleLabels, userStatusLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import { toTypedSchema } from '@/utils/zodSchema'
import { userCreateSchema } from '../schemas'

type User = components['schemas']['UserDTO']

const session = useSessionStore()
const toast = useToast()
const confirm = useConfirm()
const page = ref(1)
const pageSize = ref(25)
const users = useUsers(computed(() => ({ page: page.value, pageSize: pageSize.value })))
const m = useUserMutations()

const statusSeverity: Record<User['status'], 'success' | 'warn' | 'danger'> = {
  active: 'success',
  invited: 'warn',
  blocked: 'danger',
}

function onPage(e: { page: number; rows: number }) {
  page.value = e.page + 1
  pageSize.value = e.rows
}

function fail(err: unknown) {
  toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
}

function toggleBlock(u: User) {
  const block = u.status === 'active'
  confirm.require({
    header: block ? 'Заблокировать пользователя?' : 'Разблокировать пользователя?',
    message: block
      ? `${u.displayName} не сможет войти, открытые сеансы будут завершены.`
      : `${u.displayName} снова сможет входить в систему.`,
    acceptLabel: block ? 'Заблокировать' : 'Разблокировать',
    rejectLabel: 'Отмена',
    acceptProps: { severity: block ? 'danger' : undefined },
    accept: async () => {
      try {
        await m.update.mutateAsync({ id: u.id, body: { status: block ? 'blocked' : 'active' } })
        toast.add({
          severity: 'success',
          summary: block ? 'Пользователь заблокирован' : 'Пользователь разблокирован',
          life: 3000,
        })
      } catch (err) {
        fail(err)
      }
    },
  })
}

async function resend(u: User) {
  try {
    await m.resendInvite.mutateAsync(u.id)
    toast.add({ severity: 'success', summary: 'Приглашение отправлено', life: 3000 })
  } catch (err) {
    fail(err)
  }
}

// Диалог «Добавить собственника».
const dialog = ref(false)
const serverError = ref<string | null>(null)
const { defineField, errors, handleSubmit, resetForm, setErrors, isSubmitting } = useForm({
  validationSchema: toTypedSchema(userCreateSchema),
  initialValues: { displayName: '', email: '' },
})
const [displayName, displayNameAttrs] = defineField('displayName')
const [email, emailAttrs] = defineField('email')

function openCreate() {
  resetForm()
  serverError.value = null
  dialog.value = true
}

const onCreate = handleSubmit(async (v) => {
  serverError.value = null
  try {
    await m.create.mutateAsync({ displayName: v.displayName, email: v.email })
    dialog.value = false
    toast.add({
      severity: 'success',
      summary: 'Собственник добавлен',
      detail: 'Приглашение отправлено на e-mail',
      life: 3000,
    })
  } catch (err) {
    const p = parseProblem(err)
    setErrors(p.fieldErrors)
    serverError.value = p.code === 'conflict_exists' ? null : p.message
    if (p.code === 'conflict_exists') setErrors({ email: p.message })
  }
})
</script>

<template>
  <div class="form">
    <div class="actions">
      <Button label="Добавить собственника" icon="pi pi-user-plus" @click="openCreate" />
    </div>
    <DataTable
      :value="users.data.value?.items ?? []"
      :loading="users.isFetching.value"
      lazy
      paginator
      :rows="pageSize"
      :total-records="users.data.value?.total ?? 0"
      :rows-per-page-options="[25, 50, 100]"
      striped-rows
      size="small"
      data-key="id"
      @page="onPage"
    >
      <template #empty>Пользователей нет</template>
      <Column field="displayName" header="Имя" />
      <Column field="email" header="E-mail" />
      <Column header="Роль">
        <template #body="{ data }">{{ userRoleLabels[(data as User).role] }}</template>
      </Column>
      <Column header="Статус">
        <template #body="{ data }">
          <Tag :value="userStatusLabels[(data as User).status]" :severity="statusSeverity[(data as User).status]" />
        </template>
      </Column>
      <Column header="Последний вход">
        <template #body="{ data }">
          {{ (data as User).lastLoginAt ? formatDateTime((data as User).lastLoginAt!) : '—' }}
        </template>
      </Column>
      <Column header="" :style="{ width: '1%', whiteSpace: 'nowrap' }">
        <template #body="{ data }">
          <Button
            v-if="(data as User).status === 'invited'"
            text
            size="small"
            label="Отправить приглашение повторно"
            @click="resend(data as User)"
          />
          <Button
            v-else-if="(data as User).id !== session.user?.id"
            text
            size="small"
            :severity="(data as User).status === 'active' ? 'danger' : undefined"
            :label="(data as User).status === 'active' ? 'Заблокировать' : 'Разблокировать'"
            @click="toggleBlock(data as User)"
          />
        </template>
      </Column>
    </DataTable>

    <Dialog
      v-model:visible="dialog"
      modal
      header="Добавить собственника"
      :style="{ width: '480px' }"
      :breakpoints="{ '575px': '100vw' }"
    >
      <form class="form" novalidate @submit="onCreate">
        <div class="field">
          <label for="user-name">Имя *</label>
          <InputText
            id="user-name"
            v-model="displayName"
            v-bind="displayNameAttrs"
            :invalid="!!errors.displayName"
            fluid
          />
          <small v-if="errors.displayName" class="error">{{ errors.displayName }}</small>
          <small v-else class="hint">Как обращаться в письмах: «Александр Васильевич»</small>
        </div>
        <div class="field">
          <label for="user-email">E-mail *</label>
          <InputText id="user-email" v-model="email" v-bind="emailAttrs" type="email" :invalid="!!errors.email" fluid />
          <small v-if="errors.email" class="error">{{ errors.email }}</small>
        </div>
        <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
        <div class="actions">
          <Button type="button" label="Отмена" outlined @click="dialog = false" />
          <Button type="submit" label="Добавить и пригласить" :loading="isSubmitting" />
        </div>
      </form>
    </Dialog>
  </div>
</template>
