<script setup lang="ts">
// Опись имущества — редактируемая таблица (14 Экраны собственника, раздел 2.3, вкладка 7).
import { nextTick, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import { fetchInventory, usePropertyMutations, usePropertySub } from '@/api/queries/properties'
import { parseProblem } from '@/utils/problem'
import { useSaveState } from '@/utils/saveState'

const props = defineProps<{ propertyId: string }>()
const inv = usePropertySub(() => props.propertyId, 'inventory', fetchInventory)
const m = usePropertyMutations(() => props.propertyId)
const toast = useToast()

interface Row {
  key: number
  id: string | null
  name: string
  quantity: number
  condition: string
  note: string
}
let seq = 0
const rows = ref<Row[]>([])
const dirty = ref(false)
const error = ref<string | null>(null)
// Ключи строк и id новых строк меняются при повторной загрузке — для состояния кнопки сравниваются только данные.
const saveState = useSaveState(
  () => rows.value.map((r) => [r.name, r.quantity, r.condition, r.note]),
  'Сохранить опись',
)

watch(
  () => inv.data.value,
  (d) => {
    if (!d) return
    rows.value = d.items.map((i) => ({
      key: ++seq,
      id: i.id,
      name: i.name,
      quantity: i.quantity,
      condition: i.condition,
      note: i.note ?? '',
    }))
    dirty.value = false
  },
  { immediate: true },
)

function add() {
  rows.value.push({ key: ++seq, id: null, name: '', quantity: 1, condition: 'исправно', note: '' })
  dirty.value = true
}
function remove(i: number) {
  rows.value.splice(i, 1)
  dirty.value = true
}
function move(i: number, d: number) {
  const j = i + d
  if (j < 0 || j >= rows.value.length) return
  const [r] = rows.value.splice(i, 1)
  if (r) rows.value.splice(j, 0, r)
  dirty.value = true
}

async function save() {
  error.value = null
  saveState.reset()
  const bad = rows.value.findIndex((r) => !r.name.trim() || !r.condition.trim() || r.quantity < 1)
  if (bad >= 0) {
    error.value = `Строка ${bad + 1}: укажите наименование, количество и состояние`
    return
  }
  try {
    await m.inventory.mutateAsync({
      items: rows.value.map((r) => ({
        id: r.id,
        name: r.name,
        quantity: r.quantity,
        condition: r.condition,
        note: r.note.trim() || null,
      })),
    })
    await nextTick()
    saveState.saved()
    toast.add({ severity: 'success', summary: 'Опись сохранена', life: 3000 })
  } catch (err) {
    error.value = parseProblem(err).message
    saveState.failed()
  }
}
</script>

<template>
  <div class="form">
    <Message severity="info" :closable="false">Опись копируется в акт приёма-передачи при активации договора.</Message>
    <table class="inv">
      <thead>
        <tr>
          <th scope="col">№</th>
          <th scope="col">Наименование *</th>
          <th scope="col">Кол-во *</th>
          <th scope="col">Состояние *</th>
          <th scope="col">Примечание</th>
          <th scope="col"><span class="sr-only">Действия</span></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(r, i) in rows" :key="r.key">
          <td>{{ i + 1 }}</td>
          <td>
            <InputText v-model="r.name" :aria-label="`Наименование, строка ${i + 1}`" fluid @input="dirty = true" />
          </td>
          <td class="inv__qty">
            <InputNumber
              v-model="r.quantity"
              :min="1"
              :aria-label="`Количество, строка ${i + 1}`"
              fluid
              @input="dirty = true"
            />
          </td>
          <td>
            <InputText v-model="r.condition" :aria-label="`Состояние, строка ${i + 1}`" fluid @input="dirty = true" />
          </td>
          <td>
            <InputText v-model="r.note" :aria-label="`Примечание, строка ${i + 1}`" fluid @input="dirty = true" />
          </td>
          <td class="inv__act">
            <Button
              icon="pi pi-arrow-up"
              text
              size="small"
              aria-label="Выше"
              :disabled="i === 0"
              @click="move(i, -1)"
            />
            <Button
              icon="pi pi-arrow-down"
              text
              size="small"
              aria-label="Ниже"
              :disabled="i === rows.length - 1"
              @click="move(i, 1)"
            />
            <Button
              icon="pi pi-times"
              text
              severity="danger"
              size="small"
              aria-label="Удалить строку"
              @click="remove(i)"
            />
          </td>
        </tr>
        <tr v-if="!rows.length">
          <td colspan="6" class="hint">Опись пуста</td>
        </tr>
      </tbody>
    </table>
    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
    <div class="actions">
      <Button label="Добавить строку" icon="pi pi-plus" outlined @click="add" />
      <Button
        :label="saveState.button.value.label"
        :icon="saveState.button.value.icon"
        :severity="saveState.button.value.severity"
        :disabled="!dirty && saveState.state.value === 'idle'"
        :loading="m.inventory.isPending.value"
        @click="save"
      />
    </div>
  </div>
</template>

<style scoped>
.inv {
  width: 100%;
  border-collapse: collapse;
}
.inv th,
.inv td {
  padding: 0.25rem;
  text-align: left;
  vertical-align: middle;
}
.inv th {
  font-weight: 600;
  border-bottom: 1px solid var(--p-content-border-color);
}
.inv__qty {
  width: 110px;
}
.inv__act {
  white-space: nowrap;
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
}
</style>
