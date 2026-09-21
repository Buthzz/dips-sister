<template>
  <div class="app">
    <header class="header">
      <h1>🖼️ Distributed Image Processing</h1>
      <p class="subtitle">Sistem Terdistribusi — Tugas Kelompok</p>
      <div class="cluster-status">
        <span :class="['dot', clusterOk ? 'dot-green' : 'dot-red']"></span>
        {{ clusterOk ? aliveCount + ' node aktif' : 'Menghubungi master...' }}
      </div>
    </header>

    <main class="main">
      <!-- Upload section -->
      <section class="card upload-card">
        <h2>Upload Gambar</h2>
        <p class="hint">Upload lebih dari 4 gambar untuk memanfaatkan semua node. Format: JPEG / PNG. Maks 5 MB/gambar.</p>

        <div
          class="drop-zone"
          :class="{ dragging: isDragging }"
          @dragover.prevent="isDragging = true"
          @dragleave="isDragging = false"
          @drop.prevent="onDrop"
          @click="$refs.fileInput.click()"
        >
          <input ref="fileInput" type="file" accept=".jpg,.jpeg,.png" multiple hidden @change="onFileChange" />
          <div v-if="selectedFiles.length === 0">
            <p>Klik atau drag &amp; drop gambar ke sini</p>
          </div>
          <div v-else>
            <p>{{ selectedFiles.length }} file dipilih:</p>
            <ul class="file-list">
              <li v-for="f in selectedFiles" :key="f.name">{{ f.name }} ({{ fmtSize(f.size) }})</li>
            </ul>
          </div>
        </div>

        <div class="options-row">
          <label>
            <input type="checkbox" v-model="opts.grayscale" />
            Grayscale
          </label>
          <label>
            Resize maks
            <input type="number" v-model.number="opts.resizeWidth" min="0" max="4000" placeholder="lebar" />
            ×
            <input type="number" v-model.number="opts.resizeHeight" min="0" max="4000" placeholder="tinggi" />
            px (0 = tidak diubah)
          </label>
        </div>

        <button class="btn-primary" :disabled="selectedFiles.length === 0 || submitting" @click="submitJob">
          {{ submitting ? 'Mengirim...' : 'Proses Gambar' }}
        </button>
        <p v-if="submitError" class="error">{{ submitError }}</p>
      </section>

      <!-- Nodes section -->
      <section class="card">
        <h2>Status Node <button class="btn-sm" @click="fetchNodes">↺ Refresh</button></h2>
        <div v-if="nodes.length === 0" class="empty">Belum ada node terdaftar</div>
        <table v-else class="table">
          <thead><tr><th>Node ID</th><th>Alamat</th><th>Status</th><th>Task Aktif</th><th>Kapasitas</th><th>Heartbeat</th></tr></thead>
          <tbody>
            <tr v-for="n in nodes" :key="n.node_id">
              <td><code>{{ n.node_id }}</code></td>
              <td><code>{{ n.addr }}</code></td>
              <td><span :class="['badge', n.status === 'alive' ? 'badge-green' : 'badge-red']">{{ n.status }}</span></td>
              <td>{{ n.active_tasks }}</td>
              <td>{{ n.capacity }}</td>
              <td>{{ fmtTime(n.last_heartbeat) }}</td>
            </tr>
          </tbody>
        </table>
      </section>

      <!-- Jobs section -->
      <section class="card">
        <h2>Daftar Job <button class="btn-sm" @click="fetchJobs">↺ Refresh</button></h2>
        <div v-if="jobs.length === 0" class="empty">Belum ada job</div>
        <div v-for="job in jobs" :key="job.id" class="job-card" @click="toggleJob(job.id)">
          <div class="job-header">
            <code>{{ job.id }}</code>
            <span :class="['badge', statusClass(job.status)]">{{ job.status }}</span>
            <span class="job-progress">{{ job.done }}/{{ job.total }} selesai</span>
            <span v-if="job.failed > 0" class="job-failed">{{ job.failed }} gagal</span>
            <small>{{ fmtDate(job.created_at) }}</small>
            <button class="btn-sm btn-danger" @click.stop="deleteJob(job.id)">Hapus</button>
          </div>

          <!-- Task detail (expanded) -->
          <div v-if="expandedJob === job.id && jobDetail">
            <div v-if="loadingDetail" class="loading">Memuat...</div>
            <table v-else class="table task-table">
              <thead><tr><th>Task</th><th>File</th><th>Status</th><th>Node</th><th>Retry</th><th>Durasi</th><th>Unduh</th></tr></thead>
              <tbody>
                <tr v-for="t in jobDetail.tasks" :key="t.id">
                  <td><code>{{ t.id.slice(-6) }}</code></td>
                  <td>{{ t.filename }}</td>
                  <td><span :class="['badge', statusClass(t.status)]">{{ t.status }}</span></td>
                  <td>{{ t.assigned_to || '-' }}</td>
                  <td>{{ t.retries }}</td>
                  <td>{{ t.duration_ms ? t.duration_ms + ' ms' : '-' }}</td>
                  <td>
                    <a v-if="t.status === 'DONE'"
                       :href="`/api/v1/jobs/${job.id}/results/${t.filename}`"
                       download
                       class="btn-sm"
                       @click.stop>⬇ Unduh</a>
                    <span v-else>-</span>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </section>
    </main>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'

// ─── State ────────────────────────────────────────────────────────────────────
const nodes       = ref([])
const jobs        = ref([])
const jobDetail   = ref(null)
const expandedJob = ref(null)
const loadingDetail = ref(false)

const selectedFiles = ref([])
const isDragging    = ref(false)
const submitting    = ref(false)
const submitError   = ref('')

const opts = ref({ grayscale: true, resizeWidth: 800, resizeHeight: 800 })

// ─── Computed ─────────────────────────────────────────────────────────────────
const aliveCount = computed(() => nodes.value.filter(n => n.status === 'alive').length)
const clusterOk  = computed(() => aliveCount.value > 0)

// ─── Lifecycle ────────────────────────────────────────────────────────────────
let pollInterval

onMounted(() => {
  fetchNodes()
  fetchJobs()
  pollInterval = setInterval(() => {
    fetchNodes()
    fetchJobs()
    if (expandedJob.value) fetchJobDetail(expandedJob.value)
  }, 2000)
})

onUnmounted(() => clearInterval(pollInterval))

// ─── API calls ────────────────────────────────────────────────────────────────
async function fetchNodes() {
  try {
    const r = await fetch('/api/v1/nodes')
    nodes.value = await r.json()
  } catch { /* ignore */ }
}

async function fetchJobs() {
  try {
    const r = await fetch('/api/v1/jobs')
    jobs.value = await r.json()
  } catch { /* ignore */ }
}

async function fetchJobDetail(id) {
  loadingDetail.value = true
  try {
    const r = await fetch(`/api/v1/jobs/${id}`)
    jobDetail.value = await r.json()
  } catch { /* ignore */ }
  finally { loadingDetail.value = false }
}

async function submitJob() {
  submitError.value = ''
  submitting.value = true
  const fd = new FormData()
  for (const f of selectedFiles.value) fd.append('images', f)
  fd.append('options', JSON.stringify({
    grayscale: opts.value.grayscale,
    resize_width: opts.value.resizeWidth,
    resize_height: opts.value.resizeHeight
  }))
  try {
    const r = await fetch('/api/v1/jobs', { method: 'POST', body: fd })
    if (!r.ok) {
      const err = await r.json()
      submitError.value = err.error || `HTTP ${r.status}`
    } else {
      selectedFiles.value = []
      await fetchJobs()
    }
  } catch (e) {
    submitError.value = e.message
  } finally {
    submitting.value = false
  }
}

async function deleteJob(id) {
  await fetch(`/api/v1/jobs/${id}`, { method: 'DELETE' })
  if (expandedJob.value === id) { expandedJob.value = null; jobDetail.value = null }
  await fetchJobs()
}

// ─── UI helpers ───────────────────────────────────────────────────────────────
function onFileChange(e) { selectedFiles.value = Array.from(e.target.files) }
function onDrop(e) {
  isDragging.value = false
  selectedFiles.value = Array.from(e.dataTransfer.files).filter(f =>
    f.type === 'image/jpeg' || f.type === 'image/png'
  )
}

async function toggleJob(id) {
  if (expandedJob.value === id) {
    expandedJob.value = null; jobDetail.value = null; return
  }
  expandedJob.value = id
  await fetchJobDetail(id)
}

function statusClass(s) {
  return { DONE: 'badge-green', FAILED: 'badge-red', PROCESSING: 'badge-yellow', PENDING: 'badge-gray' }[s] || 'badge-gray'
}

function fmtSize(bytes) {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1024 / 1024).toFixed(1) + ' MB'
}

function fmtDate(iso) { return iso ? new Date(iso).toLocaleString('id-ID') : '-' }
function fmtTime(iso) {
  if (!iso) return '-'
  const d = new Date(iso)
  return d.toLocaleTimeString('id-ID')
}
</script>

<style scoped>
* { box-sizing: border-box; }
.app { font-family: 'Segoe UI', system-ui, sans-serif; max-width: 1100px; margin: 0 auto; padding: 1rem; color: #1a1a2e; }

.header { text-align: center; margin-bottom: 1.5rem; }
.header h1 { font-size: 1.8rem; margin: 0; }
.subtitle { color: #555; margin: 0.25rem 0 0.5rem; }
.cluster-status { display: inline-flex; align-items: center; gap: 0.4rem; font-size: 0.9rem; }
.dot { width: 10px; height: 10px; border-radius: 50%; }
.dot-green { background: #22c55e; }
.dot-red { background: #ef4444; }

.main { display: flex; flex-direction: column; gap: 1.2rem; }
.card { background: #fff; border: 1px solid #e2e8f0; border-radius: 10px; padding: 1.2rem; box-shadow: 0 1px 3px rgba(0,0,0,.07); }
.card h2 { margin: 0 0 0.8rem; font-size: 1.1rem; display: flex; align-items: center; gap: 0.5rem; }

.upload-card .hint { color: #666; font-size: 0.85rem; margin: 0 0 0.8rem; }
.drop-zone { border: 2px dashed #94a3b8; border-radius: 8px; padding: 2rem; text-align: center; cursor: pointer; transition: background .15s; }
.drop-zone:hover, .drop-zone.dragging { background: #f0f9ff; border-color: #3b82f6; }
.file-list { list-style: none; padding: 0; margin: 0.5rem 0 0; font-size: 0.85rem; }

.options-row { display: flex; gap: 1.5rem; align-items: center; margin: 0.8rem 0; flex-wrap: wrap; font-size: 0.9rem; }
.options-row input[type=number] { width: 70px; padding: 2px 4px; border: 1px solid #cbd5e1; border-radius: 4px; }

.btn-primary { background: #3b82f6; color: #fff; border: none; border-radius: 6px; padding: 0.5rem 1.4rem; font-size: 1rem; cursor: pointer; }
.btn-primary:disabled { opacity: 0.6; cursor: not-allowed; }
.btn-sm { background: #e2e8f0; border: none; border-radius: 4px; padding: 2px 8px; font-size: 0.8rem; cursor: pointer; }
.btn-danger { background: #fee2e2; color: #b91c1c; }
.error { color: #b91c1c; font-size: 0.9rem; margin-top: 0.5rem; }
.empty { color: #94a3b8; font-style: italic; }
.loading { color: #94a3b8; }

.table { width: 100%; border-collapse: collapse; font-size: 0.85rem; }
.table th { text-align: left; padding: 6px 8px; border-bottom: 2px solid #e2e8f0; white-space: nowrap; }
.table td { padding: 5px 8px; border-bottom: 1px solid #f1f5f9; }

.badge { display: inline-block; padding: 1px 8px; border-radius: 999px; font-size: 0.78rem; font-weight: 600; }
.badge-green  { background: #dcfce7; color: #166534; }
.badge-red    { background: #fee2e2; color: #991b1b; }
.badge-yellow { background: #fef9c3; color: #854d0e; }
.badge-gray   { background: #f1f5f9; color: #475569; }

.job-card { border: 1px solid #e2e8f0; border-radius: 8px; margin-bottom: 0.6rem; overflow: hidden; cursor: pointer; }
.job-header { display: flex; align-items: center; gap: 0.6rem; flex-wrap: wrap; padding: 0.6rem 0.8rem; background: #f8fafc; }
.job-header:hover { background: #f0f9ff; }
.job-progress { color: #64748b; font-size: 0.85rem; }
.job-failed { color: #b91c1c; font-size: 0.85rem; }
.task-table { margin: 0; border-top: 1px solid #e2e8f0; }

code { font-family: 'Consolas', monospace; font-size: 0.85em; background: #f8fafc; padding: 1px 4px; border-radius: 3px; }
</style>
