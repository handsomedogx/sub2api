import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import MonitorOrderDialog from '../MonitorOrderDialog.vue'

const api = vi.hoisted(() => ({ listDisplayOrder: vi.fn(), updateDisplayOrder: vi.fn() }))
vi.mock('@/api/admin/channelMonitor', () => api)
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key, te: () => false }),
}))
const rows = [
  { id: 7, name: 'OpenAI', provider: 'openai', group_name: 'GPT', enabled: true },
  { id: 3, name: 'Paused', provider: 'anthropic', group_name: '', enabled: false },
  { id: 1, name: 'Gemini', provider: 'gemini', group_name: '', enabled: true },
]
const mountDialog = () => mount(MonitorOrderDialog, {
  props: { show: true },
  global: { stubs: {
    Icon: true,
    BaseDialog: { props: ['show'], emits: ['close'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
  } },
})
beforeEach(() => {
  api.listDisplayOrder.mockReset().mockResolvedValue(structuredClone(rows))
  api.updateDisplayOrder.mockReset().mockResolvedValue(undefined)
})
const ids = (wrapper: ReturnType<typeof mountDialog>) => wrapper.findAll('[data-testid="monitor-v1-order-row"]').map((row) => Number(row.attributes('data-monitor-id')))

it('moves the complete list including disabled monitors, then saves every ID in order', async () => {
  const wrapper = mountDialog()
  await flushPromises()
  expect(ids(wrapper)).toEqual([7, 3, 1])
  expect(wrapper.findAll('[data-testid="monitor-v1-order-up"]')[0].attributes('disabled')).toBeDefined()
  expect(wrapper.findAll('[data-testid="monitor-v1-order-down"]')[2].attributes('disabled')).toBeDefined()
  expect(wrapper.get('[data-testid="monitor-v1-order-save"]').attributes('disabled')).toBeDefined()
  await wrapper.findAll('[data-testid="monitor-v1-order-up"]')[1].trigger('click')
  expect(ids(wrapper)).toEqual([3, 7, 1])
  await wrapper.get('[data-testid="monitor-v1-order-save"]').trigger('click')
  await flushPromises()
  expect(api.updateDisplayOrder).toHaveBeenCalledWith([3, 7, 1])
  expect(wrapper.emitted('saved')).toHaveLength(1)
  expect(wrapper.emitted('close')).toHaveLength(1)
  wrapper.unmount()
})

it('leaves the saved order untouched when edits are canceled and reloads on reopen', async () => {
  const wrapper = mountDialog()
  await flushPromises()
  await wrapper.findAll('[data-testid="monitor-v1-order-down"]')[0].trigger('click')
  await wrapper.findAll('button').find((button) => button.text() === 'common.cancel')!.trigger('click')
  expect(api.updateDisplayOrder).not.toHaveBeenCalled()
  await wrapper.setProps({ show: false })
  await wrapper.setProps({ show: true })
  await flushPromises()
  expect(ids(wrapper)).toEqual([7, 3, 1])
  expect(wrapper.get('[data-testid="monitor-v1-order-save"]').attributes('disabled')).toBeDefined()
  wrapper.unmount()
})

it('keeps edits after a save failure and blocks duplicate requests while saving', async () => {
  let rejectSave!: (reason: Error) => void
  api.updateDisplayOrder.mockReturnValue(new Promise((_resolve, reject) => { rejectSave = reject }))
  const wrapper = mountDialog()
  await flushPromises()
  await wrapper.findAll('[data-testid="monitor-v1-order-down"]')[0].trigger('click')
  await wrapper.get('[data-testid="monitor-v1-order-save"]').trigger('click')
  expect(wrapper.get('[data-testid="monitor-v1-order-save"]').attributes('disabled')).toBeDefined()
  expect(wrapper.findAll('[data-testid="monitor-v1-order-up"]').every((button) => button.attributes('disabled') !== undefined)).toBe(true)
  await wrapper.get('[data-testid="monitor-v1-order-save"]').trigger('click')
  expect(api.updateDisplayOrder).toHaveBeenCalledTimes(1)
  rejectSave(new Error('unavailable'))
  await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('unavailable')
  expect(ids(wrapper)).toEqual([3, 7, 1])
  expect(wrapper.emitted('saved')).toBeUndefined()
  expect(wrapper.emitted('close')).toBeUndefined()
  wrapper.unmount()
})

it('disables saving after a failed load and supports retry', async () => {
  api.listDisplayOrder.mockRejectedValueOnce(new Error('offline'))
  const wrapper = mountDialog()
  await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('offline')
  expect(wrapper.get('[data-testid="monitor-v1-order-save"]').attributes('disabled')).toBeDefined()
  await wrapper.findAll('button').find((button) => button.text() === 'admin.channelMonitor.order.retry')!.trigger('click')
  await flushPromises()
  expect(ids(wrapper)).toEqual([7, 3, 1])
  wrapper.unmount()
})

it('explains a changed channel list without discarding edits or reporting success', async () => {
  api.updateDisplayOrder.mockRejectedValue({ reason: 'CHANNEL_MONITOR_ORDER_CONFLICT' })
  const wrapper = mountDialog()
  await flushPromises()
  await wrapper.findAll('[data-testid="monitor-v1-order-down"]')[0].trigger('click')
  await wrapper.get('[data-testid="monitor-v1-order-save"]').trigger('click')
  await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toBe('admin.channelMonitor.order.conflict')
  expect(ids(wrapper)).toEqual([3, 7, 1])
  expect(wrapper.emitted('saved')).toBeUndefined()
  wrapper.unmount()
})
