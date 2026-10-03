<template>
  <div class="login-wrap">
    <div class="card login-card">
      <h2 style="text-align:center; margin-bottom: 6px">工位搬迁与网络开通系统</h2>
      <p class="muted" style="text-align:center; margin-bottom: 18px">演示环境：选择账号直接登录</p>
      <div v-if="error" class="alert error">{{ error }}</div>
      <div class="user-pick">
        <button v-for="u in users" :key="u.id" @click="login(u)">
          <span>{{ u.name }}</span>
          <span class="badge" :class="badgeClass(u.role)">{{ roleLabel[u.role] }}</span>
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { request } from '../api'
import { auth, roleLabel } from '../store'

const router = useRouter()
const users = ref([])
const error = ref('')

onMounted(async () => {
  try {
    users.value = await request('/api/auth/users')
  } catch (e) {
    error.value = '无法连接服务器：' + e.message
  }
})

function badgeClass(role) {
  return { admin: 'pending', it: 'ready', employee: 'completed' }[role] || 'pending'
}

async function login(u) {
  error.value = ''
  try {
    const data = await request('/api/login', { method: 'POST', body: { username: u.username } })
    auth.login(data.token, data.user)
    router.push('/')
  } catch (e) {
    error.value = e.message
  }
}
</script>
