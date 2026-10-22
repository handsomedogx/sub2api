<template>
  <BaseDialog :show="show" :title="t('admin.channelMonitor.order.title')" width="normal" @close="close">
    <p class="mb-4 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.channelMonitor.order.hint') }}</p>
    <p v-if="error" role="alert" class="mb-4 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
    <p v-if="loading" role="status" class="py-8 text-center text-sm text-gray-500">{{ t('common.loading') }}</p>
    <ol v-else class="max-h-[60vh] space-y-2 overflow-y-auto" data-testid="monitor-v1-order-list">
      <li v-for="(item, index) in items" :key="item.id" class="flex items-center gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-700" data-testid="monitor-v1-order-row" :data-monitor-id="item.id">
        <span class="w-6 shrink-0 text-center text-sm tabular-nums text-gray-400">{{ index + 1 }}</span>
        <div class="min-w-0 flex-1">
          <p class="truncate text-sm font-medium text-gray-900 dark:text-white" :title="item.name">{{ item.name }}</p>
          <p class="truncate text-xs text-gray-500">{{ platformLabel(item.provider) }}<span v-if="item.group_name"> · {{ item.group_name }}</span><span v-if="!item.enabled"> · {{ t('admin.channelMonitor.onlyDisabled') }}</span></p>
        </div>
        <button type="button" class="btn btn-secondary !p-2" :disabled="saving || index === 0" :aria-label="t('admin.channelMonitor.order.up', { name: item.name })" data-testid="monitor-v1-order-up" @click="move(index, -1)"><Icon name="arrowUp" size="sm" /></button>
        <button type="button" class="btn btn-secondary !p-2" :disabled="saving || index === items.length - 1" :aria-label="t('admin.channelMonitor.order.down', { name: item.name })" data-testid="monitor-v1-order-down" @click="move(index, 1)"><Icon name="arrowDown" size="sm" /></button>
      </li>
    </ol>
    <template #footer>
      <button type="button" class="btn btn-secondary" :disabled="saving" @click="close">{{ t('common.cancel') }}</button>
      <button v-if="loadFailed" type="button" class="btn btn-secondary" @click="load">{{ t('admin.channelMonitor.order.retry') }}</button>
      <button type="button" class="btn btn-primary" :disabled="loading || saving || loadFailed || !dirty" data-testid="monitor-v1-order-save" @click="save">{{ t(saving ? 'common.saving' : 'common.save') }}</button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { listDisplayOrder, updateDisplayOrder, type ChannelMonitorOrderItem } from '@/api/admin/channelMonitor'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { extractApiErrorCode, extractApiErrorMessage } from '@/utils/apiError'
import { platformLabel } from '@/utils/platformColors'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const items = ref<ChannelMonitorOrderItem[]>([])
const original = ref('')
const loading = ref(false)
const saving = ref(false)
const loadFailed = ref(false)
const error = ref('')
let request = 0
const dirty = computed(() => JSON.stringify(items.value.map((item) => item.id)) !== original.value)

async function load() {
  const current = ++request
  loading.value = true
  loadFailed.value = false
  error.value = ''
  items.value = []
  try {
    const next = await listDisplayOrder()
    if (current !== request || !props.show) return
    items.value = [...next]
    original.value = JSON.stringify(next.map((item) => item.id))
  } catch (err) {
    if (current !== request || !props.show) return
    loadFailed.value = true
    error.value = extractApiErrorMessage(err, t('admin.channelMonitor.order.loadFailed'))
  } finally {
    if (current === request) loading.value = false
  }
}

function close() {
  if (!saving.value) emit('close')
}

function move(index: number, offset: number) {
  const target = index + offset
  if (saving.value || target < 0 || target >= items.value.length) return
  const [item] = items.value.splice(index, 1)
  items.value.splice(target, 0, item)
}

async function save() {
  if (loading.value || saving.value || loadFailed.value || !dirty.value) return
  saving.value = true
  error.value = ''
  try {
    await updateDisplayOrder(items.value.map((item) => item.id))
    emit('saved')
    emit('close')
  } catch (err) {
    error.value = extractApiErrorCode(err) === 'CHANNEL_MONITOR_ORDER_CONFLICT'
      ? t('admin.channelMonitor.order.conflict')
      : extractApiErrorMessage(err, t('admin.channelMonitor.order.saveFailed'))
  } finally {
    saving.value = false
  }
}

watch(() => props.show, (show) => {
  if (show) void load()
  else request++
}, { immediate: true })
</script>
