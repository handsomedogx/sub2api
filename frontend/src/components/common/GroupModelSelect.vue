<template>
  <div>
    <Select
      :id="id"
      :model-value="modelValue"
      :options="options"
      :disabled="disabled || !groupId || loading || !!error"
      :loading="loading"
      :aria-label="t('monitorCommon.modelSelect')"
      :placeholder="t(!groupId ? 'monitorCommon.chooseGroupFirst' : 'monitorCommon.modelSelect')"
      searchable
      @update:model-value="emit('update:modelValue', String($event ?? ''))"
    />
    <p v-if="error" class="mt-1 text-xs text-red-600 dark:text-red-400" role="alert">
      {{ t('monitorCommon.modelsLoadError') }}
      <button type="button" class="ml-2 underline" @click="load">{{ t('common.retry') }}</button>
    </p>
    <p v-else-if="groupId && !loading && !models.length" class="mt-1 text-xs text-gray-500" role="status">{{ t('monitorCommon.modelsEmpty') }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from './Select.vue'
import { listGroupTestModels } from '@/api/admin/groupTestModels'

const props = defineProps<{ id?: string; groupId?: number | null; modelValue: string; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: string]; loaded: [models: string[]] }>()
const { t } = useI18n()
const models = ref<string[]>([])
const loading = ref(false)
const error = ref(false)
let controller: AbortController | undefined
let generation = 0
const options = computed(() => {
  const result = models.value.map(value => ({ value, label: value }))
  if (props.modelValue && !models.value.includes(props.modelValue)) {
    result.unshift({ value: props.modelValue, label: `${props.modelValue} · ${t('monitorCommon.modelUnavailable')}` })
  }
  return result
})
async function load() {
  controller?.abort()
  const current = ++generation
  models.value = []
  error.value = false
  loading.value = false
  emit('loaded', [])
  if (!props.groupId) return
  controller = new AbortController()
  loading.value = true
  try {
    const result = await listGroupTestModels(props.groupId, controller.signal)
    if (current !== generation) return
    models.value = [...new Set(result)].sort()
    emit('loaded', models.value)
  } catch {
    if (current === generation) error.value = true
  } finally {
    if (current === generation) loading.value = false
  }
}
watch(() => props.groupId, () => { void load() }, { immediate: true })
onBeforeUnmount(() => { generation++; controller?.abort() })
</script>
