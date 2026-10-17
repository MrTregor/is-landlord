<script setup lang="ts">
// Новая заявка (15 Экраны нанимателя, раздел 6.2): категория плитками, заголовок, описание, до 5 фото с камеры.
import { computed, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useToast } from 'primevue/usetoast'
import { useCabinetRequestMutations } from '@/api/queries/cabinet'
import { uploadFile } from '@/api/queries/properties'
import type { components } from '@/api/schema'
import { requestCategoryLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import { useCabinetContractId } from '../useContract'

type S = components['schemas']
type Category = S['RequestCategory']

const MAX_PHOTOS = 5
const icons: Record<Category, string> = {
  plumbing: 'pi pi-filter',
  electrical: 'pi pi-bolt',
  heating: 'pi pi-sun',
  appliance: 'pi pi-desktop',
  furniture: 'pi pi-table',
  doors_windows: 'pi pi-th-large',
  other: 'pi pi-ellipsis-h',
}
const categories = Object.keys(requestCategoryLabels) as Category[]

const router = useRouter()
const toast = useToast()
const { contractId, current, link } = useCabinetContractId()
const m = useCabinetRequestMutations()

const form = reactive({ category: null as Category | null, title: '', description: '', photos: [] as S['FileDTO'][] })
const submitted = ref(false)
const uploading = ref(false)
const serverErrors = ref<Record<string, string>>({})
const serverError = ref<string | null>(null)

const errors = computed(() => {
  const e: Record<string, string> = {}
  if (!form.category) e.category = 'Выберите категорию'
  const t = form.title.trim()
  if (t.length < 3 || t.length > 120) e.title = 'Опишите, что случилось (от 3 до 120 символов)'
  if (!form.description.trim()) e.description = 'Добавьте описание'
  return e
})
const err = (f: string) => serverErrors.value[f] ?? (submitted.value ? errors.value[f] : undefined)
const live = computed(() => !current.value || current.value.status === 'active' || current.value.status === 'extended')

async function pick(ev: Event) {
  const input = ev.target as HTMLInputElement
  const files = Array.from(input.files ?? []).slice(0, MAX_PHOTOS - form.photos.length)
  uploading.value = true
  try {
    for (const f of files) form.photos.push(await uploadFile(f, 'request_photo'))
  } catch (e) {
    serverErrors.value = { ...serverErrors.value, photoFileIds: parseProblem(e).message }
  } finally {
    uploading.value = false
    input.value = ''
  }
}

async function send() {
  submitted.value = true
  serverErrors.value = {}
  serverError.value = null
  if (Object.keys(errors.value).length || !contractId.value) return
  try {
    const r = await m.create.mutateAsync({
      contractId: contractId.value,
      category: form.category!,
      title: form.title.trim(),
      description: form.description.trim(),
      photoFileIds: form.photos.map((p) => p.id),
    })
    toast.add({ severity: 'success', summary: `Заявка ${r.number} отправлена`, life: 4000 })
    await router.replace(link(`/cabinet/requests/${r.id}`))
  } catch (e) {
    const p = parseProblem(e)
    serverErrors.value = p.fieldErrors
    if (!Object.keys(p.fieldErrors).length) serverError.value = p.message
  }
}
</script>

<template>
  <div>
    <RouterLink :to="link('/cabinet/requests')" class="back">
      <i class="pi pi-arrow-left" aria-hidden="true" /> Заявки
    </RouterLink>
    <h1 class="title">Сообщить о неисправности</h1>
    <Message v-if="!live" severity="info" :closable="false">
      Договор № {{ current?.number }} не действует — новые заявки по нему не принимаются.
    </Message>
    <form v-else class="form" novalidate @submit.prevent="send">
      <fieldset class="field">
        <legend class="label">Категория *</legend>
        <div class="tiles" role="radiogroup" aria-label="Категория">
          <button
            v-for="c in categories"
            :key="c"
            type="button"
            role="radio"
            class="tile"
            :class="{ 'tile--active': form.category === c }"
            :aria-checked="form.category === c"
            @click="form.category = c"
          >
            <i :class="icons[c]" aria-hidden="true" />
            <span>{{ requestCategoryLabels[c] }}</span>
          </button>
        </div>
        <small v-if="err('category')" class="error">{{ err('category') }}</small>
      </fieldset>
      <div class="field">
        <label for="cr-title">Что случилось *</label>
        <InputText id="cr-title" v-model="form.title" maxlength="120" :invalid="!!err('title')" fluid />
        <small v-if="err('title')" class="error">{{ err('title') }}</small>
      </div>
      <div class="field">
        <label for="cr-desc">Описание *</label>
        <Textarea
          id="cr-desc"
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
          <label v-if="form.photos.length < MAX_PHOTOS" class="p-button p-button-outlined add">
            <i class="pi pi-camera" aria-hidden="true" /> {{ uploading ? 'Загрузка…' : 'Добавить фото' }}
            <input type="file" accept="image/*" capture="environment" multiple hidden @change="pick" />
          </label>
        </div>
        <small v-if="err('photoFileIds')" class="error">{{ err('photoFileIds') }}</small>
      </div>
      <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
      <Button
        type="submit"
        label="Отправить"
        icon="pi pi-send"
        class="wide"
        :loading="m.create.isPending.value"
        :disabled="uploading"
      />
    </form>
  </div>
</template>

<style scoped>
.back {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  min-height: 44px;
}
.title {
  font-size: 1.25rem;
  margin: 0 0 0.75rem;
}
.form {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
  border: none;
  padding: 0;
  margin: 0;
  min-width: 0;
}
.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(96px, 1fr));
  gap: 0.5rem;
}
.tile {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.375rem;
  min-height: 72px;
  padding: 0.5rem;
  border: 1px solid var(--p-content-border-color);
  border-radius: 8px;
  background: var(--p-content-background);
  color: inherit;
  font: inherit;
  font-size: 0.875rem;
  cursor: pointer;
}
.tile i {
  font-size: 1.25rem;
  color: var(--p-primary-color);
}
.tile--active {
  border-color: var(--p-primary-color);
  outline: 2px solid var(--p-primary-color);
  outline-offset: -2px;
}
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
  min-height: 44px;
}
.error {
  color: var(--p-red-600);
}
.wide {
  width: 100%;
  min-height: 44px;
}
</style>
