<script setup lang="ts">
// Обзор объекта: характеристики, фото (загрузка, порядок перетаскиванием, удаление), показатели с начала года.
import { ref } from 'vue'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { uploadFile, usePropertyMutations, type PropertyDetail } from '@/api/queries/properties'
import MoneyText from '@/components/MoneyText.vue'
import { fiscalModeLabels, propertyKindLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import { areaToText } from '../schemas'

const props = defineProps<{ property: PropertyDetail }>()
const m = usePropertyMutations(() => props.property.id)
const toast = useToast()
const confirm = useConfirm()

const rows = [
  { label: 'Вид', value: (p: PropertyDetail) => propertyKindLabels[p.kind] },
  { label: 'Адрес', value: (p: PropertyDetail) => p.address },
  { label: 'Площадь', value: (p: PropertyDetail) => `${areaToText(p.areaSqm)} м²` },
  { label: 'Этаж', value: (p: PropertyDetail) => (p.floor ?? '—').toString() },
  {
    label: 'Комнат',
    value: (p: PropertyDetail) => (p.rooms === null ? '—' : p.rooms === 0 ? 'студия' : String(p.rooms)),
  },
  { label: 'Кадастровый номер', value: (p: PropertyDetail) => p.cadastralNumber ?? '—' },
  { label: 'Основание права', value: (p: PropertyDetail) => p.ownershipBasis ?? '—' },
  { label: 'С мебелью', value: (p: PropertyDetail) => (p.furnished ? 'да' : 'нет') },
  { label: 'Режим чека', value: (p: PropertyDetail) => fiscalModeLabels[p.fiscalMode] },
  { label: 'Описание', value: (p: PropertyDetail) => p.description ?? '—' },
]

const uploading = ref(false)
async function onUpload(e: { files: File | File[] }) {
  const files = Array.isArray(e.files) ? e.files : [e.files]
  uploading.value = true
  try {
    for (const f of files) {
      const dto = await uploadFile(f, 'property_photo')
      await m.addPhoto.mutateAsync({ fileId: dto.id, caption: null })
    }
    toast.add({ severity: 'success', summary: 'Фото добавлены', life: 3000 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Ошибка загрузки', detail: parseProblem(err).message, life: 6000 })
  } finally {
    uploading.value = false
  }
}

function remove(fileId: string) {
  confirm.require({
    header: 'Удалить фото?',
    message: 'Фото будет удалено из карточки объекта.',
    acceptLabel: 'Удалить',
    rejectLabel: 'Отмена',
    acceptProps: { severity: 'danger' },
    accept: async () => {
      try {
        await m.deletePhoto.mutateAsync(fileId)
      } catch (err) {
        toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
      }
    },
  })
}

// Порядок фото перетаскиванием.
const dragIndex = ref<number | null>(null)
async function drop(target: number) {
  const from = dragIndex.value
  dragIndex.value = null
  if (from === null || from === target) return
  const ids = props.property.photos.map((p) => p.fileId)
  const [moved] = ids.splice(from, 1)
  if (!moved) return
  ids.splice(target, 0, moved)
  try {
    await m.reorderPhotos.mutateAsync(ids)
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
  }
}
</script>

<template>
  <div class="overview">
    <dl class="props">
      <template v-for="r in rows" :key="r.label">
        <dt>{{ r.label }}</dt>
        <dd>{{ r.value(property) }}</dd>
      </template>
    </dl>

    <section>
      <h3>Показатели с начала года</h3>
      <div class="stats">
        <div><span class="hint">Поступления</span><MoneyText :kop="property.stats.incomeYtdKop" /></div>
        <div><span class="hint">Расходы</span><MoneyText :kop="property.stats.expensesYtdKop" /></div>
        <div>
          <span class="hint">Заполняемость</span>
          <span>{{
            property.stats.occupancyYtdPct ? property.stats.occupancyYtdPct.replace('.', ',') + ' %' : '—'
          }}</span>
        </div>
        <div>
          <span class="hint">Открытые заявки</span><span>{{ property.stats.openRequests }}</span>
        </div>
      </div>
      <small class="hint">Показатели заполняются по мере ведения платежей, затрат и заявок.</small>
    </section>

    <section>
      <h3>Фото</h3>
      <div class="photos">
        <figure
          v-for="(ph, i) in property.photos"
          :key="ph.fileId"
          class="photo"
          draggable="true"
          :class="{ 'photo--drag': dragIndex === i }"
          @dragstart="dragIndex = i"
          @dragover.prevent
          @drop="drop(i)"
        >
          <Image :src="ph.url" :alt="ph.caption ?? `Фото ${i + 1}`" preview image-class="photo__img" />
          <Button
            icon="pi pi-trash"
            severity="danger"
            text
            rounded
            size="small"
            class="photo__del"
            aria-label="Удалить фото"
            @click="remove(ph.fileId)"
          />
        </figure>
      </div>
      <FileUpload
        mode="basic"
        accept="image/jpeg,image/png,image/webp"
        multiple
        custom-upload
        auto
        choose-label="Добавить фото"
        :disabled="uploading"
        @uploader="onUpload"
      />
      <small class="hint">JPEG, PNG или WebP. Порядок меняется перетаскиванием.</small>
    </section>
  </div>
</template>

<style scoped>
.overview {
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
}
.props {
  display: grid;
  grid-template-columns: minmax(160px, 220px) 1fr;
  gap: 0.375rem 1rem;
  margin: 0;
}
.props dt {
  color: var(--p-text-muted-color);
}
.props dd {
  margin: 0;
}
h3 {
  margin: 0 0 0.5rem;
  font-size: 1rem;
}
.stats {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 0.75rem;
}
.stats > div {
  display: flex;
  flex-direction: column;
}
.photos {
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem;
  margin-bottom: 0.75rem;
}
.photo {
  position: relative;
  margin: 0;
  cursor: grab;
}
.photo--drag {
  opacity: 0.5;
}
.photo :deep(.photo__img) {
  width: 160px;
  height: 120px;
  object-fit: cover;
  border-radius: var(--p-border-radius-md);
}
.photo__del {
  position: absolute;
  top: 2px;
  right: 2px;
  background: rgb(255 255 255 / 85%);
}
</style>
