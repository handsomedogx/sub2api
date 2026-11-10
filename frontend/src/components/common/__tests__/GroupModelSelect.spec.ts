import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import GroupModelSelect from '../GroupModelSelect.vue'

const { list } = vi.hoisted(() => ({ list: vi.fn() }))
vi.mock('@/api/admin/groupTestModels', () => ({ listGroupTestModels: list }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const Select = { props: ['modelValue', 'options', 'disabled'], template: '<select :disabled="disabled"><option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option></select>' }
const render = (groupId = 1, modelValue = '') => mount(GroupModelSelect, { props: { groupId, modelValue }, global: { stubs: { Select } } })

describe('group test model catalog', () => {
  it('shows only the selected group and ignores a late response from the previous group', async () => {
    let finish!: (models: string[]) => void
    list.mockReset().mockImplementationOnce(() => new Promise<string[]>(resolve => { finish = resolve })).mockResolvedValueOnce(['public-b', 'public-b'])
    const wrapper = render()
    await wrapper.setProps({ groupId: 2 })
    await flushPromises()
    finish(['private-a'])
    await flushPromises()
    expect(wrapper.findAll('option').map(o => o.attributes('value'))).toEqual(['public-b'])
    expect(wrapper.emitted('loaded')?.at(-1)).toEqual([['public-b']])
    wrapper.unmount()
  })

  it('keeps the configured model and offers retry after a catalog failure', async () => {
    list.mockReset().mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(['saved'])
    const wrapper = render(1, 'saved')
    await flushPromises()
    expect(wrapper.get('[role=alert]').text()).toContain('modelsLoadError')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.get('[role=alert] button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role=alert]').exists()).toBe(false)
    expect(wrapper.get('option').text()).toBe('saved')
    wrapper.unmount()
  })

  it('does not request a catalog until a group is selected', async () => {
    list.mockReset()
    const wrapper = render(0)
    await flushPromises()
    expect(list).not.toHaveBeenCalled()
    expect(wrapper.get('select').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
})
