<script setup lang="ts">
// Карточка нанимателя (14 Экраны собственника, раздел 3.3).
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { documentPdfUrl, useDocumentMutations } from '@/api/queries/documents'
import { uploadFile } from '@/api/queries/properties'
import { useTenant, useTenantMutations } from '@/api/queries/tenants'
import type { components } from '@/api/schema'
import DateInput from '@/components/DateInput.vue'
import MoneyText from '@/components/MoneyText.vue'
import PageHeader from '@/components/PageHeader.vue'
import PdnField from '@/components/PdnField.vue'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate, formatDateTime } from '@/utils/date'
import {
  consentMethodLabels,
  contractStatusLabels,
  contractStatusSeverity,
  docTypeLabels,
  tenantKindLabels,
  userStatusLabels,
} from '@/utils/labels'
import { formatPhone } from '@/utils/phone'
import { openInNewTab } from '@/utils/pdf'
import { parseProblem } from '@/utils/problem'
import { today, todayIso } from '@/utils/today'

type Consent = components['schemas']['PdnConsentDTO']
type ContractItem = components['schemas']['TenantContractItem']

const route = useRoute()
const router = useRouter()
const toast = useToast()
const id = computed(() => route.params.id as string)
const tenant = useTenant(id)
const m = useTenantMutations(id)
const t = computed(() => tenant.data.value)

const confirm = useConfirm()
const hasLiveContracts = computed(() =>
  (t.value?.contracts ?? []).some((c) => c.status === 'draft' || c.status === 'active' || c.status === 'extended'),
)

/** Обезличивание (12 разд. 6) — необратимо, с подтверждением. */
function anonymize() {
  confirm.require({
    header: 'Обезличить нанимателя?',
    message: 'Персональные данные будут необратимо удалены, документы с ПДн — удалены. Денежные данные сохранятся.',
    icon: 'pi pi-exclamation-triangle',
    rejectProps: { label: 'Отмена', outlined: true },
    acceptProps: { label: 'Обезличить', severity: 'danger' },
    accept: async () => {
      try {
        await m.anonymize.mutateAsync()
        toast.add({ severity: 'success', summary: 'Наниматель обезличен', life: 3000 })
      } catch (err) {
        fail(err)
      }
    },
  })
}

function fail(err: unknown) {
  toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
}

// Раскрытие паспортных данных (пишется в журнал действий).
const revealed = ref<components['schemas']['TenantPdn'] | null>(null)
watch(id, () => (revealed.value = null))
async function reveal() {
  try {
    revealed.value = await m.reveal.mutateAsync()
  } catch (err) {
    fail(err)
  }
}

async function invite() {
  try {
    await m.invite.mutateAsync()
    toast.add({ severity: 'success', summary: 'Приглашение отправлено', life: 3000 })
  } catch (err) {
    fail(err)
  }
}
async function resend() {
  if (!t.value?.user) return
  try {
    await m.resendInvite.mutateAsync(t.value.user.id)
    toast.add({ severity: 'success', summary: 'Приглашение отправлено повторно', life: 3000 })
  } catch (err) {
    fail(err)
  }
}

// «Сформировать согласие» — бланк PDF; отметка о получении связывается с последним сформированным бланком.
const docs = useDocumentMutations()
const consentDocumentId = ref<string | null>(null)
watch(id, () => (consentDocumentId.value = null))
async function consentPdf() {
  try {
    await openInNewTab(async () => {
      const d = await docs.consentForm.mutateAsync(id.value)
      consentDocumentId.value = d.id
      return documentPdfUrl(d.id)
    })
  } catch (err) {
    fail(err)
  }
}

// «Отметить получение» подписанного согласия.
const consentDialog = ref(false)
const consentForm = reactive({ givenOn: todayIso() as string | null, file: null as File | null })
const consentError = ref<string | null>(null)
async function saveConsent() {
  consentError.value = null
  if (!consentForm.givenOn) {
    consentError.value = 'Укажите дату'
    return
  }
  try {
    const fileId = consentForm.file ? (await uploadFile(consentForm.file, 'consent_scan')).id : null
    await m.recordConsent.mutateAsync({ givenOn: consentForm.givenOn, fileId, documentId: consentDocumentId.value })
    consentDialog.value = false
    toast.add({ severity: 'success', summary: 'Согласие отмечено', life: 3000 })
  } catch (err) {
    consentError.value = parseProblem(err).message
  }
}

// «Отозвать» согласие.
const revokeDialog = ref(false)
const revokeForm = reactive({ consentId: '', revokedOn: todayIso() as string | null, note: '' })
const revokeError = ref<string | null>(null)
function openRevoke(c: Consent) {
  Object.assign(revokeForm, { consentId: c.id, revokedOn: todayIso(), note: '' })
  revokeError.value = null
  revokeDialog.value = true
}
async function saveRevoke() {
  if (!revokeForm.revokedOn) {
    revokeError.value = 'Укажите дату'
    return
  }
  try {
    await m.revokeConsent.mutateAsync({
      consentId: revokeForm.consentId,
      body: { revokedOn: revokeForm.revokedOn, note: revokeForm.note.trim() || null },
    })
    revokeDialog.value = false
    toast.add({ severity: 'success', summary: 'Согласие отозвано', life: 3000 })
  } catch (err) {
    revokeError.value = parseProblem(err).message
  }
}

const canInvite = computed(() => !!t.value && !t.value.user && !!t.value.email && !t.value.anonymizedAt)
</script>

<template>
  <div>
    <Skeleton v-if="tenant.isPending.value" height="10rem" />
    <Message v-else-if="tenant.isError.value" severity="error" :closable="false">
      {{ parseProblem(tenant.error.value).message }}
    </Message>
    <template v-else-if="t">
      <PageHeader :title="t.displayName" :subtitle="tenantKindLabels[t.kind]">
        <template #actions>
          <Button
            label="Изменить"
            icon="pi pi-pencil"
            outlined
            :disabled="!!t.anonymizedAt"
            @click="router.push(`/tenants/${t.id}/edit`)"
          />
          <Button
            v-if="canInvite"
            label="Пригласить в кабинет"
            icon="pi pi-send"
            :loading="m.invite.isPending.value"
            @click="invite"
          />
          <Button
            v-else-if="t.user?.status === 'invited'"
            label="Отправить приглашение повторно"
            icon="pi pi-send"
            outlined
            :loading="m.resendInvite.isPending.value"
            @click="resend"
          />
          <Button
            v-if="!t.anonymizedAt"
            label="Обезличить"
            icon="pi pi-eraser"
            severity="danger"
            outlined
            :disabled="hasLiveContracts"
            :title="hasLiveContracts ? 'Только без договоров «Черновик», «Действует», «Продлён»' : undefined"
            :loading="m.anonymize.isPending.value"
            @click="anonymize"
          />
        </template>
      </PageHeader>
      <Message v-if="t.anonymizedAt" severity="warn" :closable="false">
        Наниматель обезличен {{ formatDateTime(t.anonymizedAt) }}.
      </Message>

      <div class="blocks">
        <Card>
          <template #title>Контакты</template>
          <template #content>
            <dl class="props">
              <dt>Телефон</dt>
              <dd>{{ formatPhone(t.phone) || '—' }}</dd>
              <dt>E-mail</dt>
              <dd>{{ t.email ?? '—' }}</dd>
              <dt>Кабинет</dt>
              <dd>
                <template v-if="t.user">
                  {{ userStatusLabels[t.user.status] }}
                  <span v-if="t.user.lastLoginAt" class="hint">· вход {{ formatDateTime(t.user.lastLoginAt) }}</span>
                </template>
                <span v-else class="hint">нет учётной записи</span>
              </dd>
              <template v-if="t.kind === 'individual'">
                <dt>Дата рождения</dt>
                <dd>{{ formatDate(t.birthDate) || '—' }}</dd>
              </template>
              <template v-else>
                <dt>ИНН / КПП</dt>
                <dd>
                  {{ t.inn }}<template v-if="t.kpp"> / {{ t.kpp }}</template>
                </dd>
                <dt>ОГРН</dt>
                <dd>{{ t.ogrn ?? '—' }}</dd>
                <dt>Юридический адрес</dt>
                <dd>{{ t.legalAddress }}</dd>
                <dt>Подписант</dt>
                <dd>{{ t.signatoryPosition }} {{ t.signatoryName }}, на основании {{ t.signatoryBasis }}</dd>
              </template>
              <template v-if="t.note">
                <dt>Примечание</dt>
                <dd>{{ t.note }}</dd>
              </template>
            </dl>
          </template>
        </Card>

        <Card v-if="t.kind === 'individual'">
          <template #title>Документ</template>
          <template #content>
            <dl class="props">
              <dt>Вид</dt>
              <dd>{{ t.docType ? docTypeLabels[t.docType] : '—' }}</dd>
              <dt>Серия и номер</dt>
              <dd>
                <PdnField
                  :masked="t.docNumberMasked"
                  :revealed="revealed?.docNumber"
                  :loading="m.reveal.isPending.value"
                  @reveal="reveal"
                />
              </dd>
              <template v-if="revealed">
                <dt>Кем выдан</dt>
                <dd>{{ revealed.docIssuedBy ?? '—' }}</dd>
                <dt>Код подразделения</dt>
                <dd>{{ revealed.docDivisionCode ?? '—' }}</dd>
              </template>
              <dt>Дата выдачи</dt>
              <dd>{{ formatDate(t.docIssuedOn) || '—' }}</dd>
              <dt>Адрес регистрации</dt>
              <dd>
                <PdnField
                  :masked="t.registrationAddressMasked"
                  :revealed="revealed?.registrationAddress"
                  :loading="m.reveal.isPending.value"
                  @reveal="reveal"
                />
              </dd>
            </dl>
            <Message v-if="!t.hasDocument" severity="warn" :closable="false">
              Для договора найма нужны документ и адрес регистрации.
            </Message>
          </template>
        </Card>

        <Card>
          <template #title>Долг и залоги</template>
          <template #content>
            <MoneyText :kop="t.debtKop" />
            <p class="hint">Суммы по договорам появятся вместе с начислениями и платежами.</p>
          </template>
        </Card>
      </div>

      <Card v-if="t.kind === 'individual'" class="section">
        <template #title>Отдельное согласие на ПДн</template>
        <template #content>
          <DataTable :value="t.consents" size="small" data-key="id">
            <template #empty>Согласий нет — для договора оно не требуется</template>
            <Column header="Цель">
              <template #body="{ data }">{{ (data as Consent).purpose }}</template>
            </Column>
            <Column header="Дата">
              <template #body="{ data }">{{ formatDate((data as Consent).givenOn) }}</template>
            </Column>
            <Column header="Способ">
              <template #body="{ data }">{{ consentMethodLabels[(data as Consent).method] }}</template>
            </Column>
            <Column header="Скан">
              <template #body="{ data }">
                <a v-if="(data as Consent).fileUrl" :href="(data as Consent).fileUrl!" target="_blank" rel="noopener"
                  >Открыть</a
                >
                <span v-else class="hint">—</span>
              </template>
            </Column>
            <Column header="Отозвано">
              <template #body="{ data }">
                <template v-if="(data as Consent).revokedOn">
                  {{ formatDate((data as Consent).revokedOn) }}
                  <span v-if="(data as Consent).revokeNote" class="hint">· {{ (data as Consent).revokeNote }}</span>
                </template>
                <Button v-else text size="small" label="Отозвать" @click="openRevoke(data as Consent)" />
              </template>
            </Column>
          </DataTable>
          <div class="actions">
            <Button
              label="Сформировать согласие"
              icon="pi pi-file-pdf"
              outlined
              :disabled="!!t.anonymizedAt"
              :loading="docs.consentForm.isPending.value"
              @click="consentPdf"
            />
            <Button
              label="Отметить получение"
              icon="pi pi-check"
              :disabled="!!t.anonymizedAt"
              @click="
                () => {
                  consentForm.givenOn = todayIso()
                  consentForm.file = null
                  consentError = null
                  consentDialog = true
                }
              "
            />
          </div>
        </template>
      </Card>

      <Card class="section">
        <template #title>Договоры</template>
        <template #content>
          <DataTable :value="t.contracts" size="small" data-key="id">
            <template #empty>Договоров нет</template>
            <Column header="Номер">
              <template #body="{ data }">
                <RouterLink :to="`/contracts/${(data as ContractItem).id}`"
                  >№ {{ (data as ContractItem).number }}</RouterLink
                >
              </template>
            </Column>
            <Column field="propertyCode" header="Объект" />
            <Column header="Срок">
              <template #body="{ data }">
                {{ formatDate((data as ContractItem).startDate) }} – {{ formatDate((data as ContractItem).endDate) }}
              </template>
            </Column>
            <Column header="Статус">
              <template #body="{ data }">
                <StatusTag
                  :label="contractStatusLabels[(data as ContractItem).status]"
                  :severity="contractStatusSeverity[(data as ContractItem).status]"
                />
              </template>
            </Column>
          </DataTable>
        </template>
      </Card>

      <Dialog
        v-model:visible="consentDialog"
        modal
        header="Отметить получение согласия"
        :style="{ width: '480px' }"
        :breakpoints="{ '575px': '100vw' }"
      >
        <form class="form" novalidate @submit.prevent="saveConsent">
          <div class="field">
            <label for="c-date">Дата согласия *</label>
            <DateInput v-model="consentForm.givenOn" input-id="c-date" :max-date="today()" />
          </div>
          <div class="field">
            <label for="c-file">Скан подписанного бланка</label>
            <input
              id="c-file"
              type="file"
              accept="application/pdf,image/jpeg,image/png,image/webp"
              @change="(e) => (consentForm.file = (e.target as HTMLInputElement).files?.[0] ?? null)"
            />
          </div>
          <Message v-if="consentError" severity="error" :closable="false">{{ consentError }}</Message>
          <div class="actions">
            <Button type="button" label="Отмена" outlined @click="consentDialog = false" />
            <Button type="submit" label="Сохранить" :loading="m.recordConsent.isPending.value" />
          </div>
        </form>
      </Dialog>

      <Dialog
        v-model:visible="revokeDialog"
        modal
        header="Отзыв согласия"
        :style="{ width: '480px' }"
        :breakpoints="{ '575px': '100vw' }"
      >
        <form class="form" novalidate @submit.prevent="saveRevoke">
          <div class="field">
            <label for="r-date">Дата отзыва *</label>
            <DateInput v-model="revokeForm.revokedOn" input-id="r-date" :max-date="today()" />
          </div>
          <div class="field">
            <label for="r-note">Примечание</label>
            <InputText id="r-note" v-model="revokeForm.note" placeholder="например, заявление от …" fluid />
          </div>
          <p class="hint">После отзыва данные для этой цели не используются.</p>
          <Message v-if="revokeError" severity="error" :closable="false">{{ revokeError }}</Message>
          <div class="actions">
            <Button type="button" label="Отмена" outlined @click="revokeDialog = false" />
            <Button type="submit" label="Отозвать" severity="danger" :loading="m.revokeConsent.isPending.value" />
          </div>
        </form>
      </Dialog>
    </template>
  </div>
</template>

<style scoped>
.blocks {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: 1rem;
}
.section {
  margin-top: 1rem;
}
.props {
  display: grid;
  grid-template-columns: minmax(130px, 170px) 1fr;
  gap: 0.375rem 1rem;
  margin: 0;
}
.props dt {
  color: var(--p-text-muted-color);
}
.props dd {
  margin: 0;
}
</style>
