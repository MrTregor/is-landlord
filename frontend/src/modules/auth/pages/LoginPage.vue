<script setup lang="ts">
// Вход (14 Экраны собственника, раздел 0): e-mail, пароль с кнопкой показа, «Войти», «Забыли пароль?».
// После входа: собственник → next или /, наниматель → /cabinet.
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useForm } from 'vee-validate'
import { useSessionStore } from '@/app/session'
import { homeFor } from '@/app/guards'
import { parseProblem } from '@/utils/problem'
import { toTypedSchema } from '@/utils/zodSchema'
import { loginSchema } from '../schemas'

const route = useRoute()
const router = useRouter()
const session = useSessionStore()

const { defineField, errors, handleSubmit, isSubmitting, setErrors } = useForm({
  validationSchema: toTypedSchema(loginSchema),
  initialValues: { email: '', password: '' },
})
const [email, emailAttrs] = defineField('email', { validateOnModelUpdate: false })
const [password, passwordAttrs] = defineField('password', { validateOnModelUpdate: false })

const serverError = ref<string | null>(null)

const onSubmit = handleSubmit(async (values) => {
  serverError.value = null
  try {
    const user = await session.login(values.email, values.password)
    const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : null
    await router.replace(user.role === 'owner' && next ? next : homeFor(user.role))
  } catch (err) {
    const p = parseProblem(err)
    setErrors(p.fieldErrors)
    serverError.value = p.message
  }
})
</script>

<template>
  <form class="login" novalidate @submit="onSubmit">
    <h2 class="login__title">Вход</h2>

    <div class="login__field">
      <label for="login-email">E-mail <span aria-hidden="true">*</span></label>
      <InputText
        id="login-email"
        v-model="email"
        v-bind="emailAttrs"
        type="email"
        autocomplete="username"
        inputmode="email"
        :invalid="!!errors.email"
        fluid
      />
      <small v-if="errors.email" class="login__error">{{ errors.email }}</small>
    </div>

    <div class="login__field">
      <label for="login-password">Пароль <span aria-hidden="true">*</span></label>
      <Password
        v-model="password"
        v-bind="passwordAttrs"
        input-id="login-password"
        :feedback="false"
        toggle-mask
        autocomplete="current-password"
        :invalid="!!errors.password"
        fluid
      />
      <small v-if="errors.password" class="login__error">{{ errors.password }}</small>
    </div>

    <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>

    <Button type="submit" label="Войти" icon="pi pi-sign-in" :loading="isSubmitting" fluid />
    <RouterLink to="/forgot-password" class="login__forgot">Забыли пароль?</RouterLink>
  </form>
</template>

<style scoped>
.login {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
.login__title {
  margin: 0;
  font-size: 1.25rem;
  font-weight: 600;
}
.login__field {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
}
.login__error {
  color: var(--p-red-500);
}
.login__forgot {
  align-self: center;
}
</style>
