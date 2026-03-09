<template>
  <AdminShell>
    <div class="grid gap-6 xl:grid-cols-[0.95fr_1.35fr]">
      <section class="panel p-6 sm:p-8">
        <div class="flex items-center justify-between gap-4">
          <div>
            <p class="text-xs uppercase tracking-[0.35em] text-slate-400">User Editor</p>
            <h2 class="mt-3 text-2xl font-semibold text-ink">{{ editingId ? '编辑用户' : '新增用户' }}</h2>
          </div>
          <button v-if="editingId" class="ghost-btn" type="button" @click="resetForm">取消编辑</button>
        </div>
        <form class="mt-6 space-y-4" @submit.prevent="handleSubmit">
          <label class="block space-y-2">
            <span class="text-sm font-medium text-slate-700">用户名</span>
            <input v-model="form.username" class="field" placeholder="请输入用户名" />
          </label>
          <label class="block space-y-2">
            <span class="text-sm font-medium text-slate-700">真实名称</span>
            <input v-model="form.real_name" class="field" placeholder="请输入真实名称" />
          </label>
          <label class="block space-y-2">
            <span class="text-sm font-medium text-slate-700">密码{{ editingId ? '（留空则不修改）' : '' }}</span>
            <input v-model="form.password" class="field" type="password" placeholder="请输入密码" />
          </label>
          <label class="flex items-center gap-3 rounded-2xl border border-slate-200 bg-mist/60 px-4 py-3 text-sm text-slate-700">
            <input v-model="form.is_admin" class="h-4 w-4 rounded border-slate-300 text-ink" type="checkbox" />
            管理员用户
          </label>
          <label v-if="editingId" class="flex items-center gap-3 rounded-2xl border border-slate-200 bg-mist/60 px-4 py-3 text-sm text-slate-700">
            <input v-model="form.regenerate_token" class="h-4 w-4 rounded border-slate-300 text-ink" type="checkbox" />
            重新生成 API Token
          </label>
          <p v-if="errorMessage" class="rounded-2xl bg-coral/10 px-4 py-3 text-sm text-coral">{{ errorMessage }}</p>
          <button class="primary-btn w-full" type="submit" :disabled="submitting">
            {{ submitting ? '提交中...' : editingId ? '保存修改' : '创建用户' }}
          </button>
        </form>
      </section>

      <section class="panel p-6 sm:p-8">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <p class="text-xs uppercase tracking-[0.35em] text-slate-400">User Directory</p>
            <h2 class="mt-3 text-2xl font-semibold text-ink">用户列表</h2>
          </div>
          <button class="ghost-btn" type="button" @click="loadUsers">刷新列表</button>
        </div>
        <div class="mt-6 space-y-4">
          <article v-for="item in users" :key="item.id" class="rounded-3xl border border-slate-200 bg-white p-5">
            <div class="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
              <div class="min-w-0 space-y-2">
                <div class="flex flex-wrap items-center gap-2">
                  <h3 class="text-lg font-semibold text-ink">{{ item.real_name || item.username }}</h3>
                  <span class="rounded-full bg-mist px-3 py-1 text-xs text-slate-600">{{ item.is_admin ? '管理员' : '普通用户' }}</span>
                </div>
                <p class="text-sm text-slate-500">账号：{{ item.username }}</p>
                <p class="break-all text-xs text-slate-400">UUID：{{ item.userid }}</p>
                <p class="break-all rounded-2xl bg-mist px-3 py-3 text-xs text-pine">API Token：{{ item.apitoken }}</p>
              </div>
              <div class="flex shrink-0 flex-wrap gap-3">
                <button class="ghost-btn" type="button" @click="startEdit(item)">编辑</button>
                <button class="danger-btn" type="button" @click="handleDelete(item)">删除</button>
              </div>
            </div>
          </article>
        </div>
      </section>
    </div>
  </AdminShell>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import AdminShell from '../components/AdminShell.vue'
import { createUser, deleteUser, fetchUsers, updateUser } from '../lib/api'

const users = ref([])
const editingId = ref(0)
const submitting = ref(false)
const errorMessage = ref('')
const form = reactive({
  username: '',
  real_name: '',
  password: '',
  is_admin: false,
  regenerate_token: false,
})

// resetForm 统一清空编辑状态，避免新增和编辑共用表单时残留旧值。
function resetForm() {
  editingId.value = 0
  errorMessage.value = ''
  form.username = ''
  form.real_name = ''
  form.password = ''
  form.is_admin = false
  form.regenerate_token = false
}

// loadUsers 统一加载后台用户列表，保证新增、编辑和删除后都走同一刷新路径。
async function loadUsers() {
  const response = await fetchUsers()
  users.value = response.items || []
}

// startEdit 把选中的用户加载到表单，避免在列表行内维护多份编辑状态。
function startEdit(item) {
  editingId.value = item.id
  errorMessage.value = ''
  form.username = item.username
  form.real_name = item.real_name
  form.password = ''
  form.is_admin = item.is_admin
  form.regenerate_token = false
}

// handleSubmit 统一提交新增或更新请求，保证成功后列表和表单状态同步刷新。
async function handleSubmit() {
  submitting.value = true
  errorMessage.value = ''
  try {
    if (editingId.value) {
      await updateUser(editingId.value, { ...form })
    } else {
      await createUser({ ...form })
    }
    await loadUsers()
    resetForm()
  } catch (error) {
    errorMessage.value = error.message
  } finally {
    submitting.value = false
  }
}

// handleDelete 在删除前进行浏览器确认，避免误删用户导致 token 立即失效。
async function handleDelete(item) {
  if (!window.confirm(`确认删除用户 ${item.username} 吗？`)) {
    return
  }
  try {
    await deleteUser(item.id)
    await loadUsers()
    if (editingId.value === item.id) {
      resetForm()
    }
  } catch (error) {
    errorMessage.value = error.message
  }
}

onMounted(() => {
  loadUsers().catch((error) => {
    errorMessage.value = error.message
  })
})
</script>
