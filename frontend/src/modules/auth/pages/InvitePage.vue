<script setup lang="ts">
// Принятие приглашения (14 Экраны собственника, раздел 0): пароль, повтор, для нанимателя — ознакомление
// с политикой ПДн (согласие на обработку ПДн здесь не запрашивается — ст. 9 152-ФЗ).
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useQuery } from '@tanstack/vue-query'
import { useForm } from 'vee-validate'
import { api, unwrap } from '@/api/client'
import { useAppInfo } from '@/api/queries/public'
import { APP_NAME } from '@/app/constants'
import { homeFor } from '@/app/guards'
import { useSessionStore } from '@/app/session'
import { parseProblem } from '@/utils/problem'
import { toTypedSchema } from '@/utils/zodSchema'
import { newPasswordSchema } from '../schemas'

const route = useRoute()
const router = useRouter()
const session = useSessionStore()
const appInfo = useAppInfo()
const token = String(route.params.token ?? '')

const invite = useQuery({
  queryKey: ['auth', 'invite', token],
  queryFn: () => unwrap(api.GET('/auth/invite/{token}', { params: { path: { token } } })),
  retry: false,
})
const policy = useQuery({
  queryKey: ['public', 'privacy-policy'],
  queryFn: () => unwrap(api.GET('/public/privacy-policy')),
  enabled: computed(() => invite.data.value?.role === 'tenant'),
})

const { defineField, errors, handleSubmit, isSubmitting, setFieldError } = useForm({
  validationSchema: toTypedSchema(newPasswordSchema),
  initialValues: { password: '', repeat: '' },
})
const [password, passwordAttrs] = defineField('password')
const [repeat, repeatAttrs] = defineField('repeat')
const agree = ref(false)
const agreeError = ref<string | null>(null)
const serverError = ref<string | null>(null)
const isTenant = computed(() => invite.data.value?.role === 'tenant')

const onSubmit = handleSubmit(async (values) => {
  serverError.value = null
  agreeError.value = null
  if (isTenant.value && !agree.value) {
    agreeError.value = 'Подтвердите ознакомление с Политикой обработки персональных данных'
    return
  }
  try {
    const res = await unwrap(
      api.POST('/auth/invite/accept', {
        body: {
          token,
          password: values.password,
          acceptPolicyVersion: isTenant.value ? (policy.data.value?.version ?? null) : null,
        },
      }),
    )
    session.setAuth(res)
    await router.replace(homeFor(res.user.role))
  } catch (err) {
    const p = parseProblem(err)
    if (p.fieldErrors.password) setFieldError('password', p.fieldErrors.password)
    else if (p.fieldErrors.acceptPolicyVersion) agreeError.value = p.fieldErrors.acceptPolicyVersion
    else serverError.value = p.message
  }
})
</script>

<template>
  <Skeleton v-if="invite.isPending.value" height="10rem" />
  <Message v-else-if="invite.isError.value" severity="error" :closable="false">
    Приглашение недействительно или истекло. Обратитесь к {{ appInfo.data.value?.orgShortName ?? 'собственнику' }}.
  </Message>
  <form v-else class="form" novalidate @submit="onSubmit">
    <p class="lead">Вас пригласили в систему «{{ APP_NAME }}»: {{ invite.data.value?.email }}</p>
    <div class="field">
      <label for="invite-password">Пароль *</label>
      <Password
        v-model="password"
        v-bind="passwordAttrs"
        input-id="invite-password"
        :feedback="false"
        toggle-mask
        autocomplete="new-password"
        :invalid="!!errors.password"
        fluid
      />
      <small v-if="errors.password" class="error">{{ errors.password }}</small>
    </div>
    <div class="field">
      <label for="invite-repeat">Повтор *</label>
      <Password
        v-model="repeat"
        v-bind="repeatAttrs"
        input-id="invite-repeat"
        :feedback="false"
        toggle-mask
        autocomplete="new-password"
        :invalid="!!errors.repeat"
        fluid
      />
      <small v-if="errors.repeat" class="error">{{ errors.repeat }}</small>
    </div>
    <div v-if="isTenant" class="field">
      <div class="field--check field">
        <Checkbox v-model="agree" input-id="invite-agree" binary :invalid="!!agreeError" />
        <label for="invite-agree">
          Я ознакомлен(а) с
          <RouterLink to="/privacy" target="_blank">Политикой обработки персональных данных</RouterLink> *
        </label>
      </div>
      <small v-if="agreeError" class="error">{{ agreeError }}</small>
    </div>
    <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
    <Button type="submit" label="Создать пароль и войти" :loading="isSubmitting" fluid />
  </form>
</template>

<style scoped>
.lead {
  margin: 0;
}
</style>
