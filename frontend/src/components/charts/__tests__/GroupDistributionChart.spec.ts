import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import GroupDistributionChart from '../GroupDistributionChart.vue'

const messages: Record<string, string> = {
  'admin.dashboard.groupDistribution': 'Group Distribution',
  'admin.dashboard.group': 'Group',
  'admin.dashboard.noGroup': 'No Group',
  'admin.dashboard.requests': 'Requests',
  'admin.dashboard.tokens': 'Tokens',
  'admin.dashboard.actual': 'Actual',
  'admin.dashboard.accountCost': 'Account Cost',
  'admin.dashboard.standard': 'Standard',
  'admin.dashboard.metricTokens': 'By Tokens',
  'admin.dashboard.metricActualCost': 'By Actual Cost',
  'admin.dashboard.noDataAvailable': 'No data available',
  'usage.latencyCacheRate': 'Cache rate',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

vi.mock('vue-chartjs', () => ({
  Doughnut: {
    props: ['data'],
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>',
  },
}))

describe('GroupDistributionChart', () => {
  const groupStats = [
    {
      group_id: 1,
      group_name: 'group-a',
      requests: 9,
      total_tokens: 1200,
      cost: 1.8,
      actual_cost: 0.1,
    },
    {
      group_id: 2,
      group_name: 'group-b',
      requests: 4,
      total_tokens: 600,
      cost: 0.7,
      actual_cost: 0.9,
    },
  ]

  it('uses total_tokens and token ordering by default', () => {
    const wrapper = mount(GroupDistributionChart, {
      props: {
        groupStats,
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    expect(chartData.labels).toEqual(['group-a', 'group-b'])
    expect(chartData.datasets[0].data).toEqual([1200, 600])

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('group-a')
    expect(rows[1].text()).toContain('group-b')

    const options = (wrapper.vm as any).$?.setupState.doughnutOptions
    const label = options.plugins.tooltip.callbacks.label({
      label: 'group-a',
      raw: 1200,
      dataset: { data: [1200, 600] },
    })
    expect(label).toBe('group-a: 1.20K (66.7%)')
  })

  it('uses actual_cost and reorders rows in actual cost mode', () => {
    const wrapper = mount(GroupDistributionChart, {
      props: {
        groupStats,
        metric: 'actual_cost',
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    expect(chartData.labels).toEqual(['group-b', 'group-a'])
    expect(chartData.datasets[0].data).toEqual([0.9, 0.1])

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('group-b')
    expect(rows[1].text()).toContain('group-a')

    const options = (wrapper.vm as any).$?.setupState.doughnutOptions
    const label = options.plugins.tooltip.callbacks.label({
      label: 'group-b',
      raw: 0.9,
      dataset: { data: [0.9, 0.1] },
    })
    expect(label).toBe('group-b: $0.900 (90.0%)')
  })

  it('can hide account cost for user usage stats without account_cost', () => {
    const wrapper = mount(GroupDistributionChart, {
      props: {
        groupStats,
        showAccountCost: false,
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    expect(wrapper.text()).not.toContain('Account Cost')
    expect(wrapper.findAll('thead th')).toHaveLength(5)
    expect(wrapper.findAll('tbody tr')[0].findAll('td')).toHaveLength(5)
  })

  it('shows each group’s prompt cache rate from aggregated buckets, excluding output tokens', () => {
    const wrapper = mount(GroupDistributionChart, {
      props: {
        showCacheRate: true,
        groupStats: [
          { ...groupStats[0], input_tokens: 110, cache_creation_tokens: 90, cache_read_tokens: 900, total_tokens: 9100 },
          { ...groupStats[1], input_tokens: 100, cache_creation_tokens: 0, cache_read_tokens: 0 },
          { ...groupStats[1], group_id: 3, input_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0 },
        ],
      },
    })

    expect(wrapper.get('[data-testid="group-cache-rate-1"]').text()).toBe('81.8%')
    expect(wrapper.get('[data-testid="group-cache-rate-2"]').text()).toBe('0%')
    expect(wrapper.get('[data-testid="group-cache-rate-3"]').text()).toBe('—')
    expect(wrapper.findAll('thead th')).toHaveLength(7)
    expect(wrapper.get('[data-testid="group-cache-rate-1"]').attributes('title')).toBeTruthy()
    wrapper.unmount()
  })

  it('keeps unavailable cache data distinct from zero and respects the visibility switch', async () => {
    const wrapper = mount(GroupDistributionChart, { props: { groupStats, showCacheRate: true } })
    expect(wrapper.get('[data-testid="group-cache-rate-1"]').text()).toBe('—')

    await wrapper.setProps({ showCacheRate: false })
    expect(wrapper.find('[data-testid="group-cache-rate-1"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Cache rate')
    wrapper.unmount()
  })
})
