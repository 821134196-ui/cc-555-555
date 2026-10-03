<template>
  <div class="login-panel panel">
    <h2>🪑 工位搬迁协同系统</h2>
    <p class="muted">行政排座 · IT 核对网口/分机/显示器 · 员工现场确认</p>
    <div class="field">
      <label>选择账号登录（演示环境免密码）</label>
      <select v-model="username" @keyup.enter="login">
        <option value="" disabled>请选择…</option>
        <optgroup label="行政">
          <option value="admin">admin — 行政-林晓</option>
        </optgroup>
        <optgroup label="IT">
          <option value="it">it — IT-周工</option>
        </optgroup>
        <optgroup label="员工">
          <option value="wangwu">wangwu — 王五</option>
          <option value="zhaoliu">zhaoliu — 赵六</option>
          <option value="sunqi">sunqi — 孙七</option>
          <option value="chenba">chenba — 陈八</option>
          <option value="zhoujiu">zhoujiu — 周九</option>
          <option value="wushi">wushi — 吴十</option>
        </optgroup>
      </select>
    </div>
    <button class="btn btn-primary login-btn" :disabled="!username || loading" @click="login">
      {{ loading ? '登录中…' : '登 录' }}
    </button>
    <p v-if="err" class="err">{{ err }}</p>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { api, setToken } from '../api.js'

const emit = defineEmits(['logged'])
const username = ref('admin')
const loading = ref(false)
const err = ref('')

async function login() {
  if (!username.value) return
  loading.value = true
  err.value = ''
  try {
    const { token, user } = await api.login(username.value)
    setToken(token)
    emit('logged', user)
  } catch (e) {
    err.value = e.message
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-panel { width: 380px; padding: 30px 32px; }
.login-panel h2 { margin: 0 0 6px; font-size: 20px; }
.login-panel p { margin: 0 0 18px; }
.login-btn { width: 100%; padding: 10px; margin-top: 6px; font-size: 14px; }
.err { color: var(--danger); font-size: 13px; margin-top: 12px; }
</style>
