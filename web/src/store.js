import { reactive } from 'vue'

// 登录状态：token + 当前用户，持久化到 localStorage
export const auth = reactive({
  token: localStorage.getItem('token') || '',
  user: JSON.parse(localStorage.getItem('user') || 'null'),
  login(token, user) {
    this.token = token
    this.user = user
    localStorage.setItem('token', token)
    localStorage.setItem('user', JSON.stringify(user))
  },
  logout() {
    this.token = ''
    this.user = null
    localStorage.removeItem('token')
    localStorage.removeItem('user')
  },
})

export const roleLabel = { admin: '行政', it: 'IT', employee: '员工' }
