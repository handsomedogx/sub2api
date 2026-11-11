import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import InternalRequestCosts from '../InternalRequestCosts.vue'
import Select from '@/components/common/Select.vue'
import { readProfitOperations, type OperationsSnapshot } from '@/api/admin/profitOperations'

vi.mock('@/api/admin/profitOperations', () => ({ readProfitOperations: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const fixture = () => ({ usage: { summary: { internal_cost: null, covered_internal_cost: 1.25, internal_requests: 21, unknown_internal_requests: 1 }, internal_records_total:21,internal_records:[
 { id:'snapshot',source:'pelican',kind:'group_test',account_id:42,account_name:'Primary account',monitor_id:22,monitor_name:'Model heartbeat',group_id:7,group_name:'Probe group',requested_model:'alias',model:'actual-model',tokens:{InputTokens:100,OutputTokens:10},standard_cost:1,cost_multiplier:.075,cost_usd:.075,incomplete:false,status:'failed',created_at:'2026-11-09T01:00:00Z' },
 { id:'unknown',source:'monitor',kind:'v1_endpoint',account_id:0,monitor_id:3,requested_model:'model',model:'model',tokens:{},standard_cost:null,cost_multiplier:null,cost_usd:null,incomplete:true,status:'failed',created_at:'2026-11-09T01:00:00Z' },
] } }) as OperationsSnapshot
beforeEach(() => { vi.resetAllMocks(); vi.mocked(readProfitOperations).mockResolvedValue(fixture()) })
const mountCosts = () => mount(InternalRequestCosts, { global: { stubs: { Teleport: true } } })
describe('internal cost records', () => {
 it('shows the recorded range total despite unknown costs and preserves per-record amounts', async () => {
  const w=mountCosts();await flushPromises()
  expect(readProfitOperations).toHaveBeenCalledWith(expect.objectContaining({source:'internal',page:1}),expect.anything())
  expect(w.text()).toContain('0.075');expect(w.text()).toContain('actual-model');expect(w.text()).toContain('profitOps.unattributed')
  const summary = w.get('[data-testid="internal-summary"]').text()
  expect(summary).toContain('profitOps.internalAmount $1.25')
  expect(summary).toContain('profitOps.internalPartial')
  expect(summary).toContain('profitOps.unknownInternal 1')
  expect(summary).not.toContain('$profitOps.unknown')
  expect(w.text()).not.toContain('profitOps.internalNote');expect(w.text()).not.toContain('profitOps.internalHistory');expect(w.text()).not.toMatch(/NaN|Infinity/)
  const account = w.findAll('tbody tr')[0]!.findAll('td')[2]!.text()
  expect(account).toContain('Primary account #42')
  expect(account).toContain('Model heartbeat #22')
  expect(account).toContain('Probe group #7')
  w.unmount()
 })
 it('preserves applied source and time while paging', async () => {
  const w=mountCosts();await flushPromises()
  expect(w.find('select').exists()).toBe(false)
  await w.getComponent(Select).get('button').trigger('click')
  await w.get('[role="option"]:last-child').trigger('click')
  expect(w.getComponent(Select).props('modelValue')).toBe('monitor')
  await w.get('form').trigger('submit');await flushPromises()
  const applied=vi.mocked(readProfitOperations).mock.calls.at(-1)![0]
  expect(applied).toMatchObject({source:'monitor',page:1})
  await w.findAll('button').at(-1)!.trigger('click');await flushPromises()
  expect(readProfitOperations).toHaveBeenLastCalledWith(expect.objectContaining({source:'monitor',page:2,from:applied.from,until:applied.until}),expect.anything())
  expect(w.get('[data-testid="internal-summary"]').text()).toContain('$1.25')
  w.unmount()
 })
 it('shows a query failure instead of zero cost and aborts on leaving', async () => {
  vi.mocked(readProfitOperations).mockRejectedValueOnce(new Error('failed'))
  const w=mountCosts();await flushPromises()
  expect(w.get('[role="alert"]').text()).toBe('profitOps.queryFailed');expect(w.find('[data-testid="internal-summary"]').exists()).toBe(false)
  const signal=vi.mocked(readProfitOperations).mock.calls[0]![1]!
  w.unmount();expect(signal.aborted).toBe(true)
 })
 it.each([
  { covered_internal_cost: 0, internal_cost: null, unknown_internal_requests: 21, amount: '$0', partial: true },
  { covered_internal_cost: 1.25, internal_cost: 1.25, unknown_internal_requests: 0, amount: '$1.25', partial: false },
  { covered_internal_cost: undefined, internal_cost: 0.5, unknown_internal_requests: 0, amount: '$0.5', partial: false },
  { covered_internal_cost: undefined, internal_cost: null, unknown_internal_requests: 1, amount: '$profitOps.unknown', partial: true },
 ])('handles zero, complete and missing recorded sums: %j', async ({ amount, partial, ...summary }) => {
  const data = fixture();Object.assign(data.usage.summary, summary)
  vi.mocked(readProfitOperations).mockResolvedValue(data)
  const w = mountCosts();await flushPromises()
  const text = w.get('[data-testid="internal-summary"]').text()
  expect(text).toContain(amount)
  expect(text.includes('profitOps.internalPartial')).toBe(partial)
  w.unmount()
 })
 it('keeps IDs visible when related names are unavailable', async () => {
  const data = fixture()
  Object.assign(data.usage.internal_records![0]!, { account_name: null, monitor_name: null, group_name: null })
  vi.mocked(readProfitOperations).mockResolvedValue(data)
  const w = mountCosts();await flushPromises()
  const account = w.findAll('tbody tr')[0]!.findAll('td')[2]!.text()
  expect(account).toContain('profitOps.account #42')
  expect(account).toContain('profitOps.monitor: — #22')
  expect(account).toContain('profitOps.requestGroup: — #7')
  w.unmount()
 })
})
