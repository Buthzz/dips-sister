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
</script>

<template>
  <section class="card">
    <h2 class="card-title">
      <span class="title-left">Status Node</span>
      <span class="summary">{{ alive }} aktif dari {{ nodes.length }} terdaftar</span>
      <button class="btn" @click="emit('refresh')">Refresh</button>
    </h2>

    <div v-if="nodes.length === 0" class="empty-note">
      Belum ada node yang mendaftar. Nyalakan laptop worker (mode node) untuk bergabung ke kluster.
    </div>

    <div v-else-if="nodes.length > 0 && alive === 0" class="alert alert--warn">
      Tidak ada node node worker aktif — semua task akan dieksekusi oleh master (graceful degradation).
    </div>

    <div v-else>
      <table class="table">
        <thead>
          <tr>
            <th>Node ID</th>
            <th>Alamat</th>
            <th>Status</th>
            <th>Task Aktif</th>
            <th>Kapasitas</th>
            <th>Heartbeat Terakhir</th>
            <th>Tindakan</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="n in nodes" :key="n.node_id">
            <td><code>{{ n.node_id }}</code></td>
            <td><code>{{ n.addr }}</code></td>
            <td><span class="badge" :class="statusBadgeClass(n.status)">{{ n.status }}</span></td>
            <td>{{ n.active_tasks }}</td>
            <td>{{ n.capacity }}</td>
            <td>{{ formatTime(n.last_heartbeat) }}</td>
            <td class="probe-cell">
              <button
                class="btn btn--sm"
                :disabled="probing[n.node_id]?.loading"
                @click="testaKoneksi(n.node_id)"
              >
                {{ probing[n.node_id]?.loading ? '...' : 'Tes Koneksi' }}
              </button>
              <span
                v-if="probing[n.node_id]?.result"
                class="probe-result"
                :class="probing[n.node_id].result.reachable ? 'probe--ok' : 'probe--fail'"
              >
                {{
                  probing[n.node_id].result.reachable
                    ? `✅ ${probing[n.node_id].result.latency_ms} ms`
                    : `❌ Tidak terjangkau`
                }}
              </span>
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

.probe-cell {
  white-space: nowrap;
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.btn--sm {
  font-size: 0.75rem;
  padding: 0.2rem 0.55rem;
}

.probe-result {
  font-size: 0.78rem;
  font-weight: 500;
}

.probe--ok {
  color: var(--green);
}

.probe--fail {
  color: var(--red);
}
</style>