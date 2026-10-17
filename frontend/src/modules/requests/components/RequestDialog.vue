<script setup lang="ts">
// Диалог «Новая заявка» (14 Экраны собственника, раздел 9.1): Объект*, Договор (подставляется действующий),
// Категория*, Заголовок*, Описание*, Приоритет, Фото (до 5, JPEG/PNG/WebP).
import { computed, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useToast } from 'primevue/usetoast'
import { useContracts } from '@/api/queries/contracts'
import { useRequestMutations } from '@/api/queries/maintenance'
import { uploadFile, useProperties } from '@/api/queries/properties'
import type { components } from '@/api/schema'
import { options, requestCategoryLabels, requestPriorityLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'

type S = components['schemas']
const props = defineProps<{ propertyId?: string }>()
const visible = defineModel<boolean>('visible', { required: true })
const router = useRouter()
const toast = useToast()
const m = useRequestMutations()
const MAX_PHOTOS = 5

const form = reactive({
  propertyId: null as string | null,
  contractId: null as string | null,
  category: null as S['RequestCategory'] | null,
  title: '',
  description: '',
  priority: 'normal' as S['RequestPriority'],
  photos: [] as S['FileDTO'][],
})
const submitted = ref(false)
const uploading = ref(false)
const serverErrors = ref<Record<string, string>>({})
const serverError = ref<string | null>(null)

watch(
  visible,
  (v) => {
    if (!v) return
    Object.assign(form, {
      propertyId: props.propertyId ?? null,
      contractId: null,
      category: null,
      title: '',
      description: '',
      priority: 'normal',
      photos: [],
    })
    submitted.value = false
    serverErrors.value = {}
    serverError.value = null
  },
  { immediate: true },
)

const properties = useProperties(() => ({ archived: false, pageSize: 100 }))
const propertyOptions = computed(() =>
  (properties.data.value?.items ?? []).map((p) => ({ value: p.id, label: `${p.code} — ${p.address}` })),
)
const contracts = useContracts(() => ({
  propertyId: form.propertyId ?? undefined,
  status: ['active', 'extended'],
  pageSize: 20,
}))
const contractOptions = computed(() =>
  form.propertyId
    ? (contracts.data.value?.items ?? []).map((c) => ({
        value: c.id,
        label: `№ ${c.number} — ${c.tenant.displayName}`,
      }))
    : [],
)
// Договор по умолчанию — действующий договор объекта.
watch(contractOptions, (list) => {
  if (!form.contractId || !list.some((c) => c.value === form.contractId)) form.contractId = list[0]?.value ?? null
})

async function pick(e: Event) {
  const input = e.target as HTMLInputElement
  const files = Array.from(input.files ?? []).slice(0, MAX_PHOTOS - form.photos.length)
  uploading.value = true
  serverErrors.value = {}
  try {
    for (const f of files) form.photos.push(await uploadFile(f, 'request_photo'))
  } catch (err) {
    serverErrors.value = { photoFileIds: parseProblem(err).message }
  } finally {
    uploading.value = false
    input.value = ''
  }
}

const errors = computed(() => {
  const e: Record<string, string> = {}
  if (!form.propertyId) e.propertyId = 'Выберите объект'
  if (!form.category) e.category = 'Выберите категорию'
  const n = form.title.trim().length
  if (n < 3 || n > 120) e.title = 'Заголовок — от 3 до 120 символов'
  if (!form.description.trim()) e.description = 'Опишите проблему'
  return { ...e, ...serverErrors.value }
})
const err = (f: string) => (submitted.value || serverErrors.value[f] ? errors.value[f] : undefined)

async function save() {
  submitted.value = true
  serverErrors.value = {}
  serverError.value = null
  if (Object.keys(errors.value).length) return
  try {
    const r = await m.create.mutateAsync({
      propertyId: form.propertyId!,
      contractId: form.contractId,
      category: form.category!,
      title: form.title.trim(),
      description: form.description.trim(),
      priority: form.priority,
      photoFileIds: form.photos.map((p) => p.id),
    })
    visible.value = false
    toast.add({ severity: 'success', summary: `Заявка ${r.number} создана`, life: 3000 })
    void router.push(`/requests/${r.id}`)
  } catch (e) {
    const p = parseProblem(e)
    serverErrors.value = p.fieldErrors
    serverError.value = Object.keys(p.fieldErrors).length ? null : p.message
  }
}
</script>

<template>
  <Dialog
    v-model:visible="visible"
    modal
    header="Новая заявка"
    :style="{ width: '600px' }"
    :breakpoints="{ '640px': '100vw' }"
  >
    <form class="form" novalidate @submit.prevent="save">
      <div class="form-grid">
        <div class="field">
          <label for="rq-property">Объект *</label>
          <Select
            v-model="form.propertyId"
            input-id="rq-property"
            :options="propertyOptions"
            option-label="label"
            option-value="value"
            filter
            :disabled="!!propertyId"
            :invalid="!!err('propertyId')"
            fluid
          />
          <small v-if="err('propertyId')" class="error">{{ err('propertyId') }}</small>
        </div>
        <div class="field">
          <label for="rq-contract">Договор</label>
          <Select
            v-model="form.contractId"
            input-id="rq-contract"
            :options="contractOptions"
            option-label="label"
            option-value="value"
            show-clear
            placeholder="Нет действующего"
            :invalid="!!err('contractId')"
            fluid
          />
          <small v-if="err('contractId')" class="error">{{ err('contractId') }}</small>
        </div>
        <div class="field">
          <label for="rq-category">Категория *</label>
          <Select
            v-model="form.category"
            input-id="rq-category"
            :options="options(requestCategoryLabels)"
            option-label="label"
            option-value="value"
            :invalid="!!err('category')"
            fluid
          />
          <small v-if="err('category')" class="error">{{ err('category') }}</small>
        </div>
        <div class="field">
          <label id="rq-priority">Приоритет</label>
          <SelectButton
            v-model="form.priority"
            :options="options(requestPriorityLabels)"
            option-label="label"
            option-value="value"
            :allow-empty="false"
            aria-labelledby="rq-priority"
          />
        </div>
      </div>
      <div class="field">
        <label for="rq-title">Заголовок *</label>
        <InputText id="rq-title" v-model="form.title" maxlength="120" :invalid="!!err('title')" fluid />
        <small v-if="err('title')" class="error">{{ err('title') }}</small>
      </div>
      <div class="field">
        <label for="rq-desc">Описание *</label>
        <Textarea
          id="rq-desc"
          v-model="form.description"
          rows="4"
          auto-resize
          maxlength="4000"
          :invalid="!!err('description')"
          fluid
        />
        <small v-if="err('description')" class="error">{{ err('description') }}</small>
      </div>
      <div class="field">
        <span class="label">Фото (до {{ MAX_PHOTOS }})</span>
        <div class="photos">
          <div v-for="(p, i) in form.photos" :key="p.id" class="photo">
            <img :src="p.url" :alt="p.name" />
            <Button
              icon="pi pi-times"
              rounded
              text
              size="small"
              aria-label="Убрать фото"
              @click="form.photos.splice(i, 1)"
            />
          </div>
          <label v-if="form.photos.length < MAX_PHOTOS" class="p-button p-button-outlined p-button-sm add">
            <i class="pi pi-camera" aria-hidden="true" /> {{ uploading ? 'Загрузка…' : 'Добавить фото' }}
            <input type="file" accept="image/jpeg,image/png,image/webp" multiple hidden @change="pick" />
          </label>
        </div>
        <small v-if="err('photoFileIds')" class="error">{{ err('photoFileIds') }}</small>
      </div>
      <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
      <div class="actions">
        <Button type="button" label="Отмена" outlined @click="visible = false" />
        <Button type="submit" label="Создать" :loading="m.create.isPending.value" :disabled="uploading" />
      </div>
    </form>
  </Dialog>
</template>

<style scoped>
.photos {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  align-items: center;
}
.photo {
  position: relative;
  width: 80px;
  height: 80px;
}
.photo img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  border-radius: 6px;
}
.photo :deep(.p-button) {
  position: absolute;
  top: 0;
  right: 0;
  background: rgb(255 255 255 / 80%);
}
.add {
  cursor: pointer;
  gap: 0.35rem;
}
.label {
  font-weight: 600;
}
</style>
