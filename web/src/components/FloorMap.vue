<template>
  <div>
    <div class="panel toolbar">
      <div class="row">
        <label>楼层</label>
        <select v-model="floor" @change="load">
          <option v-for="f in floors" :key="f" :value="f">{{ f }}</option>
        </select>
      </div>
      <div class="legend">
        <span><i class="dot free"></i>可用</span>
        <span><i class="dot reserved"></i>预占（搬迁中）</span>
        <span><i class="dot occupied"></i>已占用</span>
        <span><i class="dot netfault"></i>网口故障</span>
      </div>
      <button class="btn btn-sm" @click="load">刷新</button>
    </div>

    <div class="panel map-panel">
      <div class="floor-name">{{ floor }} 楼层工位图</div>
      <div class="grid" :style="gridStyle">
        <button
          v-for="s in seats"
          :key="s.id"
          class="seat"
          :class="seatClass(s)"
          :style="seatStyle(s)"
          @click="select(s)"
          :title="seatTitle(s)"
        >
          <span class="code">{{ s.code.split('-').slice(1).join('-') }}</span>
          <span class="meta">{{ s.net_port }}<template v-if="s.phone_ext"> / ☎{{ s.phone_ext }}</template></span>
          <span v-if="!s.net_enabled" class="fault-flag">网口故障</span>
        </button>
      </div>
      <p class="muted hint">
        点击座位查看详情；行政可在可用座位上创建搬迁单（预占），IT 可登记/修复网口故障。
      </p>
    </div>

    <!-- 座位详情 / 操作弹层 -->
    <div v-if="selected" class="modal-mask" @click.self="selected = null">
      <div class="modal">
        <h3>座位 {{ selected.code }}</h3>
        <dl class="info">
          <div><dt>状态</dt><dd>{{ statusText(selected.status) }}</dd></div>
          <div><dt>网口</dt><dd>{{ selected.net_port || '无' }}
            <b v-if="!selected.net_enabled" class="danger-text">（故障中）</b></dd></div>
          <div><dt>电话分机</dt><dd>{{ selected.phone_ext || '无' }}</dd></div>
          <div><dt>显示器</dt><dd>{{ selected.has_monitor ? '已配备' : '无' }}</dd></div>
          <div v-if="selected.occupant"><dt>当前使用人</dt><dd>{{ selected.occupant.name }}</dd></div>
        </dl>

        <!-- 行政：在可用座位创建搬迁单 -->
        <template v-if="user.role === 'admin' && selected.status === 'free'">
          <div class="field">
            <label>搬迁员工</label>
            <select v-model="form.employeeId">
              <option v-for="e in employees" :key="e.id" :value="e.id">
                {{ e.name }}（{{ e.username }}）
              </option>
            </select>
          </div>
          <div class="field">
            <label>搬迁日期</label>
            <input type="date" v-model="form.moveDate" />
          </div>
          <div class="modal-actions">
            <button class="btn" @click="selected = null">取消</button>
            <button class="btn btn-primary" :disabled="!form.employeeId || !form.moveDate" @click="createOrder">
              创建搬迁单并预占
            </button>
          </div>
        </template>

        <!-- 预占座位：查看对应的搬迁单 -->
        <template v-else-if="selected.status === 'reserved'">
          <p class="muted">该座位已被搬迁单预占，同一座位不能分给两人。</p>
          <div class="modal-actions">
            <button class="btn" @click="selected = null">关闭</button>
            <button class="btn btn-primary" @click="openActiveOrder">
              查看搬迁单
            </button>
          </div>
        </template>

        <!-- IT：网口故障登记 / 修复 -->
        <template v-else-if="user.role === 'it' && selected.net_port">
          <div v-if="selected.net_enabled" class="field">
            <label>登记故障原因（可选）</label>
            <input v-model="faultReason" placeholder="如：配线架端口损坏" />
          </div>
          <div class="modal-actions">
            <button class="btn" @click="selected = null">关闭</button>
            <button v-if="selected.net_enabled" class="btn btn-danger" @click="setNet(false)">
              登记网口故障
            </button>
            <button v-else class="btn btn-primary" @click="setNet(true)">标记网口已修复</button>
          </div>
        </template>

        <!-- 其他只读场景 -->
        <template v-else>
          <p class="muted">
            <template v-if="selected.status === 'occupied'">该座位已有人使用。</template>
            <template v-else-if="user.role !== 'admin'">该座位当前可用。</template>
          </p>
          <div class="modal-actions">
            <button class="btn" @click="selected = null">关闭</button>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { api } from '../api.js'

const props = defineProps({
  user: Object,
  refreshKey: Number
})
const emit = defineEmits(['toast', 'changed', 'open-order'])

const floors = ref([])
const floor = ref('')
const seats = ref([])
const employees = ref([])
const selected = ref(null)
const faultReason = ref('')
const form = ref({ employeeId: null, moveDate: '' })

const maxX = computed(() => Math.max(...seats.value.map(s => s.x), 1))
const gridStyle = computed(() => ({
  gridTemplateColumns: `repeat(${maxX.value}, 1fr)`,
}))

onMounted(load)
watch(() => props.refreshKey, load)
// 行政需要员工下拉
watch(() => props.user.role, (role) => {
  if (role === 'admin') loadEmployees()
}, { immediate: true })

async function load() {
  try {
    if (!floors.value.length) {
      const { floors: fs } = await api.floors()
      floors.value = fs
      if (!floor.value) floor.value = fs[0]
    }
    const { seats: ss } = await api.seats(floor.value)
    // 按网格位置排序，保证 CSS grid 先行后列还原矩阵
    seats.value = ss.sort((a, b) => a.y - b.y || a.x - b.x)
  } catch (e) {
    emit('toast', e.message)
  }
}

async function loadEmployees() {
  try {
    const { employees: es } = await api.employees()
    employees.value = es
  } catch (_) { /* 无权限则忽略 */ }
}

function seatClass(s) {
  return {
    free: s.status === 'free' && s.net_enabled,
    reserved: s.status === 'reserved',
    occupied: s.status === 'occupied',
    'net-fault': !s.net_enabled && s.status === 'free'
  }
}
function seatStyle(s) {
  // CSS grid 行列定位（1 起）
  return { gridColumn: s.x, gridRow: s.y }
}
function statusText(st) {
  return { free: '可用', reserved: '预占（搬迁中）', occupied: '已占用' }[st] || st
}
function seatTitle(s) {
  let t = `${s.code} · ${statusText(s.status)}`
  if (!s.net_enabled) t += ' · 网口故障'
  if (s.occupant) t += ` · ${s.occupant.name}`
  return t
}

function select(s) {
  selected.value = s
  faultReason.value = ''
  form.value = { employeeId: null, moveDate: '' }
}

async function openActiveOrder() {
  try {
    const { order } = await api.activeOrderBySeat(selected.value.id)
    if (order) {
      selected.value = null
      emit('open-order', order.id)
    } else {
      emit('toast', '该座位当前没有进行中的搬迁单')
    }
  } catch (e) {
    emit('toast', e.message)
  }
}

async function createOrder() {
  try {
    await api.createOrder({
      employee_id: form.value.employeeId,
      to_seat_id: selected.value.id,
      move_date: form.value.moveDate
    })
    emit('toast', '搬迁单已创建，座位预占成功', 'ok')
    selected.value = null
    emit('changed')
  } catch (e) {
    emit('toast', e.message)
    await load()
  }
}

async function setNet(enabled) {
  try {
    await api.setNetState(selected.value.id, enabled, faultReason.value)
    emit('toast', enabled ? '网口已标记修复' : '网口故障已登记', 'ok')
    selected.value = null
    emit('changed')
  } catch (e) {
    emit('toast', e.message)
  }
}
</script>

<style scoped>
.toolbar { display: flex; align-items: center; gap: 20px; margin-bottom: 14px; }
.legend { display: flex; gap: 18px; flex: 1; font-size: 12px; color: var(--muted); }
.dot { display: inline-block; width: 10px; height: 10px; border-radius: 3px; margin-right: 5px; }
.dot.free { background: #d9f1e5; border: 1px solid var(--free); }
.dot.reserved { background: #fce9c4; border: 1px solid var(--reserved); }
.dot.occupied { background: #dfe5ec; border: 1px solid var(--occupied); }
.dot.netfault { background: var(--danger-bg); border: 1px solid var(--danger); }

.map-panel { padding: 22px; }
.floor-name { font-weight: 700; margin-bottom: 16px; }
.grid {
  display: grid;
  gap: 10px;
  grid-auto-flow: row;
}
.seat {
  min-height: 64px; border-radius: 9px; border: 1.5px solid; cursor: pointer;
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  gap: 3px; padding: 6px 4px; transition: transform .1s, box-shadow .1s;
  font-family: inherit;
}
.seat:hover { transform: translateY(-2px); box-shadow: 0 4px 12px rgba(0,0,0,.1); }
.seat .code { font-size: 13px; font-weight: 700; }
.seat .meta { font-size: 10.5px; opacity: .85; }
.seat.free { background: #d9f1e5; border-color: var(--free); color: #146c49; }
.seat.reserved { background: #fce9c4; border-color: var(--reserved); color: #8a5a06; }
.seat.occupied { background: #dfe5ec; border-color: var(--occupied); color: #475569; }
.seat.net-fault { background: var(--danger-bg); border-color: var(--danger); color: #b03535; }
.fault-flag {
  font-size: 10px; background: var(--danger); color: #fff; border-radius: 4px;
  padding: 1px 6px;
}
.hint { margin: 16px 0 0; }

.info { margin: 0 0 16px; display: grid; grid-template-columns: 80px 1fr; row-gap: 8px; font-size: 13px; }
.info dt { color: var(--muted); }
.info dd { margin: 0; }
.danger-text { color: var(--danger); }
.modal-actions { display: flex; justify-content: flex-end; gap: 10px; margin-top: 8px; }
</style>
