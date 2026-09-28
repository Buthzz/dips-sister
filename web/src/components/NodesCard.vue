<script setup>
import { computed, ref } from 'vue'
import { statusBadgeClass, probeNode } from '../api.js'
import { formatTime } from '../format.js'

const props = defineProps({
  nodes: { type: Array, default: () => [] },
})

const emit = defineEmits(['refresh'])

const alive = computed(() => props.nodes.filter((n) => n.status === 'alive').length)

// State probe per node: { nodeId -> { loading, result } }
const probing = ref({})

async function testaKoneksi(nodeId) {
  probing.value[nodeId] = { loading: true, result: null }
  try {
    const res = await probeNode(nodeId)
    probing.value[nodeId] = { loading: false, result: res }
  } catch (e) {
    probing.value[nodeId] = {
      loading: false,
      result: { reachable: false, latency_ms: 0, error: e.message },
    }
  }
}

// Cek apakah alamat node memiliki potensi salah format atau isolasi LAN
function cekPeringatanAlamat(addr) {
  if (!addr) return 'Alamat kosong'
  if (addr.includes('127.0.0.1') || addr.includes('localhost')) {
    return 'Alamat loopback (localhost): worker di laptop lain tidak dapat dihubungi master melalui alamat ini.'
  }
  const parts = addr.split(':')
  if (parts.length !== 2 || !parts[1] || isNaN(Number(parts[1]))) {
    return `Format alamat '${addr}' tidak standar (harus host:port, contoh: 192.168.1.10:9000).`
  }
  return null
}

const nodeWarnings = computed(() => {
  return props.nodes
    .map((n) => {
      const warn = cekPeringatanAlamat(n.addr)
      return warn ? { node_id: n.node_id, addr: n.addr, warning: warn } : null
    })
    .filter(Boolean)
})
</script>

<template>
  <section class="card">
    <h2 class="card-title">
      <span class="title-left">Status Kluster & Worker Node</span>
      <span class="summary">{{ alive }} aktif dari {{ nodes.length }} terdaftar</span>
      <button class="btn" @click="emit('refresh')">Refresh</button>
    </h2>

    <div v-if="nodeWarnings.length > 0" class="warning-container">
      <div v-for="w in nodeWarnings" :key="w.node_id" class="alert alert--warn">
        ⚠️ <strong>Peringatan Alamat Node <code>{{ w.node_id }}</code>:</strong>
        {{ w.warning }}
      </div>
    </div>

    <div v-if="nodes.length === 0" class="empty-note">
      Belum ada node yang mendaftar. Nyalakan laptop worker (mode node) untuk bergabung ke kluster.
    </div>

    <div v-else-if="nodes.length > 0 && alive === 0" class="alert alert--warn">
      Tidak ada node worker aktif — semua task akan dieksekusi secara lokal oleh master (graceful degradation).
    </div>

    <div v-else class="table-wrap">
      <table class="table">
        <thead>
          <tr>
            <th>Node ID</th>
            <th>Alamat Jaringan</th>
            <th>Heartbeat</th>
            <th>Daya Jangkau (Dua Arah)</th>
            <th>Task / Kapasitas</th>
            <th>Detak Terakhir</th>
            <th>Uji Konektivitas</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="n in nodes" :key="n.node_id">
            <td><code>{{ n.node_id }}</code></td>
            <td>
              <code>{{ n.addr }}</code>
              <span
                v-if="cekPeringatanAlamat(n.addr)"
                class="badge badge--amber tip-badge"
                :title="cekPeringatanAlamat(n.addr)"
              >
                Format!
              </span>
            </td>
            <td>
              <span class="badge" :class="statusBadgeClass(n.status)">
                {{ n.status === 'alive' ? 'Aktif' : 'Mati' }}
              </span>
            </td>
            <td>
              <template v-if="probing[n.node_id]?.result">
                <span
                  class="badge"
                  :class="probing[n.node_id].result.reachable ? 'badge--green' : 'badge--red'"
                  :title="probing[n.node_id].result.error || 'Dua arah lancar'"
                >
                  {{ probing[n.node_id].result.reachable ? 'Siap (Dua Arah)' : 'Terisolasi' }}
                </span>
              </template>
              <template v-else>
                <span class="badge badge--slate">Belum Diuji</span>
              </template>
            </td>
            <td>{{ n.active_tasks }} / {{ n.capacity }}</td>
            <td>{{ formatTime(n.last_heartbeat) }}</td>
            <td class="probe-cell">
              <button
                class="btn btn--sm"
                :disabled="probing[n.node_id]?.loading"
                @click="testaKoneksi(n.node_id)"
              >
                {{ probing[n.node_id]?.loading ? 'Menguji...' : '🔍 Tes Koneksi' }}
              </button>
              <div v-if="probing[n.node_id]?.result" class="probe-feedback">
                <span
                  v-if="probing[n.node_id].result.reachable"
                  class="probe--ok"
                >
                  ✓ {{ probing[n.node_id].result.latency_ms }} ms
                </span>
                <span
                  v-else
                  class="probe--fail"
                  :title="probing[n.node_id].result.error"
                >
                  ✗ Terblokir Firewall/IP
                </span>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<style scoped>
.summary {
  font-size: 0.78rem;
  font-weight: 500;
  color: var(--muted);
}

.warning-container {
  display: flex;
  flex-direction: column;
  gap: 0.45rem;
  margin-bottom: 0.85rem;
}

.alert--warn {
  background: var(--amber-bg);
  border: 1px solid var(--amber);
  color: var(--amber);
  border-radius: var(--radius);
  padding: 0.55rem 0.85rem;
  font-size: 0.84rem;
}

.tip-badge {
  margin-left: 0.4rem;
  font-size: 0.7rem;
  cursor: help;
}

.table-wrap {
  overflow-x: auto;
}

.probe-cell {
  white-space: nowrap;
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.btn--sm {
  font-size: 0.75rem;
  padding: 0.22rem 0.55rem;
}

.probe-feedback {
  font-size: 0.78rem;
  font-weight: 600;
}

.probe--ok {
  color: var(--green);
}

.probe--fail {
  color: var(--red);
  cursor: help;
}
</style>