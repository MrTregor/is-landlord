<script setup lang="ts">
// Шаблон документа (14 Экраны собственника, раздел 16, вкладка 5; 09 Документы и шаблоны, разд. 2):
// TipTap во всю ширину, «Вставить поле», «Предпросмотр PDF», «Сохранить» (новая версия), «Сбросить к стандартному»,
// список версий справа; старая версия открывается только для чтения.
import { computed, nextTick, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import type { Editor } from '@tiptap/vue-3'
import {
  fetchTemplateVersion,
  useDocumentTemplate,
  useTemplateMutations,
  useTemplateVariables,
  type DocumentKind,
  type TemplateVersion,
} from '@/api/queries/documents'
import PageHeader from '@/components/PageHeader.vue'
import RichTextEditor from '@/components/RichTextEditor.vue'
import { formatDateTime } from '@/utils/date'
import { documentKindLabels } from '@/utils/labels'
import { openBlobInNewTab } from '@/utils/pdf'
import { parseProblem } from '@/utils/problem'
import { useSaveState } from '@/utils/saveState'

const route = useRoute()
const toast = useToast()
const confirm = useConfirm()
const kind = computed(() => route.params.kind as DocumentKind)
const tpl = useDocumentTemplate(kind)
const vars = useTemplateVariables()
const m = useTemplateMutations(kind)

const name = ref('')
const body = ref('')
/** Загруженный текст активной версии — для признака несохранённых изменений. */
const loaded = ref('')
const viewing = ref<TemplateVersion | null>(null)
const error = ref<string | null>(null)
const editorKey = ref(0)
const saveState = useSaveState(() => [name.value, body.value])

function load(v: TemplateVersion) {
  name.value = v.name
  body.value = v.bodyHtml
  loaded.value = v.bodyHtml
  editorKey.value++
}
watch(
  () => tpl.data.value,
  (d) => {
    if (d && !viewing.value) load(d.active)
  },
  { immediate: true },
)

const dirty = computed(
  () => !viewing.value && (body.value !== loaded.value || name.value !== tpl.data.value?.active.name),
)

const fieldOptions = computed(() =>
  (vars.data.value?.documents[kind.value] ?? []).map((v) => ({
    value: v.key,
    label: v.key.startsWith('block:') ? `Таблица: ${v.title.replace(/^Таблица: /, '')}` : v.title,
    key: v.key,
  })),
)
const placeholder = (key: string) => '{' + '{' + key + '}' + '}'
const field = ref<string | null>(null)
function insertField(editor: Editor, key: string | null) {
  if (!key) return
  editor.chain().focus().insertContent(`{{${key}}}`).run()
  field.value = null
}

async function save() {
  error.value = null
  saveState.reset()
  if (!name.value.trim()) {
    error.value = 'Укажите название'
    return
  }
  try {
    const d = await m.save.mutateAsync({ name: name.value.trim(), bodyHtml: body.value })
    load(d.active)
    await nextTick()
    saveState.saved()
    toast.add({ severity: 'success', summary: `Сохранена версия ${d.active.version}`, life: 3000 })
  } catch (err) {
    error.value = parseProblem(err).message
    saveState.failed()
  }
}

function reset() {
  confirm.require({
    header: 'Сбросить к стандартному?',
    message: 'Будет создана новая версия с текстом системного шаблона. Прежние версии сохранятся.',
    acceptProps: { label: 'Сбросить' },
    rejectProps: { label: 'Отмена', outlined: true },
    accept: async () => {
      try {
        const d = await m.reset.mutateAsync()
        viewing.value = null
        load(d.active)
        toast.add({ severity: 'success', summary: `Создана версия ${d.active.version}`, life: 3000 })
      } catch (err) {
        error.value = parseProblem(err).message
      }
    },
  })
}

async function preview() {
  error.value = null
  try {
    await openBlobInNewTab(() => m.preview.mutateAsync(body.value))
  } catch (err) {
    error.value = parseProblem(err).message
  }
}

async function openVersion(id: string) {
  error.value = null
  if (id === tpl.data.value?.active.id) {
    backToCurrent()
    return
  }
  try {
    const v = await fetchTemplateVersion(kind.value, id)
    viewing.value = v
    body.value = v.bodyHtml
    editorKey.value++
  } catch (err) {
    error.value = parseProblem(err).message
  }
}
function backToCurrent() {
  viewing.value = null
  if (tpl.data.value) load(tpl.data.value.active)
}
</script>

<template>
  <div>
    <RouterLink to="/settings/document-templates" class="back"
      ><i class="pi pi-arrow-left" aria-hidden="true" /> Шаблоны документов</RouterLink
    >
    <PageHeader :title="`Шаблон: ${documentKindLabels[kind] ?? kind}`">
      <template #actions>
        <Button
          label="Предпросмотр PDF"
          icon="pi pi-eye"
          outlined
          :loading="m.preview.isPending.value"
          @click="preview"
        />
        <Button
          label="Сбросить к стандартному"
          icon="pi pi-replay"
          outlined
          :disabled="!!viewing"
          :loading="m.reset.isPending.value"
          @click="reset"
        />
        <Button
          :label="saveState.button.value.label"
          :icon="saveState.button.value.icon"
          :severity="saveState.button.value.severity"
          :disabled="!!viewing || (!dirty && saveState.state.value === 'idle')"
          :loading="m.save.isPending.value"
          @click="save"
        />
      </template>
    </PageHeader>
    <Skeleton v-if="tpl.isPending.value" height="20rem" />
    <Message v-else-if="tpl.isError.value" severity="error" :closable="false">
      {{ parseProblem(tpl.error.value).message }}
    </Message>
    <div v-else-if="tpl.data.value" class="layout">
      <div class="main">
        <Message v-if="viewing" severity="info" :closable="false">
          Версия {{ viewing.version }} — только просмотр.
          <Button label="Вернуться к текущей" size="small" text @click="backToCurrent" />
        </Message>
        <div v-else class="field">
          <label for="tpl-name">Название *</label>
          <InputText id="tpl-name" v-model="name" maxlength="200" fluid />
        </div>
        <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
        <RichTextEditor :key="editorKey" v-model="body" document :readonly="!!viewing" labelled-by="tpl-name">
          <template #toolbar="{ editor }">
            <Select
              v-model="field"
              :options="fieldOptions"
              option-label="label"
              option-value="value"
              placeholder="Вставить поле"
              filter
              size="small"
              class="insert"
              aria-label="Вставить поле"
              @change="(e) => insertField(editor, e.value as string)"
            >
              <template #option="{ option }">
                <div>
                  <div>{{ option.label }}</div>
                  <code class="hint">{{ placeholder(option.key) }}</code>
                </div>
              </template>
            </Select>
          </template>
        </RichTextEditor>
        <p class="hint">
          Поля вставляются как <code v-pre>{{ ключ }}</code
          >; пустое значение в документе печатается линией для заполнения от руки. Таблицы
          <code v-pre>{{block:…}}</code> подставляются системой целиком.
        </p>
      </div>
      <aside class="versions">
        <h3>Версии</h3>
        <ul>
          <li v-for="v in tpl.data.value.versions" :key="v.id">
            <button
              type="button"
              class="version"
              :class="{
                'version--on': viewing ? viewing.id === v.id : v.id === tpl.data.value.active.id,
              }"
              @click="openVersion(v.id)"
            >
              <b>Версия {{ v.version }}</b>
              <span v-if="v.id === tpl.data.value.active.id"> · текущая</span>
              <span class="hint">{{ formatDateTime(v.createdAt) }} · {{ v.createdBy ?? 'стандартный' }}</span>
            </button>
          </li>
        </ul>
      </aside>
    </div>
  </div>
</template>

<style scoped>
.layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 240px;
  gap: 1rem;
}
@media (max-width: 900px) {
  .layout {
    grid-template-columns: 1fr;
  }
}
.main {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}
.main :deep(.ProseMirror) {
  max-height: 70vh;
  min-height: 50vh;
}
.back {
  display: inline-block;
  margin-bottom: 0.5rem;
}
.insert {
  min-width: 14rem;
}
.hint {
  color: var(--p-text-muted-color);
  font-size: 0.85rem;
}
.versions h3 {
  margin: 0 0 0.5rem;
  font-size: 1rem;
}
.versions ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}
.version {
  width: 100%;
  text-align: left;
  display: flex;
  flex-direction: column;
  gap: 0.15rem;
  padding: 0.5rem 0.75rem;
  border: 1px solid var(--p-content-border-color);
  border-radius: 6px;
  background: var(--p-content-background);
  color: inherit;
  cursor: pointer;
  font: inherit;
}
.version--on {
  border-color: var(--p-primary-color);
}
</style>
