import { createRouter, createWebHashHistory } from 'vue-router'
import { auth } from './store'

const routes = [
  { path: '/login', component: () => import('./views/Login.vue'), meta: { public: true } },
  { path: '/', component: () => import('./views/Dashboard.vue') },
  { path: '/tickets/new', component: () => import('./views/NewTicket.vue') },
  { path: '/tickets/:id', component: () => import('./views/TicketDetail.vue'), props: true },
  { path: '/floor', component: () => import('./views/FloorMap.vue') },
]

export const router = createRouter({
  history: createWebHashHistory(),
  routes,
})

router.beforeEach((to) => {
  if (!to.meta.public && !auth.token) return '/login'
  if (to.path === '/login' && auth.token) return '/'
})
