<script setup lang="ts">
// Отчёты (14 Экраны собственника, раздел 12): карточки пяти отчётов с описанием в одну строку.
import PageHeader from '@/components/PageHeader.vue'
import { reports } from '../reports'
</script>

<template>
  <div>
    <PageHeader title="Отчёты" />
    <div class="cards">
      <RouterLink v-for="r in reports" :key="r.name" :to="`/reports/${r.name}`" class="card">
        <span class="card__title">{{ r.title }}</span>
        <span class="card__descr">{{ r.description }}</span>
      </RouterLink>
    </div>
  </div>
</template>

<style scoped>
.cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 1rem;
}
.card {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  padding: 1rem 1.25rem;
  border: 1px solid var(--p-content-border-color);
  border-radius: var(--p-content-border-radius);
  background: var(--p-content-background);
  color: inherit;
  text-decoration: none;
}
.card:hover,
.card:focus-visible {
  border-color: var(--p-primary-color);
}
.card__title {
  font-weight: 600;
  color: var(--p-primary-color);
}
.card__descr {
  color: var(--p-text-muted-color);
  font-size: 0.9rem;
}
</style>
