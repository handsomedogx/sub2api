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

// 只渲染分组名称与操作列。
const DataTableStub = defineComponent({
  props: { data: { type: Array, default: () => [] } },
  template: `<div><div v-for="row in data" :key="row.id" :data-testid="'group-row-' + row.id">
    <slot name="cell-name" :value="row.name" :row="row" /><slot name="cell-actions" :row="row" />
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

const buttonByText = (wrapper: ReturnType<typeof mountView>, text: string) =>
  wrapper.findAll('button').find((button) => button.text() === text)

describe('GroupsView system prompt policy', () => {
  beforeEach(() => {
    mocks.isSimpleMode = false
    localStorage.clear()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.clearAllMocks()
    mocks.listGroups.mockResolvedValue({ items: [baseGroup], total: 1, page: 1, page_size: 20, pages: 1 })
    mocks.createGroup.mockResolvedValue(baseGroup)
    mocks.updateGroup.mockResolvedValue(baseGroup)
    mocks.getModelAllowlistCandidates.mockResolvedValue([])
    mocks.getUsageSummary.mockResolvedValue([])
    mocks.getCapacitySummary.mockResolvedValue([])
    mocks.getLiveCapability.mockResolvedValue({ supported: false })
  })
  afterEach(() => vi.restoreAllMocks())

  it.each([false, true])('defaults new groups to passthrough (simple mode: %s)', async (simple) => {
    mocks.isSimpleMode = simple
    const wrapper = mountView()
    await flushPromises()
    await buttonByText(wrapper, 'admin.groups.createGroup')!.trigger('click')
    expect((wrapper.get('[data-testid="create-system-prompt-mode"]').element as HTMLSelectElement).value).toBe('passthrough')
    expect(wrapper.get('[data-testid="create-system-prompt"]').attributes('disabled')).toBeDefined()
    await wrapper.get('#create-group-form input[type="text"]').setValue('Default')
    await wrapper.get('#create-group-form').trigger('submit')
    await flushPromises()
    expect(mocks.createGroup).toHaveBeenCalledWith(expect.objectContaining({ system_prompt_mode: 'passthrough', system_prompt: '' }))
    wrapper.unmount()
  })

  it.each([['inject', false], ['replace', false], ['inject', true], ['replace', true]] as const)('creates a group in %s mode (simple mode: %s)', async (mode, simple) => {
    mocks.isSimpleMode = simple
    const wrapper = mountView()
    await flushPromises()
    await buttonByText(wrapper, 'admin.groups.createGroup')!.trigger('click')
    await wrapper.get('#create-group-form input[type="text"]').setValue('Prompt group')
    await wrapper.get('[data-testid="create-system-prompt-mode"]').setValue(mode)
    await wrapper.get('[data-testid="create-system-prompt"]').setValue('网关指令')
    await wrapper.get('#create-group-form').trigger('submit')
    await flushPromises()
    expect(mocks.createGroup).toHaveBeenCalledWith(expect.objectContaining({ system_prompt_mode: mode, system_prompt: '网关指令' }))
    wrapper.unmount()
  })

  it.each([false, true])('loads saved prompts and keeps the text when switching to passthrough (simple mode: %s)', async (simple) => {
    mocks.isSimpleMode = simple
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="group-row-42"]').findAll('button').find((b) => b.text() === 'common.edit')!.trigger('click')
    expect((wrapper.get('[data-testid="edit-system-prompt-mode"]').element as HTMLSelectElement).value).toBe('replace')
    expect((wrapper.get('[data-testid="edit-system-prompt"]').element as HTMLTextAreaElement).value).toBe('saved gateway prompt')
    await wrapper.get('[data-testid="edit-system-prompt-mode"]').setValue('passthrough')
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(mocks.updateGroup).toHaveBeenCalledWith(42, expect.objectContaining({ system_prompt_mode: 'passthrough', system_prompt: 'saved gateway prompt' }))
    wrapper.unmount()
  })

  it.each([
    ['   ', 'admin.groups.systemPrompt.promptRequired'],
    ['中'.repeat(22000), 'admin.groups.systemPrompt.promptTooLong']
  ])('rejects invalid prompts before saving', async (prompt, error) => {
    const wrapper = mountView()
    await flushPromises()
    await buttonByText(wrapper, 'admin.groups.createGroup')!.trigger('click')
    await wrapper.get('#create-group-form input[type="text"]').setValue('Invalid')
    await wrapper.get('[data-testid="create-system-prompt-mode"]').setValue('inject')
    await wrapper.get('[data-testid="create-system-prompt"]').setValue(prompt)
    await wrapper.get('#create-group-form').trigger('submit')
    await flushPromises()
    expect(mocks.createGroup).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenCalledWith(error)
    wrapper.unmount()
  })
})
