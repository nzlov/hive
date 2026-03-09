<template>
  <AdminShell>
    <section class="space-y-6">
      <div class="panel p-6 sm:p-8">
        <div class="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <p class="text-xs uppercase tracking-[0.35em] text-slate-400">Memory Browser</p>
            <h2 class="mt-3 text-3xl font-semibold text-ink">记忆浏览</h2>
            <p class="mt-2 text-sm text-slate-500">支持按标题、项目、标签、总结与正文复用记忆搜索逻辑进行筛选。</p>
          </div>
          <button class="ghost-btn" type="button" @click="loadMemories">刷新列表</button>
        </div>

        <form class="mt-6 grid gap-4 lg:grid-cols-[minmax(0,1fr)_180px_120px]" @submit.prevent="handleSearch">
          <label class="block space-y-2">
            <span class="text-sm font-medium text-slate-700">关键字搜索</span>
            <input
              v-model="keywordInput"
              class="field"
              placeholder="多个关键字用空格分隔，将按记忆搜索逻辑同时命中"
            />
          </label>
          <label class="block space-y-2">
            <span class="text-sm font-medium text-slate-700">每页条数</span>
            <select v-model.number="pageSize" class="field" @change="handlePageSizeChange">
              <option v-for="size in pageSizeOptions" :key="size" :value="size">{{ size }} 条/页</option>
            </select>
          </label>
          <div class="flex items-end gap-3">
            <button class="primary-btn flex-1" type="submit">搜索</button>
          </div>
        </form>

        <p v-if="errorMessage" class="mt-4 rounded-2xl bg-coral/10 px-4 py-3 text-sm text-coral">{{ errorMessage }}</p>
      </div>

      <section class="panel overflow-hidden">
        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-slate-200 text-sm">
            <thead class="bg-slate-50/80 text-left text-slate-500">
              <tr>
                <th class="px-5 py-4 font-medium">项目</th>
                <th class="px-5 py-4 font-medium">标题</th>
                <th class="px-5 py-4 font-medium">Tags</th>
                <th class="px-5 py-4 font-medium">总结</th>
                <th class="px-5 py-4 font-medium">创建人</th>
                <th class="px-5 py-4 font-medium">创建时间</th>
                <th class="px-5 py-4 font-medium">操作</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-slate-100 bg-white/90">
              <tr v-if="loading">
                <td class="px-5 py-10 text-center text-slate-400" colspan="7">正在加载记忆列表...</td>
              </tr>
              <tr v-else-if="!memories.length">
                <td class="px-5 py-10 text-center text-slate-400" colspan="7">暂无匹配的记忆</td>
              </tr>
              <tr v-for="item in memories" :key="item.id" class="align-top">
                <td class="px-5 py-4 font-medium text-ink">{{ item.project_name || '-' }}</td>
                <td class="px-5 py-4 text-ink">{{ item.title || '-' }}</td>
                <td class="px-5 py-4">
                  <div class="flex max-w-xs flex-wrap gap-2">
                    <span
                      v-for="tag in item.tags"
                      :key="`${item.id}-${tag}`"
                      class="rounded-full bg-mist px-3 py-1 text-xs text-slate-600"
                    >
                      {{ tag }}
                    </span>
                    <span v-if="!item.tags?.length" class="text-slate-300">-</span>
                  </div>
                </td>
                <td class="px-5 py-4 text-slate-600">
                  <p class="max-w-md whitespace-pre-wrap break-words">{{ item.summary || '-' }}</p>
                </td>
                <td class="px-5 py-4 text-slate-500">{{ item.userid || '-' }}</td>
                <td class="px-5 py-4 text-slate-500">{{ formatDate(item.created_at) }}</td>
                <td class="px-5 py-4">
                  <div class="flex flex-wrap gap-3">
                    <button class="ghost-btn" type="button" @click="openDetail(item.id)">查看</button>
                    <button class="danger-btn" type="button" @click="handleDelete(item)">删除</button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div class="flex flex-col gap-4 border-t border-slate-200 px-5 py-4 sm:flex-row sm:items-center sm:justify-between">
          <p class="text-sm text-slate-500">共 {{ total }} 条，当前第 {{ page }} / {{ totalPage || 1 }} 页</p>
          <div class="flex flex-wrap gap-3">
            <button class="ghost-btn" type="button" :disabled="page <= 1 || loading" @click="changePage(page - 1)">上一页</button>
            <button class="ghost-btn" type="button" :disabled="page >= totalPage || loading || totalPage === 0" @click="changePage(page + 1)">下一页</button>
          </div>
        </div>
      </section>
    </section>

    <div v-if="detailVisible" class="fixed inset-0 z-40 flex justify-end bg-slate-950/35">
      <button class="absolute inset-0 cursor-default" type="button" aria-label="关闭详情" @click="closeDetail" />
      <aside class="relative z-10 flex h-full w-full max-w-2xl flex-col bg-white shadow-2xl">
        <div class="border-b border-slate-200 px-6 py-5">
          <div class="flex items-start justify-between gap-4">
            <div>
              <p class="text-xs uppercase tracking-[0.35em] text-slate-400">Memory Detail</p>
              <h3 class="mt-3 text-2xl font-semibold text-ink">{{ detail?.title || '记忆详情' }}</h3>
            </div>
            <button class="ghost-btn" type="button" @click="closeDetail">关闭</button>
          </div>
        </div>

        <div class="flex-1 overflow-y-auto px-6 py-6">
          <p v-if="detailLoading" class="text-sm text-slate-400">正在加载详情...</p>
          <p v-else-if="detailError" class="rounded-2xl bg-coral/10 px-4 py-3 text-sm text-coral">{{ detailError }}</p>
          <div v-else-if="detail" class="space-y-6">
            <div class="grid gap-4 sm:grid-cols-2">
              <article class="rounded-3xl border border-slate-200 bg-mist/50 p-4">
                <p class="text-xs uppercase tracking-[0.3em] text-slate-400">项目</p>
                <p class="mt-3 text-sm font-semibold text-ink">{{ detail.project_name || '-' }}</p>
              </article>
              <article class="rounded-3xl border border-slate-200 bg-mist/50 p-4">
                <p class="text-xs uppercase tracking-[0.3em] text-slate-400">分支</p>
                <p class="mt-3 text-sm font-semibold text-ink">{{ detail.git_branch || '-' }}</p>
              </article>
              <article class="rounded-3xl border border-slate-200 bg-mist/50 p-4">
                <p class="text-xs uppercase tracking-[0.3em] text-slate-400">类型</p>
                <p class="mt-3 text-sm font-semibold text-ink">{{ detail.type || '-' }}</p>
              </article>
              <article class="rounded-3xl border border-slate-200 bg-mist/50 p-4">
                <p class="text-xs uppercase tracking-[0.3em] text-slate-400">创建人</p>
                <p class="mt-3 break-all text-sm font-semibold text-ink">{{ detail.userid || '-' }}</p>
              </article>
            </div>

            <section class="rounded-3xl border border-slate-200 p-5">
              <p class="text-xs uppercase tracking-[0.3em] text-slate-400">Tags</p>
              <div class="mt-4 flex flex-wrap gap-2">
                <span v-for="tag in detail.tags" :key="`detail-${tag}`" class="rounded-full bg-mist px-3 py-1 text-xs text-slate-600">{{ tag }}</span>
                <span v-if="!detail.tags?.length" class="text-sm text-slate-300">-</span>
              </div>
            </section>

            <section class="rounded-3xl border border-slate-200 p-5">
              <p class="text-xs uppercase tracking-[0.3em] text-slate-400">总结</p>
              <p class="mt-4 whitespace-pre-wrap break-words text-sm leading-7 text-slate-700">{{ detail.summary || '-' }}</p>
            </section>

            <section class="rounded-3xl border border-slate-200 p-5">
              <p class="text-xs uppercase tracking-[0.3em] text-slate-400">正文</p>
              <pre class="mt-4 overflow-x-auto whitespace-pre-wrap break-words rounded-2xl bg-slate-950 px-4 py-4 text-sm leading-7 text-slate-100">{{ detail.content || '-' }}</pre>
            </section>

            <section class="grid gap-4 sm:grid-cols-2">
              <article class="rounded-3xl border border-slate-200 p-4">
                <p class="text-xs uppercase tracking-[0.3em] text-slate-400">添加时间</p>
                <p class="mt-3 text-sm text-slate-700">{{ formatDate(detail.created_at) }}</p>
              </article>
              <article class="rounded-3xl border border-slate-200 p-4">
                <p class="text-xs uppercase tracking-[0.3em] text-slate-400">记忆时间戳</p>
                <p class="mt-3 text-sm text-slate-700">{{ detail.timestamp || '-' }}</p>
              </article>
            </section>
          </div>
        </div>
      </aside>
    </div>
  </AdminShell>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import AdminShell from '../components/AdminShell.vue'
import { deleteMemory, fetchMemories, fetchMemoryDetail } from '../lib/api'

const memories = ref([])
const loading = ref(false)
const errorMessage = ref('')
const keywordInput = ref('')
const page = ref(1)
const pageSize = ref(10)
const total = ref(0)
const totalPage = ref(0)
const detailVisible = ref(false)
const detailLoading = ref(false)
const detailError = ref('')
const detail = ref(null)
const pageSizeOptions = [10, 15, 20, 30, 50]

// buildQueries 统一按空白拆分关键字，确保后台列表与记忆搜索接口共享同样的多词匹配输入形式。
function buildQueries() {
  return keywordInput.value
    .split(/\s+/)
    .map((item) => item.trim())
    .filter(Boolean)
}

// loadMemories 统一拉取分页结果，避免搜索、翻页和删除后出现不同刷新路径。
async function loadMemories() {
  loading.value = true
  errorMessage.value = ''
  try {
    const response = await fetchMemories(page.value, pageSize.value, buildQueries())
    memories.value = response.items || []
    total.value = response.total || 0
    totalPage.value = response.total_page || 0
    page.value = response.page || 1
    pageSize.value = response.page_size || pageSize.value
  } catch (error) {
    errorMessage.value = error.message
  } finally {
    loading.value = false
  }
}

// handleSearch 在用户显式提交后重置到第一页，避免旧页码导致新结果看起来为空。
async function handleSearch() {
  page.value = 1
  await loadMemories()
}

// handlePageSizeChange 修改分页大小后回到第一页，避免页码超界影响体验。
async function handlePageSizeChange() {
  page.value = 1
  await loadMemories()
}

// changePage 统一处理前后翻页，避免边界判断散落在模板中。
async function changePage(nextPage) {
  if (nextPage < 1 || (totalPage.value > 0 && nextPage > totalPage.value)) {
    return
  }
  page.value = nextPage
  await loadMemories()
}

// openDetail 按需加载详情抽屉，避免列表首屏就携带大量正文内容。
async function openDetail(id) {
  detailVisible.value = true
  detailLoading.value = true
  detailError.value = ''
  detail.value = null
  try {
    const response = await fetchMemoryDetail(id)
    detail.value = response.item || null
  } catch (error) {
    detailError.value = error.message
  } finally {
    detailLoading.value = false
  }
}

// closeDetail 统一回收抽屉状态，避免上一次详情残留到下一次查看。
function closeDetail() {
  detailVisible.value = false
  detailLoading.value = false
  detailError.value = ''
  detail.value = null
}

// handleDelete 删除前先做确认，并在删除详情项时同步关闭抽屉避免展示脏数据。
async function handleDelete(item) {
  if (!window.confirm(`确认删除记忆《${item.title || item.id}》吗？删除后会同步清理向量。`)) {
    return
  }
  errorMessage.value = ''
  try {
    await deleteMemory(item.id)
    if (detail.value?.id === item.id) {
      closeDetail()
    }
    if (memories.value.length === 1 && page.value > 1) {
      page.value -= 1
    }
    await loadMemories()
  } catch (error) {
    errorMessage.value = error.message
  }
}

// formatDate 统一处理后台时间字符串，避免空值或非法值直接显示影响可读性。
function formatDate(value) {
  if (!value) {
    return '-'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString('zh-CN', { hour12: false })
}

onMounted(() => {
  loadMemories()
})
</script>
