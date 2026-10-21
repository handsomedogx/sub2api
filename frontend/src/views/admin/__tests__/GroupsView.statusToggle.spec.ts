import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AdminGroup } from '@/types'
import GroupsView from '@/views/admin/GroupsView.vue'

const mocks = vi.hoisted(() => ({
  isSimpleMode: false,
  listGroups: vi.fn(),
  createGroup: vi.fn(),
  updateGroup: vi.fn(),
  toggleStatus: vi.fn(),
  getModelAllowlistCandidates: vi.fn(),
  getUsageSummary: vi.fn(),
  getCapacitySummary: vi.fn(),
  getLiveCapability: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: {
      list: mocks.listGroups,
      create: mocks.createGroup,
      update: mocks.updateGroup,
      toggleStatus: mocks.toggleStatus,
      getModelAllowlistCandidates: mocks.getModelAllowlistCandidates,
      getUsageSummary: mocks.getUsageSummary,
      getCapacitySummary: mocks.getCapacitySummary,
      getLiveCapability: mocks.getLiveCapability,
      getAll: vi.fn().mockResolvedValue([]),
      duplicate: vi.fn(),
      delete: vi.fn(),
      updateSortOrder: vi.fn()
    },
    accounts: { list: vi.fn(), getById: vi.fn() }
  }
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: mocks.showSuccess, showError: mocks.showError }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSimpleMode: mocks.isSimpleMode }) }))
vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({ isCurrentStep: vi.fn(() => false), nextStep: vi.fn() })
}))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const baseGroup = {
  id: 42, name: 'Claude Code', description: null, platform: 'anthropic', rate_multiplier: 1, rpm_limit: 0,
  is_exclusive: false, status: 'active', subscription_type: 'standard',
  daily_limit_usd: null, weekly_limit_usd: null, monthly_limit_usd: null,
  allow_image_generation: false, allow_batch_image_generation: false,
  peak_rate_enabled: false, peak_start: '', peak_end: '', peak_rate_multiplier: 1,
  claude_code_only: false, fallback_group_id: null, fallback_group_id_on_invalid_request: null,
  allow_messages_dispatch: false, require_oauth_only: false, require_privacy_set: false,
  force_openai_fast: false, free_openai_fast: false, stream_only: true, system_prompt_mode: 'replace', system_prompt: 'saved gateway prompt',
  model_routing: null, model_routing_enabled: false, mcp_xml_inject: true, supported_model_scopes: [],
  account_count: 1, active_account_count: 1, rate_limited_account_count: 0, sort_order: 10,
  created_at: '2026-09-24T00:00:00Z', updated_at: '2026-09-24T00:00:00Z'
} as unknown as AdminGroup

// Render the status cell alongside each group's name and actions.
const DataTableStub = defineComponent({
  props: { data: { type: Array, default: () => [] } },
  template: `<div><div v-for="row in data" :key="row.id" :data-testid="'group-row-' + row.id">
    <slot name="cell-name" :value="row.name" :row="row" /><slot name="cell-status" :value="row.status" :row="row" /><slot name="cell-actions" :row="row" />
  </div></div>`
})

function mountView() {
  return mount(GroupsView, {
    global: {
      stubs: {
        AppLayout: defineComponent({ template: '<main><slot /></main>' }),
        TablePageLayout: defineComponent({ template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>' }),
        DataTable: DataTableStub,
        BaseDialog: defineComponent({ props: { show: Boolean }, template: '<div v-if="show"><slot /><slot name="footer" /></div>' }),
        Pagination: true, ConfirmDialog: true, EmptyState: true, Select: defineComponent({
          props: ['modelValue', 'options'], emits: ['update:modelValue'],
          template: `<select :value="modelValue" @change="$emit('update:modelValue', $event.target.value)"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>`
        }), PlatformIcon: true, Icon: true,
        GroupCapacityBadge: true, GroupRateMultipliersModal: true, GroupRPMOverridesModal: true, VueDraggable: true
      }
    }
  })
}

describe('GroupsView quick status toggle', () => {
  beforeEach(() => {
    localStorage.clear()
    mocks.isSimpleMode = false
    vi.clearAllMocks()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    mocks.listGroups.mockResolvedValue({ items: [{ ...baseGroup }, { ...baseGroup, id: 43, status: 'inactive' }], total: 2, page: 1, page_size: 20, pages: 1 })
    mocks.getModelAllowlistCandidates.mockResolvedValue([])
    mocks.getUsageSummary.mockResolvedValue([])
    mocks.getCapacitySummary.mockResolvedValue([])
    mocks.getLiveCapability.mockResolvedValue({ supported: false })
  })
  afterEach(() => vi.restoreAllMocks())

  it.each([
    [42, 'inactive', 'true', 'false', 'admin.groups.groupDisabled'],
    [43, 'active', 'false', 'true', 'admin.groups.groupEnabled']
  ])('changes group %s status without opening the editor', async (id, status, before, after, message) => {
    mocks.toggleStatus.mockResolvedValue({ ...baseGroup, id, status })
    const wrapper = mountView()
    await flushPromises()
    const toggle = wrapper.get('[data-testid="group-status-toggle-' + id + '"]')
    expect(toggle.attributes('aria-checked')).toBe(before)
    await toggle.trigger('click')
    await flushPromises()
    expect(mocks.toggleStatus).toHaveBeenCalledWith(id, status)
    expect(toggle.attributes('aria-checked')).toBe(after)
    expect(mocks.showSuccess).toHaveBeenCalledWith(message)
    expect(mocks.listGroups).toHaveBeenCalledTimes(1)
    expect(wrapper.find('#edit-group-form').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps the previous state on failure and allows retry', async () => {
    mocks.toggleStatus.mockRejectedValue(new Error('offline'))
    const wrapper = mountView()
    await flushPromises()
    const toggle = wrapper.get('[data-testid="group-status-toggle-42"]')
    await toggle.trigger('click')
    await flushPromises()
    expect(toggle.attributes('aria-checked')).toBe('true')
    expect(toggle.attributes('disabled')).toBeUndefined()
    expect(mocks.showError).toHaveBeenCalled()
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('blocks duplicate updates for the same row and leaves other rows usable', async () => {
    let finish!: (value: AdminGroup) => void
    mocks.toggleStatus.mockImplementation(() => new Promise<AdminGroup>((resolve) => { finish = resolve }))
    const wrapper = mountView()
    await flushPromises()
    const toggle = wrapper.get('[data-testid="group-status-toggle-42"]')
    await toggle.trigger('click')
    expect(toggle.attributes('disabled')).toBeDefined()
    expect(toggle.attributes('aria-busy')).toBe('true')
    expect(toggle.attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[data-testid="group-status-toggle-43"]').attributes('disabled')).toBeUndefined()
    await toggle.trigger('click')
    expect(mocks.toggleStatus).toHaveBeenCalledTimes(1)
    finish({ ...baseGroup, status: 'inactive' })
    await flushPromises()
    expect(toggle.attributes('disabled')).toBeUndefined()
    expect(toggle.attributes('aria-checked')).toBe('false')
    wrapper.unmount()
  })

  it('refreshes the status-filtered list after a successful toggle', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="group-status-filter"]').setValue('active')
    await flushPromises()
    const before = mocks.listGroups.mock.calls.length
    mocks.toggleStatus.mockResolvedValue({ ...baseGroup, status: 'inactive' })
    mocks.listGroups.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
    await wrapper.get('[data-testid="group-status-toggle-42"]').trigger('click')
    await flushPromises()
    expect(mocks.listGroups.mock.calls.length).toBe(before + 1)
    expect(wrapper.find('[data-testid="group-row-42"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('preserves the existing read-only status in simple mode', async () => {
    mocks.isSimpleMode = true
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-testid="group-status-toggle-42"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="group-row-42"]').text()).toContain('admin.accounts.status.active')
    wrapper.unmount()
  })
})
