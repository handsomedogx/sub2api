<template>
  <section class="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-600" data-test="cyber-evidence">
    <h3 class="font-semibold">{{ t('admin.riskControl.evidence.title') }}</h3>
    <p class="text-sm text-gray-500">{{ t('admin.riskControl.evidence.scope') }}</p>
    <p class="text-sm text-amber-700 dark:text-amber-300">{{ t('admin.riskControl.evidence.score') }}</p>
    <p v-if="busy" role="status" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
    <button v-if="!autoLoad || !busy" type="button" class="btn btn-secondary" :disabled="busy" @click="load">{{ t(autoLoad ? 'common.refresh' : 'admin.riskControl.evidence.load') }}</button>
    <p v-if="error" role="alert">{{ error }}</p>
    <template v-if="record">
      <p class="break-all text-sm">UID {{ record.user_id ?? '-' }} · {{ record.request_id || '-' }} · {{ record.occurred_at }}</p>
      <p class="text-sm">{{ t('admin.riskControl.evidence.state') }}: {{ statusText(record.status) }}</p>
      <p v-if="!record.payload" class="text-sm">{{ t('admin.riskControl.evidence.noHistory') }}</p>
      <template v-if="!record.payload && fallbackReason">
        <h4 class="font-medium">{{ t('admin.riskControl.evidence.reason') }}</h4>
        <pre class="max-h-40 overflow-auto whitespace-pre-wrap break-words text-sm">{{ fallbackReason }}</pre>
      </template>
      <template v-if="record.payload">
        <div v-if="record.payload.body_retention_version" class="rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-900" data-testid="evidence-retained-body">
          <p>{{ t('admin.riskControl.evidence.retainedRequest', { size: formatBytes(record.payload.request_body_bytes ?? 0) }) }}</p>
          <p v-if="record.payload.request_omission" class="text-amber-700 dark:text-amber-300">{{ statusText(record.payload.request_omission) }}</p>
          <p>{{ t('admin.riskControl.evidence.retainedResponse', { size: formatBytes(record.payload.response_bytes ?? 0) }) }}</p>
          <p v-if="record.payload.response_omission" class="text-amber-700 dark:text-amber-300">{{ statusText(record.payload.response_omission) }}</p>
          <p class="mt-1 text-gray-500">{{ t('admin.riskControl.evidence.retainedHint') }}</p>
        </div>
        <p class="text-sm">{{ record.payload.redaction_version }} · {{ record.payload.source }} · HTTP {{ record.payload.upstream_http_status ?? '-' }} · {{ record.payload.original_bytes }} bytes<span v-if="record.payload.input"> · {{ t(record.payload.truncated ? 'admin.riskControl.evidence.truncated' : 'admin.riskControl.evidence.complete') }}</span></p>
        <p v-if="record.payload.scanned_bytes !== undefined" class="text-sm" data-testid="evidence-coverage">{{ t('admin.riskControl.evidence.coverage', { original: formatBytes(record.payload.original_bytes), scanned: formatBytes(record.payload.scanned_bytes), stored: formatBytes(record.payload.stored_bytes ?? 0), kept: record.payload.messages_retained ?? 0, omitted: record.payload.messages_omitted ?? 0 }) }}</p>
        <p v-if="record.payload.truncated" class="text-sm text-amber-700 dark:text-amber-300">{{ t('admin.riskControl.evidence.partialContext') }}</p>
        <p v-if="record.payload.omission" class="text-sm text-amber-700 dark:text-amber-300">{{ statusText(record.payload.omission) }}</p>
        <p v-if="record.payload.omitted_fields?.length" class="text-sm">{{ t('admin.riskControl.evidence.omittedFields', { fields: record.payload.omitted_fields.join(', ') }) }}</p>
        <p class="text-sm">{{ t('admin.riskControl.evidence.expires') }}: {{ record.expires_at || '-' }}</p>
        <h4 class="font-medium">{{ t('admin.riskControl.evidence.input') }}</h4>
        <pre v-if="record.payload.input" class="max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-gray-950 p-4 text-sm text-gray-100">{{ formattedInput }}</pre>
        <p v-else class="text-sm text-gray-500">{{ t(record.payload.request_body_bytes ? 'admin.riskControl.evidence.previewUnavailable' : 'admin.riskControl.evidence.metadataOnly') }}</p>
        <h4 class="font-medium">{{ t('admin.riskControl.evidence.reason') }}</h4>
        <pre class="max-h-40 overflow-auto whitespace-pre-wrap break-words text-sm">{{ record.payload.upstream_reason || '-' }}</pre>
        <p class="text-xs text-gray-500">{{ t('admin.riskControl.evidence.redaction') }}</p>
      </template>
      <div class="flex gap-2">
        <button v-if="record.payload" type="button" class="btn btn-secondary" :disabled="busy" @click="download">{{ t(record.payload.body_retention_version ? 'admin.riskControl.evidence.exportRetained' : 'admin.riskControl.evidence.export') }}</button>
        <button type="button" class="btn btn-secondary" :disabled="busy || record.status === 'deleted'" @click="remove">{{ t('admin.riskControl.evidence.delete') }}</button>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { deleteEvidence, exportEvidence, getEvidence, type CyberEvidence } from '@/api/admin/cyberEvidence'
const props = defineProps<{ logId: number; autoLoad?: boolean; fallbackReason?: string }>()
const { t, te } = useI18n()
const record = ref<CyberEvidence | null>(null)
const busy = ref(false)
const error = ref('')
function formatBytes(value: number) { return value < 1024 ? `${value} B` : value < 1024 * 1024 ? `${(value / 1024).toFixed(1)} KiB` : `${(value / (1024 * 1024)).toFixed(1)} MiB` }
let requestVersion = 0
watch(() => [props.logId, props.autoLoad] as const, () => {
  requestVersion++
  record.value = null; error.value = ''; busy.value = false
  if (props.autoLoad) void load()
}, { immediate: true })
onUnmounted(() => { requestVersion++ })
const formattedInput = computed(() => {
  const text = record.value?.payload?.input || ''
  try { return JSON.stringify(JSON.parse(text), null, 2) } catch { return text }
})
function statusText(state: string) {
  const key = `admin.riskControl.evidence.status.${state}`
  return te(key) ? t(key) : t('admin.riskControl.evidence.status.unavailable')
}
async function load() {
  busy.value = true; error.value = ''
  const id = props.logId
  const version = ++requestVersion
  try { const value = await getEvidence(id); if (version === requestVersion) record.value = value }
  catch { if (version === requestVersion) error.value = t('admin.riskControl.evidence.failed') }
  finally { if (version === requestVersion) busy.value = false }
}
async function download() {
  busy.value = true; error.value = ''
  const id = props.logId, version = ++requestVersion
  try {
    const blob = await exportEvidence(id)
    if (version !== requestVersion) return
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a'); link.href = url; link.download = `cyber-evidence-${id}.json`; link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  } catch { if (version === requestVersion) error.value = t('admin.riskControl.evidence.failed') }
  finally { if (version === requestVersion) busy.value = false }
}
async function remove() {
  busy.value = true; error.value = ''
  const id = props.logId
  const version = ++requestVersion
  try { await deleteEvidence(id); if (version === requestVersion) record.value = { ...record.value!, status: 'deleted', payload: undefined } }
  catch { if (version === requestVersion) error.value = t('admin.riskControl.evidence.failed') }
  finally { if (version === requestVersion) busy.value = false }
}
</script>
