import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import MonitorFormDialog from '@/components/admin/monitor/MonitorFormDialog.vue'

const api = vi.hoisted(() => ({ create: vi.fn(), groups: vi.fn(), models: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: {
  groups: { getAll: api.groups }, channelMonitor: { create: api.create },
  channelMonitorTemplate: { list: vi.fn().mockResolvedValue({ items: [] }) },
  accounts: { list: vi.fn().mockResolvedValue({ items: [] }) },
} }))
vi.mock('@/api/admin/groupTestModels', () => ({ listGroupTestModels: api.models }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: null, showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const Select = {
  props: ['modelValue', 'options', 'disabled'], emits: ['update:modelValue'],
  template: `<select :disabled="disabled" :value="modelValue" @change="$emit('update:modelValue', options.find(o => String(o.value) === $event.target.value)?.value)"><option value="" /><option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option></select>`,
}
function render() { return mount(MonitorFormDialog, { props: { show: true, monitor: null }, global: { stubs: {
  BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }, Select,
  Toggle: true, ProviderIcon: true, ModelTagInput: true, MonitorKeyPickerDialog: true, MonitorAdvancedRequestConfig: true,
} } }) }
beforeEach(() => {
  vi.clearAllMocks()
  api.groups.mockResolvedValue([{ id: 4, name: 'Group A', platform: 'openai', status: 'active' }, { id: 6, name: 'Group B', platform: 'anthropic', status: 'active' }])
  api.models.mockImplementation((id: number) => Promise.resolve(id === 4 ? ['public-a'] : ['public-b']))
  api.create.mockResolvedValue({})
})
describe('V1 group monitor creation', () => {
  it('selects a group and model without requesting external credentials', async () => {
    const wrapper = render()
    await flushPromises()
    expect(wrapper.find('[data-testid=monitor-endpoint]').exists()).toBe(false)
    expect(wrapper.find('input[type=password]').exists()).toBe(false)
    await wrapper.get('[data-testid=monitor-group]').setValue('4')
    await flushPromises()
    await wrapper.get('[data-testid=monitor-group-model] select').setValue('public-a')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.create).toHaveBeenCalledWith(expect.objectContaining({ group_id: 4, name: 'Group A', primary_model: 'public-a', provider: 'openai', endpoint: '', api_key: '', check_mode: 'probe' }))
    wrapper.unmount()
  })
  it('clears model selections when changing groups', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid=monitor-group]').setValue('4')
    await flushPromises()
    await wrapper.get('[data-testid=monitor-group-model] select').setValue('public-a')
    await wrapper.get('[data-testid=monitor-group]').setValue('6')
    await flushPromises()
    expect(wrapper.get('[data-testid=monitor-group-model] select').findAll('option').map(o => o.attributes('value'))).not.toContain('public-a')
    await wrapper.get('form').trigger('submit')
    expect(api.create).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
