<template>
  <div class="page" v-if="ticket">
    <div v-if="error" class="alert error">{{ error }}</div>
    <div v-if="notice" class="alert success">{{ notice }}</div>

    <div class="card">
      <div class="toolbar" style="margin-bottom:10px">
        <h2 style="margin:0">搬迁单 {{ ticket.ticket_no }}</h2>
        <StatusBadge :status="ticket.status" />
      </div>
      <div class="info-grid">
        <div><div class="k">员工</div><div class="v">{{ ticket.employee?.name }}（分机 {{ ticket.employee?.extension }}）</div></div>
        <div><div class="k">原座位</div><div class="v">{{ ticket.from_seat?.code }}</div></div>
        <div><div class="k">目标座位</div><div class="v">{{ ticket.to_seat?.code }}</div></div>
        <div><div class="k">搬迁日期</div><div class="v">{{ ticket.move_date }}</div></div>
        <div><div class="k">创建人</div><div class="v">{{ ticket.created_by?.name }}</div></div>
        <div><div class="k">创建时间</div><div class="v">{{ fmt(ticket.created_at) }}</div></div>
        <div v-if="ticket.note"><div class="k">备注</div><div class="v">{{ ticket.note }}</div></div>
        <div v-if="ticket.cancel_reason"><div class="k">取消原因</div><div class="v">{{ ticket.cancel_reason }}</div></div>
      </div>
    </div>

    <div v-if="ticket.status === 'blocked'" class="alert warn">
      流程受阻：请 IT 排查网口或行政更换目标座位后再继续。
    </div>
    <div v-if="ticket.asset_mismatch && active" class="alert warn">
      资产标签不符：台账 {{ ticket.to_seat?.asset_tag }}，实物 {{ ticket.asset_tag_reported }}，待 IT 核实。
    </div>

    <!-- IT 核对结果 -->
    <div class="card">
      <h3>IT 核对结果</h3>
      <div class="check-grid">
        <div class="check-item">
          <div class="ci-title">网口测试（{{ ticket.to_seat?.port_code }}）</div>
          <div class="ci-status" :class="checkClass(ticket.network_ok, ticket.network_tested_at)">
            {{ checkText(ticket.network_ok, ticket.network_tested_at) }}
          </div>
          <div class="ci-msg" v-if="ticket.network_message">
            {{ ticket.network_message }}（{{ ticket.network_latency_ms }}ms）
          </div>
        </div>
        <div class="check-item">
          <div class="ci-title">电话分机</div>
          <div class="ci-status" :class="ticket.phone_ok ? 'ok' : 'todo'">
            {{ ticket.phone_ok ? `已迁移至 ${ticket.extension_no}` : '未迁移' }}
          </div>
        </div>
        <div class="check-item">
          <div class="ci-title">显示器</div>
          <div class="ci-status" :class="ticket.monitor_ok ? 'ok' : 'todo'">
            {{ ticket.monitor_ok ? '正常' : '未核对' }}
            <span v-if="ticket.asset_mismatch" style="color:#dc2626">（资产异常）</span>
          </div>
          <div class="ci-msg" v-if="ticket.asset_tag_reported">实物标签：{{ ticket.asset_tag_reported }}</div>
        </div>
      </div>
    </div>

    <!-- IT 操作面板 -->
    <div class="card" v-if="auth.user?.role === 'it' && active">
      <h3>IT 操作</h3>
      <div style="display:flex; gap:10px; flex-wrap:wrap; align-items:flex-end">
        <button class="btn primary" :disabled="busy" @click="itAction('network-test')">执行网口测试</button>
        <button class="btn primary" :disabled="busy" @click="itAction('phone-migrate')">迁移电话分机</button>
        <div style="display:flex; gap:8px; align-items:center">
          <input v-model="monitor.asset_tag" placeholder="扫描资产标签" style="border:1px solid var(--border); border-radius:6px; padding:7px 10px" />
          <label style="font-size:13px"><input type="checkbox" v-model="monitor.monitor_ok" /> 显示器正常</label>
          <button class="btn primary" :disabled="busy" @click="submitMonitor">提交显示器核对</button>
        </div>
      </div>
      <div v-if="ticket.asset_mismatch" class="mt" style="display:flex; gap:8px; align-items:center">
        <input v-model="resolveTag" placeholder="核实后的正确资产标签" style="border:1px solid var(--border); border-radius:6px; padding:7px 10px" />
        <button class="btn" :disabled="busy" @click="resolveAsset">更正台账并解除异常</button>
      </div>
    </div>

    <!-- 行政操作面板 -->
    <div class="card" v-if="auth.user?.role === 'admin' && active">
      <h3>行政操作</h3>
      <div style="display:flex; gap:10px; flex-wrap:wrap; align-items:center">
        <input type="date" v-model="rescheduleDate" style="border:1px solid var(--border); border-radius:6px; padding:7px 10px" />
        <button class="btn" :disabled="busy || !rescheduleDate" @click="reschedule">改期</button>
        <button class="btn" :disabled="busy" @click="openSeatPicker">更换目标座位</button>
        <button class="btn danger" :disabled="busy" @click="cancel">取消搬迁</button>
      </div>
    </div>

    <!-- 员工确认面板 -->
    <div class="card" v-if="auth.user?.role === 'employee' && ticket.employee_id === auth.user?.id && active">
      <h3>搬入确认</h3>
      <template v-if="ticket.status === 'ready'">
        <p class="muted mb">IT 已完成核对。请确认新工位设备与网络是否可用，全部勾选后提交即完成搬迁；如有问题请取消对应勾选并说明，单据将打回 IT 处理。</p>
        <div style="display:flex; gap:18px; margin-bottom:12px">
          <label><input type="checkbox" v-model="confirm.network_ok" /> 网络可用</label>
          <label><input type="checkbox" v-model="confirm.phone_ok" /> 电话分机可用</label>
          <label><input type="checkbox" v-model="confirm.monitor_ok" /> 显示器正常</label>
        </div>
        <div class="field">
          <label>问题说明（有问题时填写）</label>
          <textarea v-model="confirm.comment" rows="2"></textarea>
        </div>
        <button class="btn primary" :disabled="busy" @click="submitConfirm">提交确认</button>
      </template>
      <p v-else class="muted">IT 核对尚未全部通过（需网口测试通过），暂不能确认完成。</p>
    </div>

    <!-- 变更经过 -->
    <div class="card">
      <h3>变更经过</h3>
      <Timeline :events="ticket.events || []" />
    </div>

    <!-- 换座弹窗 -->
    <div v-if="seatPickerOpen" class="modal-mask" @click.self="seatPickerOpen = false">
      <div class="modal">
        <h3>选择新目标座位（当前 {{ ticket.to_seat?.code }}）</h3>
        <div class="floor-tabs">
          <button v-for="f in floors" :key="f.floor" :class="{ active: f.floor === pickerFloor }"
            @click="pickerFloor = f.floor; loadPickerSeats()">{{ f.floor }}F</button>
        </div>
        <SeatMap :seats="pickerSeats" selectable :selected-id="newSeat?.id" @select="newSeat = $event" />
        <div class="actions">
          <button class="btn" @click="seatPickerOpen = false">取消</button>
          <button class="btn primary" :disabled="!newSeat || busy" @click="changeSeat">确认换座</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { request } from '../api'
import { auth } from '../store'
import StatusBadge from '../components/StatusBadge.vue'
import SeatMap from '../components/SeatMap.vue'
import Timeline from '../components/Timeline.vue'

const props = defineProps({ id: String })

const ticket = ref(null)
const error = ref('')
const notice = ref('')
const busy = ref(false)

const monitor = reactive({ monitor_ok: true, asset_tag: '' })
const resolveTag = ref('')
const rescheduleDate = ref('')
const confirm = reactive({ network_ok: true, phone_ok: true, monitor_ok: true, comment: '' })

const seatPickerOpen = ref(false)
const floors = ref([])
const pickerFloor = ref(0)
const pickerSeats = ref([])
const newSeat = ref(null)

const active = computed(() => ['pending', 'ready', 'blocked'].includes(ticket.value?.status))

function fmt(t) {
  return t ? new Date(t).toLocaleString('zh-CN', { hour12: false }) : ''
}
function checkClass(ok, testedAt) {
  if (!testedAt) return 'todo'
  return ok ? 'ok' : 'fail'
}
function checkText(ok, testedAt) {
  if (!testedAt) return '未测试'
  return ok ? '通过' : '未通过'
}

async function load() {
  ticket.value = await request(`/api/tickets/${props.id}`)
}

async function run(fn, okMsg) {
  error.value = ''
  notice.value = ''
  busy.value = true
  try {
    ticket.value = await fn()
    if (okMsg) notice.value = okMsg
  } catch (e) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}

const itAction = (action) =>
  run(() => request(`/api/tickets/${props.id}/it/${action}`, { method: 'POST' }), '操作完成')

const submitMonitor = () =>
  run(() => request(`/api/tickets/${props.id}/it/monitor-check`, {
    method: 'POST',
    body: { monitor_ok: monitor.monitor_ok, asset_tag: monitor.asset_tag },
  }), '显示器核对已提交')

const resolveAsset = () =>
  run(() => request(`/api/tickets/${props.id}/it/resolve-asset`, {
    method: 'POST', body: { asset_tag: resolveTag.value },
  }), '资产异常已解除')

const reschedule = () =>
  run(() => request(`/api/tickets/${props.id}/reschedule`, {
    method: 'PATCH', body: { move_date: rescheduleDate.value },
  }), '已改期')

async function cancel() {
  const reason = prompt('请输入取消原因：')
  if (reason === null) return
  await run(() => request(`/api/tickets/${props.id}/cancel`, {
    method: 'POST', body: { reason },
  }), '搬迁单已取消，预占座位已释放')
}

async function openSeatPicker() {
  floors.value = await request('/api/floors')
  pickerFloor.value = ticket.value.to_seat.floor
  newSeat.value = null
  await loadPickerSeats()
  seatPickerOpen.value = true
}
async function loadPickerSeats() {
  pickerSeats.value = await request(`/api/floors/${pickerFloor.value}/seats`)
}
async function changeSeat() {
  await run(() => request(`/api/tickets/${props.id}/change-seat`, {
    method: 'POST', body: { to_seat_id: newSeat.value.id },
  }), `已更换目标座位为 ${newSeat.value.code}，IT 需重新核对`)
  seatPickerOpen.value = false
}

const submitConfirm = () =>
  run(() => request(`/api/tickets/${props.id}/confirm`, {
    method: 'POST',
    body: { ...confirm },
  }).then((t) => {
    if (t.status === 'blocked') {
      error.value = '已记录您反馈的问题，单据已打回 IT 处理'
    }
    return t
  }), confirm.network_ok && confirm.phone_ok && confirm.monitor_ok ? '搬迁完成，感谢确认' : '')

onMounted(async () => {
  try {
    await load()
  } catch (e) {
    error.value = e.message
  }
})
</script>
