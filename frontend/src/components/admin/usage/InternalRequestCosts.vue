<template>
  <section class="space-y-4 p-4" data-testid="internal-request-costs">
    <form class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4" @submit.prevent="apply">
      <label>{{ t('profitOps.from') }}<input v-model="from" type="datetime-local" required class="input w-full" /></label>
      <label>{{ t('profitOps.until') }}<input v-model="until" type="datetime-local" required class="input w-full" /></label>
      <div><label for="internal-source">{{ t('profitOps.source') }}</label><Select id="internal-source" v-model="source" data-testid="internal-source" :options="sourceOptions" :aria-label="t('profitOps.source')" /></div>
      <label>{{ t('profitOps.account') }}<input v-model.number="account" type="number" min="0" class="input w-full" /></label>
      <label>{{ t('profitOps.group') }}<input v-model.number="group" type="number" min="0" class="input w-full" /></label>
      <label>{{ t('profitOps.model') }}<input v-model="model" maxlength="200" class="input w-full" /></label>
      <button class="btn btn-primary self-end" :disabled="loading">{{ t('profitOps.apply') }}</button>
      <button type="button" class="btn btn-secondary self-end" :disabled="loading" @click="load">{{ t('profitOps.refresh') }}</button>
    </form>
    <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
    <p v-if="loading" role="status">{{ t('profitOps.loading') }}</p>
    <template v-if="data && !error">
      <p data-testid="internal-summary">{{ t('profitOps.internalRequests') }} {{ total }} · {{ t('profitOps.internalAmount') }} ${{ amount(recordedCost) }}<span v-if="unknownRequests" class="text-amber-600"> ({{ t('profitOps.internalPartial') }})</span> · {{ t('profitOps.unknownInternal') }} {{ unknownRequests }}</p>
      <div class="overflow-x-auto"><table class="w-full min-w-[1050px] text-left text-sm">
        <thead><tr><th v-for="key in headings" :key="key" class="p-2">{{ t(`profitOps.${key}`) }}</th></tr></thead>
        <tbody><tr v-for="r in data.usage.internal_records" :key="r.id" class="border-t dark:border-dark-700">
          <td class="p-2">{{ new Date(r.created_at).toLocaleString() }}</td>
          <td class="p-2">{{ t(`profitOps.internalKinds.${r.kind}`) }}</td>
          <td class="max-w-64 break-words p-2">
            <span v-if="r.account_id">{{ r.account_name || t('profitOps.account') }} <span class="text-gray-500">#{{ r.account_id }}</span></span>
            <span v-else>{{ t('profitOps.unattributed') }}</span>
            <span v-if="r.monitor_id" class="mt-1 block text-xs text-gray-500">{{ t('profitOps.monitor') }}: {{ r.monitor_name || '—' }} #{{ r.monitor_id }}</span>
            <span v-if="r.group_id" class="mt-1 block text-xs text-gray-500">{{ t('profitOps.requestGroup') }}: {{ r.group_name || '—' }} #{{ r.group_id }}</span>
          </td>
          <td class="p-2 break-all">{{ r.requested_model }}<span v-if="r.model !== r.requested_model" class="block text-xs text-gray-500">→ {{ r.model }}</span></td>
          <td class="p-2 tabular-nums">{{ tokenText(r.tokens) }}</td>
          <td class="p-2">{{ amount(r.standard_cost) }}</td>
          <td class="p-2">{{ amount(r.cost_multiplier) }}</td>
          <td class="p-2">{{ amount(r.cost_usd) }}<span v-if="r.incomplete" class="block text-xs text-amber-600">{{ r.cost_usd == null ? t('profitOps.unknown') : t('profitOps.internalPartial') }}</span></td>
          <td class="p-2">{{ r.status }}</td>
        </tr></tbody>
      </table></div>
      <p v-if="!total">{{ t('profitOps.empty') }}</p>
      <div class="flex items-center gap-3"><button class="btn btn-secondary" :disabled="loading || query.page <= 1" @click="page(-1)">{{ t('profitOps.previous') }}</button><span>{{ query.page }} · {{ total }}</span><button class="btn btn-secondary" :disabled="loading || query.page * query.page_size >= total" @click="page(1)">{{ t('profitOps.next') }}</button></div>
    </template>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import { readProfitOperations, type OperationsQuery, type OperationsSnapshot } from '@/api/admin/profitOperations'
const { t } = useI18n()
const sources = ['internal', 'pelican', 'monitor'] as const
const sourceOptions = computed(() => sources.map(value => ({ value, label: t(`profitOps.sources.${value}`) })))
const headings = ['internalTime','source','account','model','internalTokens','standard_charge','snapshotRate','internalAmount','internalStatus'] as const
const now = new Date(), start = new Date(now.getTime() - 86400000)
const local = (d: Date) => new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
const from = ref(local(start)), until = ref(local(now)), source = ref<typeof sources[number]>('internal')
const account = ref(0), group = ref(0), model = ref('')
const query = ref<OperationsQuery>({ from: start.toISOString(), until: now.toISOString(), source:'internal', upstream:'',account_id:0,group_id:0,model:'',model_scope:'public',dimension:'account',sort:'requests',direction:'desc',page:1,page_size:20 })
const data = ref<OperationsSnapshot|null>(null), error = ref(''), loading = ref(false)
const total = computed(() => data.value?.usage.internal_records_total || 0)
const unknownRequests = computed(() => data.value?.usage.summary.unknown_internal_requests || 0)
// Use the full filtered range's saved amounts, including partial costs, rather than the complete total or current page.
const recordedCost = computed(() => data.value?.usage.summary.covered_internal_cost ?? data.value?.usage.summary.internal_cost)
let controller: AbortController|undefined, request = 0, alive = true
function amount(value: number|null|undefined) { return value == null || !Number.isFinite(value) ? t('profitOps.unknown') : value.toLocaleString(undefined,{maximumFractionDigits:8}) }
function tokenText(tokens: Record<string,number> | null) { return ['InputTokens','OutputTokens','CacheReadTokens','CacheCreationTokens'].map(k => tokens?.[k] == null ? '—' : tokens[k].toLocaleString()).join(' / ') }
async function load() {
 const id = ++request; controller?.abort(); controller = new AbortController(); loading.value = true; error.value = ''; data.value = null
 try { const result = await readProfitOperations({...query.value},controller.signal); if (alive && id === request) data.value = result }
 catch { if (alive && id === request) error.value = t('profitOps.queryFailed') }
 finally { if (alive && id === request) loading.value = false }
}
function apply() {
 const a = new Date(from.value), b = new Date(until.value)
 if (!Number.isFinite(a.getTime()) || !Number.isFinite(b.getTime()) || b <= a || b.getTime()-a.getTime()>32*86400000) { error.value=t('profitOps.invalidRange'); return }
 query.value={...query.value,from:a.toISOString(),until:b.toISOString(),source:source.value,account_id:account.value,group_id:group.value,model:model.value,page:1}; void load()
}
function page(delta: number) { query.value={...query.value,page:query.value.page+delta}; void load() }
onMounted(load)
onBeforeUnmount(() => { alive=false; request++; controller?.abort() })
</script>
