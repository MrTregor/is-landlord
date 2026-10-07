<script setup lang="ts">
// Смена пароля (меню пользователя рабочего места, профиль кабинета).
import { ref, watch } from 'vue'
import { useForm } from 'vee-validate'
import { useToast } from 'primevue/usetoast'
import { z } from 'zod'
import { api, unwrap } from '@/api/client'
import { passwordRule } from '@/modules/auth/schemas'
import { parseProblem } from '@/utils/problem'
import { toTypedSchema } from '@/utils/zodSchema'

const visible = defineModel<boolean>('visible', { required: true })
const toast = useToast()

const schema = z
  .object({
    currentPassword: z.string().min(1, 'Введите текущий пароль'),
    newPassword: passwordRule,
    repeat: z.string(),
  })
  .refine((v) => v.newPassword === v.repeat, { path: ['repeat'], message: 'Пароли не совпадают' })

const { defineField, errors, handleSubmit, isSubmitting, setErrors, resetForm } = useForm({
  validationSchema: toTypedSchema(schema),
  initialValues: { currentPassword: '', newPassword: '', repeat: '' },
})
const [current, currentAttrs] = defineField('currentPassword')
const [next, nextAttrs] = defineField('newPassword')
const [repeat, repeatAttrs] = defineField('repeat')
const serverError = ref<string | null>(null)

watch(visible, (v) => {
  if (v) {
    resetForm()
    serverError.value = null
  }
})

const onSubmit = handleSubmit(async (values) => {
  serverError.value = null
  try {
    await unwrap(
      api.POST('/auth/password/change', {
        body: { currentPassword: values.currentPassword, newPassword: values.newPassword },
      }),
    )
    visible.value = false
    toast.add({ severity: 'success', summary: 'Пароль изменён', detail: 'Другие сеансы завершены', life: 3000 })
  } catch (err) {
    const p = parseProblem(err)
    setErrors(p.fieldErrors)
    serverError.value = Object.keys(p.fieldErrors).length ? null : p.message
  }
})
</script>

<template>
  <Dialog
    v-model:visible="visible"
    modal
    header="Смена пароля"
    :style="{ width: '480px' }"
    :breakpoints="{ '575px': '100vw' }"
  >
    <form class="form" novalidate @submit="onSubmit">
      <div class="field">
        <label for="cp-current">Текущий пароль *</label>
        <Password
          v-model="current"
          v-bind="currentAttrs"
          input-id="cp-current"
          :feedback="false"
          toggle-mask
          autocomplete="current-password"
          :invalid="!!errors.currentPassword"
          fluid
        />
        <small v-if="errors.currentPassword" class="error">{{ errors.currentPassword }}</small>
      </div>
      <div class="field">
        <label for="cp-new">Новый пароль *</label>
        <Password
          v-model="next"
          v-bind="nextAttrs"
          input-id="cp-new"
          :feedback="false"
          toggle-mask
          autocomplete="new-password"
          :invalid="!!errors.newPassword"
          fluid
        />
        <small v-if="errors.newPassword" class="error">{{ errors.newPassword }}</small>
      </div>
      <div class="field">
        <label for="cp-repeat">Повтор нового пароля *</label>
        <Password
          v-model="repeat"
          v-bind="repeatAttrs"
          input-id="cp-repeat"
          :feedback="false"
          toggle-mask
          autocomplete="new-password"
          :invalid="!!errors.repeat"
          fluid
        />
        <small v-if="errors.repeat" class="error">{{ errors.repeat }}</small>
      </div>
      <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
      <div class="actions">
        <Button type="button" label="Отмена" outlined @click="visible = false" />
        <Button type="submit" label="Сменить пароль" :loading="isSubmitting" />
      </div>
    </form>
  </Dialog>
</template>
