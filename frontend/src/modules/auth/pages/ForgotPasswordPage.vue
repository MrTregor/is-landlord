<script setup lang="ts">
// Восстановление пароля, шаг 1 (14 Экраны собственника, раздел 0): ответ не раскрывает наличие адреса.
import { ref } from 'vue'
import { useForm } from 'vee-validate'
import { api, unwrap } from '@/api/client'
import { parseProblem } from '@/utils/problem'
import { toTypedSchema } from '@/utils/zodSchema'
import { forgotSchema } from '../schemas'

const { defineField, errors, handleSubmit, isSubmitting } = useForm({
  validationSchema: toTypedSchema(forgotSchema),
  initialValues: { email: '' },
})
const [email, emailAttrs] = defineField('email', { validateOnModelUpdate: false })
const sent = ref(false)
const serverError = ref<string | null>(null)

const onSubmit = handleSubmit(async (values) => {
  serverError.value = null
  try {
    await unwrap(api.POST('/auth/password/forgot', { body: { email: values.email } }))
    sent.value = true
  } catch (err) {
    serverError.value = parseProblem(err).message
  }
})
</script>

<template>
  <div class="form">
    <h2 class="title">Восстановление пароля</h2>
    <template v-if="sent">
      <Message severity="success" :closable="false">
        Если адрес зарегистрирован, мы отправили письмо со ссылкой
      </Message>
      <RouterLink to="/login">Вернуться ко входу</RouterLink>
    </template>
    <form v-else class="form" novalidate @submit="onSubmit">
      <div class="field">
        <label for="forgot-email">E-mail *</label>
        <InputText
          id="forgot-email"
          v-model="email"
          v-bind="emailAttrs"
          type="email"
          autocomplete="username"
          :invalid="!!errors.email"
          fluid
        />
        <small v-if="errors.email" class="error">{{ errors.email }}</small>
      </div>
      <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
      <Button type="submit" label="Отправить ссылку" :loading="isSubmitting" fluid />
      <RouterLink to="/login" class="back">Вернуться ко входу</RouterLink>
    </form>
  </div>
</template>

<style scoped>
.title {
  margin: 0;
  font-size: 1.25rem;
  font-weight: 600;
}
.back {
  align-self: center;
}
</style>
