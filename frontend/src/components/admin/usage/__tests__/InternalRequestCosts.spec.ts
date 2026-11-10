import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import InternalRequestCosts from '../InternalRequestCosts.vue'
import { readProfitOperations, type OperationsSnapshot } from '@/api/admin/profitOperations'

vi.mock('@/api/admin/profitOperations', () => ({ readProfitOperations: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const fixture = () => ({ usage: { summary: { internal_cost: null, internal_requests: 21, unknown_internal_requests: 1 }, internal_records_total:21,internal_records:[
 { id:'snapshot',source:'pelican',kind:'group_test',account_id:42,monitor_id:0,requested_model:'alias',model:'actual-model',tokens:{InputTokens:100,OutputTokens:10},standard_cost:1,cost_multiplier:.075,cost_usd:.075,incomplete:false,status:'failed',created_at:'2026-11-09T01:00:00Z' },
 { id:'unknown',source:'monitor',kind:'v1_endpoint',account_id:0,monitor_id:3,requested_model:'model',model:'model',tokens:{},standard_cost:null,cost_multiplier:null,cost_usd:null,incomplete:true,status:'failed',created_at:'2026-11-09T01:00:00Z' },
] } }) as OperationsSnapshot
beforeEach(() => { vi.resetAllMocks(); vi.mocked(readProfitOperations).mockResolvedValue(fixture()) })
describe('internal cost records', () => {
 it('shows saved multipliers and unknown costs without treating them as zero', async () => {
  const w=mount(InternalRequestCosts);await flushPromises()
  expect(readProfitOperations).toHaveBeenCalledWith(expect.objectContaining({source:'internal',page:1}),expect.anything())
  expect(w.text()).toContain('0.075');expect(w.text()).toContain('actual-model');expect(w.text()).toContain('profitOps.unattributed')
  expect(w.get('[data-testid="internal-summary"]').text()).toContain('profitOps.unknown')
  expect(w.text()).toContain('profitOps.internalNote');expect(w.text()).not.toMatch(/NaN|Infinity/)
  w.unmount()
 })
 it('preserves applied source and time while paging', async () => {
  const w=mount(InternalRequestCosts);await flushPromises()
  await w.get('[data-testid="internal-source"]').setValue('monitor');await w.get('form').trigger('submit');await flushPromises()
  const applied=vi.mocked(readProfitOperations).mock.calls.at(-1)![0]
  expect(applied).toMatchObject({source:'monitor',page:1})
  await w.findAll('button').at(-1)!.trigger('click');await flushPromises()
  expect(readProfitOperations).toHaveBeenLastCalledWith(expect.objectContaining({source:'monitor',page:2,from:applied.from,until:applied.until}),expect.anything())
  w.unmount()
 })
 it('shows a query failure instead of zero cost and aborts on leaving', async () => {
  vi.mocked(readProfitOperations).mockRejectedValueOnce(new Error('failed'))
  const w=mount(InternalRequestCosts);await flushPromises()
  expect(w.get('[role="alert"]').text()).toBe('profitOps.queryFailed');expect(w.find('[data-testid="internal-summary"]').exists()).toBe(false)
  const signal=vi.mocked(readProfitOperations).mock.calls[0]![1]!
  w.unmount();expect(signal.aborted).toBe(true)
 })
})
