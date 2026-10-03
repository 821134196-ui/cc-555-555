<template>
  <div v-if="id" class="modal-mask" @click.self="$emit('close')">
    <div class="modal wide">
      <div v-if="loading" class="muted">加载中…</div>
      <template v-else-if="order">
        <div class="head">
          <h3>搬迁单 #{{ order.id }}</h3>
          <span class="tag" :class="'tag-' + order.status">{{ statusText(order.status) }}</span>
        </div>

        <div class="summary">
          <div><label>搬迁员工</label><b>{{ order.employee.name }}</b></div>
          <div><label>原座位</label><b>{{ order.from_seat?.code || '未登记工位' }}</b></div>
          <div><label>目标座位</label><b>{{ order.to_seat.code }}</b></div>
          <div><label>搬迁日期</label><b>{{ fmtDate(order.move_date) }}</b></div>
        </div>

        <div class="check-grid">
          <div class="check-item">
            <label>网口</label>
            <span>{{ order.to_seat.net_port || '无' }}</span>
            <b v-if="!order.to_seat.net_enabled" class="no">故障中</b>
          </div>
          <div class="check-item">
            <label>网络测试</label>
            <b :class="order.net_tested ? 'ok' : 'no'">
              {{ order.checked_at ? (order.net_tested ? '测试通过' : '未通过') : '未测试' }}
            </b>
          </div>
          <div class="check-item">
            <label>分机迁移</label>
            <b :class="order.ext_migrated ? 'ok' : 'muted-b'">
              {{ order.checked_at ? (order.ext_migrated ? '已迁移' : '无分机/未迁移') : '—' }}
            </b>
          </div>
          <div class="check-item">
            <label>显示器</label>
            <b :class="order.monitor_ok ? 'ok' : 'no'">{{ order.checked_at ? (order.monitor_ok ? '齐备' : '缺失') : '—' }}</b>
          </div>
          <div class="check-item">
            <label>资产标签</label>
            <b :class="order.asset_match ? 'ok' : 'no'">
              {{ order.checked_at ? (order.asset_match ? '相符' : '不符') : '—' }}
            </b>
          </div>
        </div>

        <!-- ===== 行政操作：改期 / 换座 / 取消 ===== -->
        <section v-if="user.role === 'admin' && canEdit" class="action-block">
          <h4>行政操作</h4>
          <div class="row wrap">
            <div class="inline-field">
              <label>改期到</label>
              <input type="date" v-model="rs.date" />
            </div>
            <div class="inline-field">
              <label>换到目标座位</label>
              <select v-model="rs.seatId" style="width:200px">
                <option :value="null">不换座位，仅改期</option>
                <optgroup v-for="f in freeSeatGroups" :key="f.floor" :label="f.floor">
                  <option v-for="s in f.seats" :key="s.id" :value="s.id">
                    {{ s.code }}（网口 {{ s.net_enabled ? '正常' : '故障' }}）
                  </option>
                </optgroup>
              </select>
            </div>
            <button class="btn btn-primary" :disabled="!rs.date && !rs.seatId" @click="reschedule">
              改期 / 换座（释放旧预占）
            </button>
          </div>
          <div class="row cancel-row">
            <input v-model="cancelReason" placeholder="取消原因（可选）" style="flex:1" />
            <button class="btn btn-danger" @click="cancel">取消搬迁并释放预占</button>
          </div>
        </section>

        <!-- ===== IT 操作：调模拟网络测试 + 分机迁移，核对显示器与资产标签 ===== -->
        <section v-if="user.role === 'it' && canEdit" class="action-block">
          <h4>IT 现场核对</h4>
          <p class="muted">
            网络测试与分机迁移由模拟接口自动执行（结果不可手填）；请现场核对显示器与资产标签。
            员工登记资产标签：<b>{{ order.employee.asset_tag }}</b>
          </p>
          <div class="row wrap">
            <label class="check-line">
              <input type="checkbox" v-model="itForm.monitor" />
              显示器已就位并可正常使用
            </label>
            <div class="inline-field">
              <label>现场资产标签</label>
              <input v-model="itForm.tag" :placeholder="order.employee.asset_tag" style="width:170px" />
            </div>
            <div class="inline-field">
              <label>备注</label>
              <input v-model="itForm.note" placeholder="可选" style="width:170px" />
            </div>
            <button class="btn btn-primary" :disabled="checking" @click="itCheck">
              {{ checking ? '正在调用网络测试…' : '执行核对（网络测试 + 分机迁移）' }}
            </button>
          </div>
          <p v-if="itResult" class="muted">{{ itResult }}</p>
        </section>

        <!-- ===== 员工操作：仅本人 ===== -->
        <section v-if="user.role === 'employee' && isOwner && canEdit" class="action-block">
          <h4>我的现场确认</h4>
          <template v-if="order.status === 'it_checked'">
            <p class="muted">IT 已核对通过，请确认新工位实际办公条件：</p>
            <label class="check-line block">
              <input type="checkbox" v-model="cf.net" />
              网络可正常使用（完成条件之一）
            </label>
            <label class="check-line block">
              <input type="checkbox" v-model="cf.device" />
              电脑/显示器等设备可正常使用
            </label>
            <input v-model="cf.note" placeholder="备注（可选）" style="width:100%;margin-top:8px" />
            <div class="modal-actions">
              <button class="btn btn-primary" :disabled="!cf.net || !cf.device" @click="confirm">
                确认接收，完成搬迁
              </button>
            </div>
          </template>
          <p v-else class="muted">
            IT 核对通过前不能确认。当前：{{ order.net_tested ? '网络测试已通过' : '网络尚未测试通过' }}。
          </p>
        </section>
        <p v-if="user.role === 'employee' && !isOwner" class="no perm-note">
          仅搬迁单上的员工本人可以确认接收，不能代他人确认。
        </p>

        <!-- ===== 变更经过 ===== -->
        <section class="timeline">
          <h4>变更经过</h4>
          <ul>
            <li v-for="e in events" :key="e.id" :class="['ev ev-' + e.action]">
              <div class="ev-head">
                <span class="ev-action">{{ actionText(e.action) }}</span>
                <span class="muted">{{ e.actor_name }} · {{ fmtTime(e.created_at) }}</span>
              </div>
              <p>{{ e.detail }}</p>
            </li>
          </ul>
          <h5>IT 核对记录</h5>
          <ul>
            <li v-for="c in order.it_checks" :key="c.id" class="ev" :class="c.passed ? 'ev-ok' : 'ev-fail'">
              <div class="ev-head">
                <b :class="c.passed ? 'ok' : 'no'">{{ c.passed ? '通过' : '未通过' }}</b>
                <span class="muted">{{ fmtTime(c.checked_at) }}</span>
              </div>
              <p>
                网络：{{ c.net_tested ? '✓' : '✗' }} {{ c.net_detail }} ｜
                分机：{{ c.ext_migrated ? '✓' : '—' }} {{ c.ext_detail }} ｜
                显示器：{{ c.monitor_ok ? '✓' : '✗' }} ｜
                标签：{{ c.asset_match ? '✓' : '✗' }}（期望 {{ c.expected_tag }} / 现场 {{ c.actual_tag || '空' }}）
              </p>
            </li>
            <li v-if="!order.it_checks?.length" class="muted">暂无</li>
          </ul>
        </section>

        <div class="modal-actions">
          <button class="btn" @click="$emit('close')">关闭</button>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { api } from '../api.js'

const props = defineProps({
  id: Number,
  user: Object
})
const emit = defineEmits(['close', 'toast', 'changed'])

const order = ref(null)
const events = ref([])
const loading = ref(false)
const checking = ref(false)
const itResult = ref('')
const rs = ref({ date: '', seatId: null })
const cancelReason = ref('')
const itForm = ref({ monitor: false, tag: '', note: '' })
const cf = ref({ net: false, device: false, note: '' })
const freeSeatGroups = ref([])

const canEdit = computed(() =>
  order.value && ['reserved', 'it_checked'].includes(order.value.status))
const isOwner = computed(() => order.value && props.user.id === order.value.employee_id)

watch(() => props.id, (id) => { if (id) load() })
watch(() => props.user.role, (r) => { if (r === 'admin') loadFreeSeats() }, { immediate: true })

async function loadFreeSeats() {
  try {
    const { floors } = await api.floors()
    const groups = []
    for (const f of floors) {
      const { seats } = await api.seats(f)
      const free = seats.filter(s => s.status === 'free')
      groups.push({ floor: f, seats: free })
    }
    freeSeatGroups.value = groups
  } catch (_) { /* 忽略 */ }
}

async function load() {
  loading.value = true
  order.value = null
  itResult.value = ''
  try {
    const data = await api.order(props.id)
    order.value = data.order
    events.value = data.events || []
  } catch (e) {
    emit('toast', e.message)
    emit('close')
  } finally {
    loading.value = false
  }
}

async function reschedule() {
  const payload = {}
  if (rs.value.date) payload.new_move_date = rs.value.date
  if (rs.value.seatId) payload.new_to_seat_id = rs.value.seatId
  try {
    await api.reschedule(props.id, payload)
    emit('toast', '改期/换座成功，旧预占已释放', 'ok')
    await load()
    emit('changed')
  } catch (e) {
    emit('toast', e.message)
  }
}

async function cancel() {
  if (!confirm('确认取消该搬迁单？目标座位预占将立即释放。')) return
  try {
    await api.cancel(props.id, cancelReason.value)
    emit('toast', '已取消并释放预占座位', 'ok')
    await load()
    emit('changed')
  } catch (e) {
    emit('toast', e.message)
  }
}

async function itCheck() {
  if (!itForm.value.tag) {
    emit('toast', '请填写现场看到的资产标签（未发现标签请明确填写）')
    return
  }
  checking.value = true
  itResult.value = ''
  try {
    const r = await api.itCheck(props.id, {
      monitor_ok: itForm.value.monitor,
      actual_asset_tag: itForm.value.tag.trim(),
      note: itForm.value.note
    })
    itResult.value = r.passed ? '核对通过：网络测试通过，可通知员工现场确认。' : ''
    emit('toast', 'IT 核对通过', 'ok')
  } catch (e) {
    itResult.value = '核对未通过：' + e.message
    emit('toast', e.message)
  } finally {
    checking.value = false
    await load()
    emit('changed')
  }
}

async function confirm() {
  try {
    await api.confirm(props.id, {
      net_usable: cf.value.net,
      device_usable: cf.value.device,
      note: cf.value.note
    })
    emit('toast', '已确认接收，搬迁完成 🎉', 'ok')
    await load()
    emit('changed')
  } catch (e) {
    emit('toast', e.message)
  }
}

function fmtDate(d) { return d ? d.slice(0, 10) : '—' }
function fmtTime(d) {
  if (!d) return ''
  return d.replace('T', ' ').slice(0, 16)
}
function statusText(s) {
  return {
    reserved: '预占·待 IT 核对', it_checked: 'IT 已核对·待员工确认',
    confirmed: '已完成', cancelled: '已取消'
  }[s] || s
}
function actionText(a) {
  return {
    created: '创建搬迁单', rescheduled: '改期/换座', cancelled: '取消',
    it_checked: 'IT 核对通过', it_check_failed: 'IT 核对未通过',
    net_failed: '网络未通过', asset_mismatch: '资产标签不符',
    confirmed: '员工确认完成', net_port_fault: '网口故障登记',
    net_port_repaired: '网口修复'
  }[a] || a
}
</script>

<style scoped>
.wide { width: 720px; }
.head { display: flex; align-items: center; gap: 12px; margin-bottom: 14px; }
.head h3 { margin: 0; }
.summary {
  display: grid; grid-template-columns: repeat(4, 1fr); gap: 10px;
  background: #f7f9fc; border-radius: 8px; padding: 12px 14px; margin-bottom: 14px;
}
.summary label, .check-item label { display: block; color: var(--muted); font-size: 11px; margin-bottom: 3px; }
.check-grid { display: grid; grid-template-columns: repeat(5, 1fr); gap: 8px; margin-bottom: 16px; }
.check-item { background: #fff; border: 1px solid var(--line); border-radius: 8px; padding: 8px 10px; font-size: 12px; display:flex;flex-direction:column;gap:2px; }
.ok { color: #178a5b; }
.no { color: var(--danger); }
.muted-b { color: var(--muted); }

.action-block {
  border-top: 1px dashed var(--line); padding-top: 14px; margin-top: 4px; margin-bottom: 14px;
}
.action-block h4, .timeline h4, .timeline h5 { margin: 0 0 10px; font-size: 14px; }
.inline-field { display: flex; flex-direction: column; gap: 4px; }
.wrap { flex-wrap: wrap; row-gap: 10px; }
.check-line { display: flex; align-items: center; gap: 6px; font-size: 13px; }
.check-line.block { display: flex; margin: 8px 0; }
.cancel-row { margin-top: 10px; }
.perm-note { margin: 10px 0; }

.timeline ul { list-style: none; padding: 0; margin: 0 0 14px; }
.ev {
  border-left: 3px solid var(--line); padding: 6px 0 6px 12px; margin-bottom: 8px;
}
.ev p { margin: 4px 0 0; font-size: 12.5px; color: #3c4756; }
.ev-head { display: flex; justify-content: space-between; align-items: center; }
.ev-action { font-weight: 600; font-size: 13px; }
.ev-created { border-color: var(--brand); }
.ev-it_checked, .ev-ok { border-color: var(--free); }
.ev-cancelled { border-color: var(--occupied); }
.ev-net_failed, .ev-asset_mismatch, .ev-it_check_failed, .ev-fail { border-color: var(--danger); }
.ev-net_port_fault { border-color: var(--reserved); }
.ev-net_port_repaired { border-color: var(--free); }
.modal-actions { display: flex; justify-content: flex-end; gap: 10px; margin-top: 6px; }
</style>
