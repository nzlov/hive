import { clearSession, getToken, saveSession } from './auth'

// request 统一封装管理端请求，避免每个页面重复拼接 JWT 请求头和错误处理。
async function request(url, options = {}) {
  const headers = new Headers(options.headers || {})
  if (!headers.has('Content-Type') && options.body) {
    headers.set('Content-Type', 'application/json')
  }
  const token = getToken()
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }
  const response = await fetch(url, { ...options, headers })
  const payload = await response.json().catch(() => ({}))
  if (!response.ok) {
    if (response.status === 401) {
      clearSession()
    }
    throw new Error(payload.error || '请求失败')
  }
  return payload
}

// login 统一处理登录响应，确保令牌与当前用户资料同步写入本地状态。
export async function login(username, password) {
  const payload = await request('/api/v1/users/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  })
  saveSession(payload.token, payload.user)
  return payload
}

// fetchCurrentUser 用于刷新当前会话身份，避免页面直读本地缓存导致信息过期。
export function fetchCurrentUser() {
  return request('/api/v1/users/me')
}

// fetchUsers 拉取用户分页列表，确保搜索和翻页共享同一查询协议。
export function fetchUsers(page = 1, pageSize = 10, keyword = '') {
  const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
  if (String(keyword || '').trim()) {
    params.set('keyword', String(keyword).trim())
  }
  return request(`/api/v1/admin/users?${params.toString()}`)
}

// createUser 创建新用户并返回完整对象，便于页面直接更新列表而不是再次推断生成字段。
export function createUser(payload) {
  return request('/api/v1/admin/users', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

// updateUser 更新用户资料、密码或 token，集中复用同一接口约定。
export function updateUser(id, payload) {
  return request(`/api/v1/admin/users/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

// deleteUser 删除指定用户，保持页面层无需关心删除接口的空响应细节。
export function deleteUser(id) {
  return request(`/api/v1/admin/users/${id}`, { method: 'DELETE' })
}

// fetchMemories 拉取后台记忆分页列表，确保管理页与服务端共享同一筛选协议。
export function fetchMemories(page = 1, pageSize = 10, queries = []) {
  const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
  queries.forEach((query) => {
    if (String(query || '').trim()) {
      params.append('queries', String(query).trim())
    }
  })
  return request(`/api/v1/memories?${params.toString()}`)
}

// fetchMemoryDetail 读取单条记忆详情，避免列表页为了抽屉展示预取整段正文。
export function fetchMemoryDetail(id) {
  return request(`/api/v1/memories/${id}`)
}

// updateMemory 更新单条记忆，并由后端同步刷新向量数据保持检索一致。
export function updateMemory(id, payload) {
  return request(`/api/v1/admin/memories/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

// deleteMemory 删除记忆并触发后端同步清理向量，保持索引与主记录一致。
export function deleteMemory(id) {
  return request(`/api/v1/admin/memories/${id}`, { method: 'DELETE' })
}
