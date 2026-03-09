const TOKEN_KEY = 'hive_admin_token'
const USER_KEY = 'hive_admin_user'

// getToken 统一读取本地 JWT，避免路由守卫和请求封装分别维护存储键名。
export function getToken() {
  return window.localStorage.getItem(TOKEN_KEY) || ''
}

// saveSession 统一持久化登录状态，避免页面刷新后管理界面丢失身份信息。
export function saveSession(token, user) {
  window.localStorage.setItem(TOKEN_KEY, token)
  window.localStorage.setItem(USER_KEY, JSON.stringify(user || {}))
}

// clearSession 用于退出登录时清理管理端状态，避免旧令牌残留影响后续调试。
export function clearSession() {
  window.localStorage.removeItem(TOKEN_KEY)
  window.localStorage.removeItem(USER_KEY)
}

// getStoredUser 统一读取当前用户资料，让导航栏和鉴权逻辑共享同一状态来源。
export function getStoredUser() {
  try {
    return JSON.parse(window.localStorage.getItem(USER_KEY) || '{}')
  } catch {
    return {}
  }
}
