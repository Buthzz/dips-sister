<script setup>
import { ref } from 'vue'
import { fetchJobDetail, deleteJob, resultUrl, statusBadgeClass } from '../api.js'
import { formatDateTime } from '../format.js'

const props = defineProps({
  jobs: { type: Array, default: () => [] },
})

const emit = defineEmits(['delete', 'changed'])

const expandedId = ref(null)
const detail = ref(null)
const loadingDetail = ref(false)

function persen(job) {
  return job.total > 0 ? Math.round((job.done / job.total) * 100) : 0
}

async function toggle(id) {
  if (expandedId.value === id) {
    expandedId.value = null
    detail.value = null
    return
  }
  expandedId.value = id
  detail.value = null
  loadingDetail.value = true
  try {
    detail.value = await fetchJobDetail(id)
  } finally {
    loadingDetail.value = false
  }
}

async function hapus(id) {
  if (!window.confirm(`Hapus job ${id} beserta seluruh berkasnya?`)) return
  try {
    await deleteJob(id)
    if (expandedId.value === id) {
      expandedId.value = null
      detail.value = null
    }
    emit('delete')
  } catch {
    /* error dibiarkan, daftar disegarkan saat polling berikutnya */
  }
}

function analisaSolusi(errorMsg) {
  if (!errorMsg) {
    return 'Periksa status worker node dan log terminal master untuk detail masalah.'
  }
  const msg = errorMsg.toLowerCase()
  if (msg.includes('timeout') || msg.includes('dial') || msg.includes('connection error')) {
    return 'Master gagal membuka koneksi TCP ke worker node. Periksa apakah Windows Defender Firewall di laptop worker memblokir port gRPC (9000), atau apakah alamat IP worker berubah.'
  }
  if (msg.includes('unreachable') || msg.includes('no route') || msg.includes('destination host unreachable')) {
    return 'Alamat IP worker tidak dapat dijangkau dari Master. Pastikan kedua laptop terhubung ke jaringan Wi-Fi/Hotspot yang sama dan tidak mengaktifkan AP Isolation.'
  }
  if (msg.includes('format alamat') || msg.includes('host:port')) {
    return 'Format alamat advertise salah (misal memakai titik alih-alih titik dua). Jalankan worker dengan format --advertise=<IP>:9000.'
  }
  if (msg.includes('maksimum percobaan')) {
    return 'Task gagal setelah percobaan ulang maksimal. Kemungkinan worker mengalami crash atau terputus saat memproses citra.'
  }
  return 'Periksa kecocokan token kluster (--token) dan log terminal pada node worker.'
}
</script>

<template>
  <section class="card">
    <h2 class="card-title">
      <span class="title-left">Daftar Job</span>
      <span v-if="jobs.length" class="summary">{{ jobs.length }} job</span>
    </h2>

    <div v-if="jobs.length === 0" class="empty-note">
      Belum ada job. Upload gambar untuk memulai pemrosesan terdistribusi.
    </div>

    <div v-else class="job-list">
      <div v-for="job in jobs" :key="job.id" class="job" :class="{ 'job--open': expandedId === job.id }">
        <div class="job-head" @click="toggle(job.id)">
          <code class="job-id">{{ job.id }}</code>
          <span class="badge" :class="statusBadgeClass(job.status)">{{ job.status }}</span>
          <div class="job-progress">
            <div class="bar">
              <div class="bar-fill" :class="'bar-fill--' + (job.status === 'FAILED' ? 'red' : 'green')" :style="{ width: persen(job) + '%' }"></div>
            </div>
            <span>{{ job.done }}/{{ job.total }} selesai</span>
          </div>
          <span v-if="job.failed" class="job-failed">{{ job.failed }} gagal</span>
          <span class="job-date">{{ formatDateTime(job.created_at) }}</span>
          <button class="btn btn--danger" title="Hapus job" @click.stop="hapus(job.id)">Hapus</button>
        </div>

        <div v-if="expandedId === job.id" class="job-detail">
          <p v-if="loadingDetail" class="empty-note">Memuat rincian task...</p>
          <template v-else-if="detail">
            <table class="table" v-if="detail.tasks && detail.tasks.length">
              <thead>
                <tr>
                  <th>Task</th>
                  <th>Berkas</th>
                  <th>Status</th>
                  <th>Node</th>
                  <th>Retry</th>
                  <th>Durasi</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                <template v-for="t in detail.tasks" :key="t.id">
                  <tr :class="{ 'row-has-error': t.error || t.status === 'FAILED' }">
                    <td><code>{{ t.id.slice(-3) }}</code></td>
                    <td>
                      <span class="task-name">{{ t.filename }}</span>
                      <span v-if="t.error" class="task-error" :title="t.error"> (!)</span>
                    </td>
                    <td><span class="badge" :class="statusBadgeClass(t.status)">{{ t.status }}</span></td>
                    <td><code>{{ t.assigned_to || '-' }}</code></td>
                    <td>{{ t.retries }}</td>
                    <td>{{ t.duration_ms ? t.duration_ms + ' ms' : '-' }}</td>
                    <td>
                      <a
                        v-if="t.status === 'DONE'"
                        class="btn btn--ghost"
                        :href="resultUrl(detail.id, t.filename)"
                        :download="t.filename"
                        @click.stop
                      >
                        Unduh
                      </a>
                      <span v-else>-</span>
                    </td>
                  </tr>
                  <tr v-if="t.error || t.status === 'FAILED'" class="row-diagnostic">
                    <td colspan="7">
                      <div class="diagnostic-box">
                        <div class="diag-header">
                          <span class="diag-icon">⚠️</span>
                          <strong>Diagnostik Task {{ t.id.slice(-3) }} ({{ t.filename }}):</strong>
                          <span v-if="t.assigned_to" class="diag-node">Target: <code>{{ t.assigned_to }}</code></span>
                        </div>
                        <div class="diag-reason">
                          <strong>Penyebab:</strong> <code>{{ t.error || 'Eksekusi gagal pada worker node' }}</code>
                        </div>
                        <div class="diag-action">
                          💡 <strong>Rekomendasi Solusi:</strong> {{ analisaSolusi(t.error) }}
                        </div>
                      </div>
                    </td>
                  </tr>
                </template>
              </tbody>
            </table>
            <p v-else class="empty-note">Tidak ada task.</p>
          </template>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.summary {
  font-size: 0.78rem;
  font-weight: 500;
  color: var(--muted);
}

.job-list {
  display: flex;
  flex-direction: column;
  gap: 0.55rem;
}

.job {
  border: 1px solid var(--border);
  border-radius: 8px;
  overflow: hidden;
}

.job--open {
  border-color: var(--primary);
}

.job-head {
  display: flex;
  align-items: center;
  gap: 0.65rem;
  padding: 0.5rem 0.7rem;
  cursor: pointer;
  flex-wrap: wrap;
  background: var(--surface);
}

.job-head:hover {
  background: var(--primary-weak);
}

.job-id {
  flex: 0 0 auto;
  max-width: 230px;
  overflow: hidden;
  text-overflow: ellipsis;
}

.job-progress {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex: 1 1 180px;
  font-size: 0.78rem;
  color: var(--muted);
  min-width: 150px;
}

.bar {
  flex: 1;
  height: 6px;
  background: var(--slate-bg);
  border-radius: 999px;
  overflow: hidden;
}

.bar-fill {
  height: 100%;
  border-radius: 999px;
  transition: width 0.4s ease;
}

.bar-fill--green {
  background: var(--green);
}

.bar-fill--red {
  background: var(--red);
}

.job-failed {
  font-size: 0.78rem;
  color: var(--red);
}

.job-date {
  font-size: 0.75rem;
  color: var(--muted);
}

.job-detail {
  border-top: 1px solid var(--border);
  padding: 0.6rem 0.7rem 0.3rem;
  overflow-x: auto;
}

.task-name {
  max-width: 260px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  display: inline-block;
  vertical-align: bottom;
}

.task-error {
  color: var(--red);
  font-weight: 700;
  cursor: help;
}

.row-has-error {
  background: rgba(254, 226, 226, 0.25);
}

.row-diagnostic {
  background: rgba(254, 226, 226, 0.4);
}

.row-diagnostic td {
  padding: 0.4rem 0.8rem 0.7rem;
  border-bottom: 1px solid var(--border);
}

.diagnostic-box {
  background: var(--surface);
  border: 1px solid #fca5a5;
  border-left: 4px solid var(--red);
  border-radius: 6px;
  padding: 0.55rem 0.75rem;
  font-size: 0.82rem;
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}

.diag-header {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  color: var(--red);
  font-size: 0.83rem;
}

.diag-node {
  margin-left: auto;
  color: var(--muted);
  font-size: 0.78rem;
}

.diag-reason code {
  color: var(--red);
  word-break: break-all;
  white-space: pre-wrap;
}

.diag-action {
  background: var(--amber-bg);
  border: 1px solid rgba(245, 158, 11, 0.35);
  border-radius: 4px;
  padding: 0.4rem 0.6rem;
  color: #78350f;
  line-height: 1.4;
}
</style>