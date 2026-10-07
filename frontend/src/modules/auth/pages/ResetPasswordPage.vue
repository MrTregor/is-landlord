<script setup lang="ts">
// Восстановление пароля, шаг 2: новый пароль по ссылке из письма.
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useForm } from 'vee-validate'
import { useToast } from 'primevue/usetoast'
import { api, unwrap } from '@/api/client'
import { parseProblem } from '@/utils/problem'
import { toTypedSchema } from '@/utils/zodSchema'
import { newPasswordSchema } from '../schemas'

const route = useRoute()
const router = useRouter()
const toast = useToast()
const token = String(route.params.token ?? '')

const { defineField, errors, handleSubmit, isSubmitting, setFieldError } = useForm({
  validationSchema: toTypedSchema(newPasswordSchema),
  initialValues: { password: '', repeat: '' },
})
const [password, passwordAttrs] = defineField('password')
const [repeat, repeatAttrs] = defineField('repeat')
const serverError = ref<string | null>(null)

const onSubmit = handleSubmit(async (values) => {
  serverError.value = null
  try {
    await unwrap(api.POST('/auth/password/reset', { body: { token, newPassword: values.password } }))
    toast.add({ severity: 'success', summary: 'Пароль изменён', life: 3000 })
    await router.replace('/login')
  } catch (err) {
    const p = parseProblem(err)
    if (p.fieldErrors.newPassword) setFieldError('password', p.fieldErrors.newPassword)
    else serverError.value = p.message
  }
})
</script>

<template>
  <form class="form" novalidate @submit="onSubmit">
    <h2 class="title">Новый пароль</h2>
    <div class="field">
      <label for="reset-password">Новый пароль *</label>
      <Password
        v-model="password"
        v-bind="passwordAttrs"
        input-id="reset-password"
        :feedback="false"
        toggle-mask
        autocomplete="new-password"
        :invalid="!!errors.password"
        fluid
      />
      <small v-if="errors.password" class="error">{{ errors.password }}</small>
    </div>
    <div class="field">
      <label for="reset-repeat">Повтор *</label>
      <Password
        v-model="repeat"
        v-bind="repeatAttrs"
        input-id="reset-repeat"
        :feedback="false"
        toggle-mask
        autocomplete="new-password"
        :invalid="!!errors.repeat"
        fluid
      />
      <small v-if="errors.repeat" class="error">{{ errors.repeat }}</small>
    </div>
    <Message v-if="serverError" severity="error" :closable="false">
      {{ serverError }} <RouterLink to="/forgot-password">Запросить ссылку заново</RouterLink>
    </Message>
    <Button type="submit" label="Сохранить" :loading="isSubmitting" fluid />
  </form>
</template>

<style scoped>
.title {
  margin: 0;
  font-size: 1.25rem;
  font-weight: 600;
}
</style>
