<template>
  <el-card class="wrap" v-loading="pageLoading">
    <h2>编辑商品</h2>
    <el-form :model="form" label-width="90px">
      <el-form-item label="商品名称" required>
        <el-input v-model="form.title" maxlength="128" show-word-limit placeholder="例如：iPhone 13 128G 几乎全新" />
      </el-form-item>
      <el-form-item label="描述" required>
        <el-input v-model="form.description" type="textarea" :rows="4" placeholder="成色、入手渠道、使用感受等" />
      </el-form-item>
      <el-form-item label="分类" required>
        <el-select v-model="form.category" placeholder="选择分类">
          <el-option v-for="(text, key) in ProductCategoryText" :key="key" :label="text" :value="key" />
        </el-select>
      </el-form-item>
      <el-form-item label="成色" required>
        <el-select v-model="form.condition" placeholder="选择成色">
          <el-option v-for="(text, key) in ProductConditionText" :key="key" :label="text" :value="key" />
        </el-select>
      </el-form-item>
      <el-form-item label="原价" required>
        <el-input-number v-model="form.original_price" :min="0" :precision="2" />
      </el-form-item>
      <el-form-item label="售价" required>
        <el-input-number v-model="form.price" :min="0.01" :precision="2" />
      </el-form-item>
      <el-form-item label="商品图片">
        <ImageUploader v-model="form.images" />
      </el-form-item>
      <el-form-item>
        <el-button type="primary" :loading="loading" @click="submit">保存修改</el-button>
        <el-button @click="$router.push(`/products/${productId}`)">取消</el-button>
      </el-form-item>
    </el-form>
  </el-card>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import * as productApi from '../api/product'
import ImageUploader from '../components/ImageUploader.vue'
import { useUserStore } from '../stores/userStore'
import { ProductCategoryText, ProductConditionText } from '../constants'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const productId = Number(route.params.id)
const pageLoading = ref(false)
const loading = ref(false)
const form = reactive({
  title: '',
  description: '',
  category: '',
  condition: '',
  original_price: 0,
  price: 0,
  images: [] as string[]
})

onMounted(async () => {
  pageLoading.value = true
  try {
    if (!userStore.user) await userStore.fetchProfile()
    // 编辑回填走专用接口，不计浏览量
    const res: any = await productApi.getProductForEdit(productId)
    const p = res.data
    if (p.seller_id !== userStore.user?.id) {
      ElMessage.error('只有商品所属卖家可以编辑该商品')
      router.replace(`/products/${productId}`)
      return
    }
    form.title = p.title
    form.description = p.description
    form.category = p.category
    form.condition = p.condition
    form.original_price = p.original_price
    form.price = p.price
    form.images = [...(p.images || [])]
  } catch {
    // 非卖家或商品不存在：拦截器已提示原因，回退到详情页
    router.replace(`/products/${productId}`)
  } finally {
    pageLoading.value = false
  }
})

async function submit() {
  if (!form.title || !form.description || !form.category || !form.condition) {
    ElMessage.warning('请填写完整商品信息')
    return
  }
  if (form.price <= 0) {
    ElMessage.warning('售价必须大于 0')
    return
  }
  loading.value = true
  try {
    await productApi.updateProduct(productId, { ...form })
    ElMessage.success('商品已更新')
    router.push(`/products/${productId}`)
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.wrap {
  max-width: 760px;
  margin: 0 auto;
}
</style>
