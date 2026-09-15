// 商品编辑页组件测试：进入编辑/保存/取消不触碰浏览量计数接口，非卖家被拦截。
// 数据隔离：每个用例独立 pinia 与全新 mock，互不影响，连续运行结果一致。
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import ProductEditPage from '../ProductEditPage.vue'
import { useUserStore } from '../../stores/userStore'
import type { UserVO } from '../../api/types'

// ---- mock 路由与接口层（vi.hoisted 保证在页面模块导入前就绪） ----
const routerMock = vi.hoisted(() => ({ push: vi.fn(), replace: vi.fn() }))
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: '7' } }),
  useRouter: () => routerMock
}))

const apiMock = vi.hoisted(() => ({
  getProductForEdit: vi.fn(),
  getProduct: vi.fn(),
  updateProduct: vi.fn()
}))
vi.mock('../../api/product', () => apiMock)

const messageMock = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn(), warning: vi.fn() }))
vi.mock('element-plus', () => ({ ElMessage: messageMock }))

vi.mock('../../components/ImageUploader.vue', () => ({ default: { template: '<div />' } }))

const sellerUser: UserVO = {
  id: 1, username: 'seller', nickname: '卖家', role: 'user',
  credit_score: 100, status: 'active', created_at: ''
}

const productVO = {
  id: 7,
  seller_id: 1,
  title: 'iPhone 13',
  description: '九成新，配件齐全',
  original_price: 5999,
  price: 3999,
  condition: 'almost_new',
  category: 'digital',
  images: ['/uploads/a.jpg', '/uploads/b.jpg'],
  status: 'on_sale',
  view_count: 5,
  favorite_count: 2,
  created_at: ''
}

const stubs = {
  ElCard: { template: '<div><slot /></div>' },
  ElForm: { template: '<form><slot /></form>' },
  ElFormItem: { template: '<div><slot /></div>' },
  ElInput: { props: ['modelValue'], template: '<input :value="modelValue" />' },
  ElInputNumber: true,
  ElSelect: true,
  ElOption: true,
  ElButton: { template: '<button><slot /></button>' } // @click 经属性透传落到 button 上
}

function mountPage() {
  return mount(ProductEditPage, {
    global: {
      stubs,
      directives: { loading: {} }, // Element Plus 的 v-loading 在测试环境置空
      mocks: { $router: routerMock } // 模板中取消按钮使用 $router.push
    }
  })
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
  const userStore = useUserStore()
  userStore.user = { ...sellerUser }
})

describe('ProductEditPage 浏览量链路', () => {
  it('进入编辑页：走不计数接口完整回填，不触碰计数接口', async () => {
    apiMock.getProductForEdit.mockResolvedValue({ data: { ...productVO } })
    const wrapper = mountPage()
    await flushPromises()

    // 关键断言：回填走编辑专用接口，公开详情（计数）接口一次都不能被调用
    expect(apiMock.getProductForEdit).toHaveBeenCalledTimes(1)
    expect(apiMock.getProductForEdit).toHaveBeenCalledWith(7)
    expect(apiMock.getProduct).not.toHaveBeenCalled()

    // 商品信息完整回填
    const inputs = wrapper.findAll('input')
    expect((inputs[0].element as HTMLInputElement).value).toBe('iPhone 13')
    expect((inputs[1].element as HTMLInputElement).value).toBe('九成新，配件齐全')
  })

  it('保存：提交更新并返回详情页，全程不调用计数接口', async () => {
    apiMock.getProductForEdit.mockResolvedValue({ data: { ...productVO } })
    apiMock.updateProduct.mockResolvedValue({ data: { ...productVO, title: 'iPhone 13 Pro' } })
    const wrapper = mountPage()
    await flushPromises()

    const buttons = wrapper.findAll('button')
    await buttons[0].trigger('click') // 保存修改
    await flushPromises()

    expect(apiMock.updateProduct).toHaveBeenCalledTimes(1)
    const [id, payload] = apiMock.updateProduct.mock.calls[0]
    expect(id).toBe(7)
    expect(payload).toMatchObject({
      title: 'iPhone 13',
      description: '九成新，配件齐全',
      category: 'digital',
      condition: 'almost_new',
      original_price: 5999,
      price: 3999,
      images: ['/uploads/a.jpg', '/uploads/b.jpg']
    })
    expect(routerMock.push).toHaveBeenCalledWith('/products/7')
    expect(apiMock.getProduct).not.toHaveBeenCalled()
  })

  it('取消：直接返回详情页，不提交也不调用计数接口', async () => {
    apiMock.getProductForEdit.mockResolvedValue({ data: { ...productVO } })
    const wrapper = mountPage()
    await flushPromises()

    const buttons = wrapper.findAll('button')
    await buttons[1].trigger('click') // 取消
    await flushPromises()

    expect(routerMock.push).toHaveBeenCalledWith('/products/7')
    expect(apiMock.updateProduct).not.toHaveBeenCalled()
    expect(apiMock.getProduct).not.toHaveBeenCalled()
  })

  it('非卖家：接口 403 被拦截回详情页，表单不回填数据', async () => {
    apiMock.getProductForEdit.mockRejectedValue(new Error('商品编辑信息获取失败：只有卖家可编辑商品'))
    const wrapper = mountPage()
    await flushPromises()

    expect(routerMock.replace).toHaveBeenCalledWith('/products/7')
    expect(apiMock.updateProduct).not.toHaveBeenCalled()
    expect(apiMock.getProduct).not.toHaveBeenCalled()
    // 表单不回填任何商品数据
    const inputs = wrapper.findAll('input')
    for (const input of inputs) {
      expect((input.element as HTMLInputElement).value).toBe('')
    }
  })

  it('非卖家：即使拿到数据（seller_id 不符）也被页面拦截', async () => {
    apiMock.getProductForEdit.mockResolvedValue({ data: { ...productVO, seller_id: 2 } })
    mountPage()
    await flushPromises()

    expect(messageMock.error).toHaveBeenCalledWith('只有商品所属卖家可以编辑该商品')
    expect(routerMock.replace).toHaveBeenCalledWith('/products/7')
    expect(apiMock.getProduct).not.toHaveBeenCalled()
  })
})
