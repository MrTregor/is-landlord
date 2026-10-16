<script setup lang="ts">
// Шаблон письма (14 Экраны собственника, раздел 16, вкладка 4; 10 Уведомления, раздел 1): Тема*, Текст (TipTap),
// «Вставить поле», «Предпросмотр», «Сбросить к стандартному», «Включён». Поля проверяются при сохранении.
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import type { Editor } from '@tiptap/vue-3'
import { useTemplateVariables } from '@/api/queries/documents'
import {
  useEmailTemplate,
  useEmailTemplateMutations,
  type EmailTemplate,
  type EmailTemplateCode,
} from '@/api/queries/notify'
import PageHeader from '@/components/PageHeader.vue'
import RichTextEditor from '@/components/RichTextEditor.vue'
import { formatDateTime } from '@/utils/date'
import { emailRecipientLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'

const route = useRoute()
const toast = useToast()
const confirm = useConfirm()
const code = computed(() => route.params.code as EmailTemplateCode)
const tpl = useEmailTemplate(code)
const vars = useTemplateVariables()
const m = useEmailTemplateMutations(code)

const subject = ref('')
const body = ref('')
const enabled = ref(true)
const loaded = ref<EmailTemplate | null>(null)
const editorKey = ref(0)
const errors = ref<Record<string, string>>({})
const error = ref<string | null>(null)

function load(t: EmailTemplate) {
  loaded.value = t
  subject.value = t.subject
  body.value = t.bodyHtml
  enabled.value = t.enabled
  editorKey.value++
}
watch(
  () => tpl.data.value,
  (t) => {
    if (t && t !== loaded.value) load(t)
  },
  { immediate: true },
)
const dirty = computed(
  () =>
    !!loaded.value &&
    (subject.value !== loaded.value.subject ||
      body.value !== loaded.value.bodyHtml ||
      enabled.value !== loaded.value.enabled),
)

const fieldOptions = computed(() =>
  (vars.data.value?.emails[code.value] ?? []).map((v) => ({ value: v.key, label: v.title, key: v.key })),
)
const placeholder = (key: string) => '{' + '{' + key + '}' + '}'
const field = ref<string | null>(null)
function insertField(editor: Editor, key: string | null) {
  if (!key) return
  editor.chain().focus().insertContent(placeholder(key)).run()
  field.value = null
}

function showError(err: unknown) {
  const p = parseProblem(err)
  errors.value = p.fieldErrors
  error.value = Object.keys(p.fieldErrors).length ? null : p.message
}

async function save() {
  errors.value = {}
  error.value = null
  if (!subject.value.trim()) {
    errors.value = { subject: 'Укажите тему письма' }
    return
  }
  try {
    load(await m.save.mutateAsync({ subject: subject.value.trim(), bodyHtml: body.value, enabled: enabled.value }))
    toast.add({ severity: 'success', summary: 'Шаблон сохранён', life: 3000 })
  } catch (err) {
    showError(err)
  }
}

function reset() {
  confirm.require({
    header: 'Сбросить к стандартному?',
    message: 'Тема и текст письма заменятся текстом по умолчанию. Признак «Включён» не изменится.',
    acceptProps: { label: 'Сбросить' },
    rejectProps: { label: 'Отмена', outlined: true },
    accept: async () => {
      errors.value = {}
      error.value = null
      try {
        load(await m.reset.mutateAsync())
        toast.add({ severity: 'success', summary: 'Восстановлен стандартный текст', life: 3000 })
      } catch (err) {
        showError(err)
      }
    },
  })
}

const previewVisible = ref(false)
const previewSubject = ref('')
const previewHtml = ref('')
async function preview() {
  errors.value = {}
  error.value = null
  try {
    const p = await m.preview.mutateAsync({ subject: subject.value, bodyHtml: body.value })
    previewSubject.value = p.subject
    previewHtml.value = p.html
    previewVisible.value = true
  } catch (err) {
    showError(err)
  }
}
</script>

<template>
  <div>
    <RouterLink to="/settings/email-templates" class="back"
      ><i class="pi pi-arrow-left" aria-hidden="true" /> Шаблоны писем</RouterLink
    >
    <PageHeader :title="tpl.data.value ? `Шаблон письма: ${tpl.data.value.name}` : 'Шаблон письма'">
      <template #actions>
        <Button label="Предпросмотр" icon="pi pi-eye" outlined :loading="m.preview.isPending.value" @click="preview" />
        <Button
          label="Сбросить к стандартному"
          icon="pi pi-replay"
          outlined
          :loading="m.reset.isPending.value"
          @click="reset"
        />
        <Button
          label="Сохранить"
          icon="pi pi-check"
          :disabled="!dirty"
          :loading="m.save.isPending.value"
          @click="save"
        />
      </template>
    </PageHeader>
    <Skeleton v-if="tpl.isPending.value" height="20rem" />
    <Message v-else-if="tpl.isError.value" severity="error" :closable="false">
      {{ parseProblem(tpl.error.value).message }}
    </Message>
    <div v-else-if="tpl.data.value" class="main">
      <p class="hint">
        {{ emailRecipientLabels[tpl.data.value.recipient] }} · изменён {{ formatDateTime(tpl.data.value.updatedAt) }}
      </p>
      <div class="field">
        <label for="et-subject">Тема *</label>
        <InputText id="et-subject" v-model="subject" maxlength="300" :invalid="!!errors.subject" fluid />
        <small v-if="errors.subject" class="error">{{ errors.subject }}</small>
      </div>
      <div class="field field--check">
        <ToggleSwitch v-model="enabled" input-id="et-enabled" />
        <label for="et-enabled">Включён</label>
        <small v-if="!enabled" class="hint">Выключенное письмо не отправляется — в журнале оно «Пропущено»</small>
      </div>
      <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
      <small v-if="errors.bodyHtml" class="error">{{ errors.bodyHtml }}</small>
      <span id="et-body" class="label">Текст *</span>
      <RichTextEditor :key="editorKey" v-model="body" labelled-by="et-body">
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
        >; <code v-pre>{{block:lines}}</code> — таблица, которую подставляет система. Шапка и подвал письма добавляются
        автоматически.
      </p>
    </div>

    <Dialog
      v-model:visible="previewVisible"
      modal
      header="Предпросмотр письма"
      :style="{ width: '720px' }"
      :breakpoints="{ '760px': '100vw' }"
    >
      <p><b>Тема:</b> {{ previewSubject }}</p>
      <iframe class="frame" title="Предпросмотр письма" sandbox="" :srcdoc="previewHtml" />
      <p class="hint">Значения полей — примеры; реквизиты организации — из настроек.</p>
    </Dialog>
  </div>
</template>

<style scoped>
.main {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}
.main :deep(.ProseMirror) {
  min-height: 16rem;
  max-height: 60vh;
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
  margin: 0;
}
.label {
  font-weight: 600;
}
.frame {
  width: 100%;
  height: 60vh;
  border: 1px solid var(--p-content-border-color);
  border-radius: 6px;
  background: #fff;
}
</style>
