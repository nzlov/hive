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
                <th class="px-5 py-4 font-medium">总结</th>
                <th class="px-5 py-4 font-medium">创建人</th>
                <th class="px-5 py-4 font-medium">置信度</th>
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
                <td class="px-5 py-4 text-ink">
                  <div class="max-w-[22rem] min-w-[16rem] space-y-1">
                    <p class="text-xs uppercase tracking-[0.24em] text-slate-400">项目</p>
                    <p class="whitespace-pre-wrap break-all font-semibold leading-6">{{ item.project_name || '-' }}</p>
                  </div>
                </td>
                <td class="px-5 py-4 text-ink">
                  <div class="max-w-[16rem] space-y-3">
                    <div class="flex flex-wrap items-center gap-2">
                      <p class="break-words font-medium leading-6">{{ item.title || '-' }}</p>
                      <span
                        v-if="item.type"
                        class="inline-flex h-7 w-7 items-center justify-center rounded-full border border-slate-200 bg-slate-50 text-slate-500"
                        :class="getTypeIconClass(item.type)"
                        :aria-label="`${formatTypeLabel(item.type)} 类型`"
                      >
                        <svg v-if="item.type === 'error'" viewBox="0 0 24 24" aria-hidden="true" class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8">
                          <circle cx="12" cy="12" r="9" />
                          <path d="M12 7.5v5" />
                          <path d="M12 16.5h.01" />
                        </svg>
                        <svg v-else viewBox="0 0 24 24" aria-hidden="true" class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8">
                          <path d="M9 12.75 11.25 15 15 9.75" />
                          <circle cx="12" cy="12" r="9" />
                        </svg>
                        <span class="sr-only">{{ formatTypeLabel(item.type) }}</span>
                      </span>
                    </div>
                    <div v-if="item.tags?.length" class="flex flex-wrap gap-2">
                      <span
                        v-for="tag in item.tags"
                        :key="`${item.id}-${tag}`"
                        class="rounded-full bg-mist px-3 py-1 text-xs text-slate-600"
                      >
                        {{ tag }}
                      </span>
                    </div>
                  </div>
                </td>
                <td class="px-5 py-4 text-slate-600">
                  <p class="max-w-md whitespace-pre-wrap break-words">{{ item.summary || '-' }}</p>
                </td>
                <td class="px-5 py-4 text-slate-500">
                  <p class="max-w-[7rem] break-words leading-6">{{ item.creator_name || item.userid || '-' }}</p>
                </td>
                <td class="px-5 py-4 text-slate-500">{{ formatConfidence(item.confidence) }}</td>
                <td class="px-5 py-4 text-slate-500">{{ formatDate(item.created_at) }}</td>
                <td class="px-5 py-4">
                  <div class="flex flex-wrap items-center gap-2">
                    <button class="icon-btn" type="button" aria-label="查看详情" @click="openDetail(item.id)">
                      <svg viewBox="0 0 24 24" aria-hidden="true" class="h-4 w-4" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8">
                        <path d="M2 12s3.6-6 10-6 10 6 10 6-3.6 6-10 6-10-6-10-6Z" />
                        <circle cx="12" cy="12" r="3" />
                      </svg>
                    </button>
                    <button v-if="user.is_admin" class="icon-btn" type="button" aria-label="编辑记忆" @click="openEdit(item.id)">
                      <svg viewBox="0 0 24 24" aria-hidden="true" class="h-4 w-4" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8">
                        <path d="M12 20h9" />
                        <path d="m16.5 3.5 4 4L8 20l-5 1 1-5 12.5-12.5Z" />
                      </svg>
                    </button>
                    <button v-if="user.is_admin" class="icon-btn icon-btn--danger" type="button" aria-label="删除记忆" @click="handleDelete(item)">
                      <svg viewBox="0 0 24 24" aria-hidden="true" class="h-4 w-4" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8">
                        <path d="M3 6h18" />
                        <path d="M8 6V4.5A1.5 1.5 0 0 1 9.5 3h5A1.5 1.5 0 0 1 16 4.5V6" />
                        <path d="M19 6l-1 13.5A1.5 1.5 0 0 1 16.5 21h-9A1.5 1.5 0 0 1 6 19.5L5 6" />
                        <path d="M10 11v6" />
                        <path d="M14 11v6" />
                      </svg>
                    </button>
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
            <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              <article class="rounded-3xl border border-slate-200 bg-mist/50 p-4 sm:col-span-2 lg:col-span-3">
                <p class="text-xs uppercase tracking-[0.3em] text-slate-400">项目</p>
                <p class="mt-3 whitespace-pre-wrap break-all text-sm font-semibold leading-6 text-ink">{{ detail.project_name || '-' }}</p>
              </article>
              <article class="rounded-3xl border border-slate-200 bg-mist/50 p-4">
                <p class="text-xs uppercase tracking-[0.3em] text-slate-400">分支</p>
                <p class="mt-3 break-all text-sm font-semibold leading-6 text-ink">{{ detail.git_branch || '-' }}</p>
              </article>
              <article class="rounded-3xl border border-slate-200 bg-mist/50 p-4">
                <p class="text-xs uppercase tracking-[0.3em] text-slate-400">类型</p>
                <div class="mt-3">
                  <span class="type-badge" :class="getTypeBadgeClass(detail.type)">{{ formatTypeLabel(detail.type) }}</span>
                </div>
              </article>
              <article class="rounded-3xl border border-slate-200 bg-mist/50 p-4">
                <p class="text-xs uppercase tracking-[0.3em] text-slate-400">创建人</p>
                <p class="mt-3 break-all text-sm font-semibold leading-6 text-ink">{{ detail.creator_name || detail.userid || '-' }}</p>
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

    <div v-if="editVisible" class="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/45 px-4 py-8">
      <button class="absolute inset-0 cursor-default" type="button" aria-label="关闭编辑" @click="closeEdit" />
      <section class="relative z-10 max-h-full w-full max-w-4xl overflow-y-auto rounded-[2rem] bg-white p-6 shadow-2xl sm:p-8">
        <div class="flex items-start justify-between gap-4">
          <div>
            <p class="text-xs uppercase tracking-[0.35em] text-slate-400">Memory Editor</p>
            <h3 class="mt-3 text-2xl font-semibold text-ink">编辑记忆</h3>
            <p class="mt-2 text-sm text-slate-500">仅允许修改标题、标签、总结和正文，保存后会同步更新向量数据。</p>
          </div>
          <button class="ghost-btn" type="button" :disabled="editSaving" @click="closeEdit">关闭</button>
        </div>

        <p v-if="editLoading" class="mt-6 text-sm text-slate-400">正在加载编辑数据...</p>
        <p v-else-if="editError" class="mt-6 rounded-2xl bg-coral/10 px-4 py-3 text-sm text-coral">{{ editError }}</p>

        <form v-else class="mt-6 space-y-6" @submit.prevent="handleSaveEdit">
          <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
            <article class="rounded-3xl border border-slate-200 bg-mist/50 p-4 sm:col-span-2 xl:col-span-3">
              <p class="text-xs uppercase tracking-[0.3em] text-slate-400">项目</p>
              <p class="mt-3 whitespace-pre-wrap break-all text-sm font-semibold leading-6 text-ink">{{ editReadonly.project_name || '-' }}</p>
            </article>
            <article class="rounded-3xl border border-slate-200 bg-mist/50 p-4">
              <p class="text-xs uppercase tracking-[0.3em] text-slate-400">分支</p>
              <p class="mt-3 break-all text-sm font-semibold leading-6 text-ink">{{ editReadonly.git_branch || '-' }}</p>
            </article>
            <article class="rounded-3xl border border-slate-200 bg-mist/50 p-4">
              <p class="text-xs uppercase tracking-[0.3em] text-slate-400">类型</p>
              <div class="mt-3">
                <span class="type-badge" :class="getTypeBadgeClass(editReadonly.type)">{{ formatTypeLabel(editReadonly.type) }}</span>
              </div>
            </article>
            <article class="rounded-3xl border border-slate-200 bg-mist/50 p-4">
              <p class="text-xs uppercase tracking-[0.3em] text-slate-400">创建人</p>
              <p class="mt-3 break-all text-sm font-semibold leading-6 text-ink">{{ editReadonly.creator_name || editReadonly.userid || '-' }}</p>
            </article>
          </div>

          <label class="block space-y-2">
            <span class="text-sm font-medium text-slate-700">标题</span>
            <input v-model="editForm.title" class="field" placeholder="请输入记忆标题" :disabled="editSaving" />
          </label>

          <label class="block space-y-2">
            <span class="text-sm font-medium text-slate-700">标签</span>
            <input
              v-model="editTagsInput"
              class="field"
              placeholder="多个标签用空格、逗号或换行分隔"
              :disabled="editSaving"
            />
          </label>

          <label class="block space-y-2">
            <span class="text-sm font-medium text-slate-700">总结</span>
            <textarea
              v-model="editForm.summary"
              class="field min-h-28 whitespace-pre-wrap break-words leading-7"
              placeholder="请输入总结"
              :disabled="editSaving"
            />
          </label>

          <label class="block space-y-2">
            <span class="text-sm font-medium text-slate-700">正文</span>
            <textarea
              v-model="editForm.content"
              class="field min-h-72 font-mono text-sm"
              placeholder="请输入完整正文"
              :disabled="editSaving"
            />
          </label>

          <div class="flex flex-wrap justify-end gap-3">
            <button class="ghost-btn" type="button" :disabled="editSaving" @click="closeEdit">取消</button>
            <button class="primary-btn" type="submit" :disabled="editSaving">{{ editSaving ? '保存中...' : '保存修改' }}</button>
          </div>
        </form>
      </section>
    </div>
  </AdminShell>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import AdminShell from '../components/AdminShell.vue'
import { deleteMemory, fetchMemories, fetchMemoryDetail, updateMemory } from '../lib/api'
import { getStoredUser } from '../lib/auth'

const user = getStoredUser()
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
const editVisible = ref(false)
const editLoading = ref(false)
const editSaving = ref(false)
const editError = ref('')
const editMemoryID = ref(null)
const editTagsInput = ref('')
const editForm = ref({ title: '', summary: '', content: '' })
const editReadonly = ref({})
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

// buildTagList 统一把输入框内容拆成标签数组，避免不同分隔符导致前后端保存格式漂移。
function buildTagList(value) {
  return String(value || '')
    .split(/[\s,，\n]+/)
    .map((item) => item.trim())
    .filter(Boolean)
}

// fillEditForm 用详情数据初始化编辑表单，确保提交字段和后端可编辑范围一致。
function fillEditForm(item) {
  editMemoryID.value = item?.id || null
  editReadonly.value = item || {}
  editForm.value = {
    title: item?.title || '',
    summary: item?.summary || '',
    content: item?.content || '',
  }
  editTagsInput.value = (item?.tags || []).join(' ')
}

// openEdit 编辑前先拉完整详情，避免列表页缺少正文导致误覆盖未展示内容。
async function openEdit(id) {
  editVisible.value = true
  editLoading.value = true
  editSaving.value = false
  editError.value = ''
  fillEditForm(null)
  try {
    const response = await fetchMemoryDetail(id)
    fillEditForm(response.item || null)
    if (!response.item) {
      throw new Error('未获取到记忆详情')
    }
  } catch (error) {
    editError.value = error.message
  } finally {
    editLoading.value = false
  }
}

// closeEdit 统一回收编辑弹窗状态，避免上一次编辑内容残留到下一条记录。
function closeEdit() {
  if (editSaving.value) {
    return
  }
  editVisible.value = false
  editLoading.value = false
  editError.value = ''
  editMemoryID.value = null
  editTagsInput.value = ''
  editForm.value = { title: '', summary: '', content: '' }
  editReadonly.value = {}
}

// handleSaveEdit 提交编辑内容，并在成功后同步刷新列表和当前详情视图。
async function handleSaveEdit() {
  if (!editMemoryID.value) {
    editError.value = '缺少可编辑的记忆ID'
    return
  }
  editSaving.value = true
  editError.value = ''
  try {
    const response = await updateMemory(editMemoryID.value, {
      title: editForm.value.title,
      tags: buildTagList(editTagsInput.value),
      summary: editForm.value.summary,
      content: editForm.value.content,
    })
    if (detail.value?.id === editMemoryID.value) {
      detail.value = response.item || null
    }
    await loadMemories()
    editSaving.value = false
    closeEdit()
  } catch (error) {
    editError.value = error.message
  } finally {
    editSaving.value = false
  }
}

// formatConfidence 统一格式化搜索置信度，避免未搜索时把空值误显示成 0。
function formatConfidence(value) {
  if (typeof value !== 'number' || Number.isNaN(value)) {
    return '-'
  }
  return `${(value * 100).toFixed(value >= 0.995 ? 0 : 1)}%`
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

// formatTypeLabel 统一兜底记忆类型文案，避免空值直接露出无意义占位风格。
function formatTypeLabel(value) {
  return value || '-'
}

// getTypeBadgeClass 按类型返回徽标语义色，帮助列表中快速区分不同记忆来源。
function getTypeBadgeClass(value) {
  if (value === 'summary') {
    return 'type-badge--summary'
  }
  if (value === 'error') {
    return 'type-badge--error'
  }
  return 'type-badge--neutral'
}

// getTypeIconClass 为标题后的类型小图标提供语义色，避免列表依赖文字占据额外空间。
function getTypeIconClass(value) {
  if (value === 'summary') {
    return 'border-pine/20 bg-pine/10 text-pine'
  }
  if (value === 'error') {
    return 'border-coral/20 bg-coral/10 text-coral'
  }
  return 'border-slate-200 bg-slate-50 text-slate-500'
}

onMounted(() => {
  loadMemories()
})
</script>
