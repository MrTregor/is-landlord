<script setup lang="ts">
// Выбор периода отчёта: диапазон дат и быстрые кнопки (14 Экраны собственника, раздел 12).
import DateInput from '@/components/DateInput.vue'
import { presetPeriod, presets, type Period } from '../period'
import { todayIso } from '@/utils/today'

const model = defineModel<Period>({ required: true })
</script>

<template>
  <div class="period">
    <div class="dates">
      <div class="date">
        <DateInput
          :model-value="model.from"
          input-id="rp-from"
          placeholder="с"
          @update:model-value="(v) => v && (model = { ...model, from: v })"
        />
      </div>
      <span aria-hidden="true">—</span>
      <div class="date">
        <DateInput
          :model-value="model.to"
          input-id="rp-to"
          placeholder="по"
          @update:model-value="(v) => v && (model = { ...model, to: v })"
        />
      </div>
    </div>
    <div class="presets">
      <Button
        v-for="p in presets"
        :key="p.key"
        :label="p.label"
        size="small"
        text
        @click="model = presetPeriod(p.key, todayIso())"
      />
    </div>
  </div>
</template>

<style scoped>
.period {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem 1rem;
}
.dates {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.date {
  width: 11rem;
}
.presets {
  display: flex;
  flex-wrap: wrap;
}
</style>
