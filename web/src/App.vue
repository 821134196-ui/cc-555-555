<template>
  <div>
    <div v-if="auth.user" class="navbar">
      <div class="brand">工位搬迁与网络开通系统</div>
      <nav>
        <router-link to="/" exact-active-class="active">搬迁单</router-link>
        <router-link to="/floor" active-class="active">楼层图</router-link>
      </nav>
      <div class="who">
        <span>{{ auth.user.name }}</span>
        <span class="role-tag">{{ roleLabel[auth.user.role] }}</span>
        <a href="#" style="color:#94a3b8" @click.prevent="logout">退出</a>
      </div>
    </div>
    <router-view />
  </div>
</template>

<script setup>
import { auth, roleLabel } from './store'
import { useRouter } from 'vue-router'

const router = useRouter()
function logout() {
  auth.logout()
  router.push('/login')
}
</script>
