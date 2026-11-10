<template>
  <span class="tabular-nums" :style="colorStyle" :title="t('monitorCommon.cacheRate24hHint')">{{ splitUnit ? label.replace(/%$/, '') : label }}<span v-if="splitUnit && label.endsWith('%')" class="ml-0.5 text-base font-semibold">%</span></span>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { hslForPct } from '@/composables/useChannelMonitorFormat'
const props = defineProps<{ value?: number | null; splitUnit?: boolean; colored?: boolean }>()
const { t } = useI18n()
const colorStyle = computed(() => {
  if (!props.colored) return undefined
  const rate = props.value
  const valid = rate != null && Number.isFinite(rate) && rate >= 0 && rate <= 1
  return { color: valid ? hslForPct(rate * 100) : 'rgb(156 163 175)' }
})
const label = computed(() => {
  const rate = props.value
  if (rate == null || !Number.isFinite(rate) || rate < 0 || rate > 1) return '—'
  if (rate === 0) return '0%'
  if (rate >= 0.9995) return '100%'
  if (rate < 0.0005) return '<0.1%'
  return `${(rate * 100).toFixed(1)}%`
})
</script>
