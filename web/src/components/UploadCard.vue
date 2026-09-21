<script setup>
import { ref, reactive } from 'vue'
import { createJob } from '../api.js'
import { formatSize } from '../format.js'

const emit = defineEmits(['created'])

const MAX_IMAGES = 20
const MAX_MB = 5

const files = ref([])
const dragging = ref(false)
const submitting = ref(false)
const error = ref('')
const success = ref('')

const options = reactive({
  grayscale: true,
  resizeWidth: 800,
  resizeHeight: 800,
})

function tambahBerkas(list) {
  const pesanError = []
  const terima = []
  for (const f of Array.from(list)) {
    if (!/\.(jpe?g|png)$/i.test(f.name)) {
      pesanError.push(`"${f.name}" bukan JPG/JPEG/PNG`)
      continue
    }
    if (f.size > MAX_MB * 1024 * 1024) {
      pesanError.push(`"${f.name}" melebihi ${MAX_MB} MB`)
      continue
    }
    terima.push(f)
  }

  const sisa = MAX_IMAGES - files.value.length
  const ditambah = terima.slice(0, sisa)
  if (terima.length > sisa) {
    pesanError.push(`maksimal ${MAX_IMAGES} berkas per job, sisa hanya ${sisa}`)
  }

  files.value = [...files.value, ...ditambah]
  error.value = pesanError.length ? pesanError.join(' · ') : ''
  success.value = ''
}

function onFileChange(e) {
  tambahBerkas(e.target.files)
  e.target.value = ''
}

function onDrop(e) {
  dragging.value = false
  tambahBerkas(e.dataTransfer.files)
}

function hapusBerkas(i) {
  files.value.splice(i, 1)
}

async function submit() {
  error.value = ''
  success.value = ''
  submitting.value = true
  try {
    const body = await createJob(files.value, {
      grayscale: options.grayscale,
      resize_width: options.resizeWidth || 0,
      resize_height: options.resizeHeight || 0,
    })
    success.value = `Job ${body.job_id} diterima — ${body.tasks} gambar diproses asinkron.`
    files.value = []
    emit('created')
  } catch (e) {
    error.value = e.message
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <section class="card">
    <h2 class="card-title">
      <span class="title-left">Upload Gambar</span>
      <span class="limit">maks {{ MAX_IMAGES }} berkas &times; {{ MAX_MB }} MB</span>
    </h2>

    <div
      class="dropzone"
      :class="{ 'dropzone--active': dragging }"
      @dragover.prevent="dragging = true"
      @dragleave="dragging = false"
      @drop.prevent="onDrop"
      @click="$refs.input.click()"
    >
      <input
        ref="input"
        type="file"
        accept=".jpg,.jpeg,.png"
        multiple
        hidden
        @change="onFileChange"
      />

      <template v-if="files.length === 0">
        <div class="dropzone-icon">+</div>
        <p>Klik area ini atau tarik &amp; lepas gambar ke sini</p>
        <small>Hanya JPG, JPEG, atau PNG</small>
      </template>
      <template v-else>
        <p class="dropzone-count">{{ files.length }} berkas dipilih</p>
        <ul class="file-list">
          <li v-for="(f, i) in files" :key="f.name + i">
            <span class="file-name">{{ f.name }}</span>
            <span class="file-size">{{ formatSize(f.size) }}</span>
            <button class="btn--ghost" type="button" title="Hapus berkas" @click.stop="hapusBerkas(i)">hapus</button>
          </li>
        </ul>
      </template>
    </div>

    <div class="options">
      <label class="option">
        <input v-model="options.grayscale" type="checkbox" />
        Grayscale (BT.601)
      </label>
      <label class="option">
        Resize maks
        <input v-model.number="options.resizeWidth" type="number" min="0" max="4096" placeholder="lebar" />
        &times;
        <input v-model.number="options.resizeHeight" type="number" min="0" max="4096" placeholder="tinggi" />
        px
      </label>
      <span class="option-hint">Nilai 0 = pertahankan ukuran asli</span>
    </div>

    <p v-if="files.length > 0 && files.length <= 4" class="node-hint">
      Tip: unggah lebih dari 4 gambar agar seluruh node worker terpakai.
    </p>

    <div class="submit-row">
      <button class="btn btn--primary" :disabled="files.length === 0 || submitting" @click="submit">
        {{ submitting ? 'Mengirim & memproses...' : 'Proses Gambar' }}
      </button>
    </div>

    <p v-if="error" class="alert alert--error">{{ error }}</p>
    <p v-if="success" class="alert alert--ok">{{ success }}</p>
  </section>
</template>

<style scoped>
.card-title .limit {
  font-size: 0.75rem;
  color: var(--muted);
  font-weight: 500;
}

.dropzone {
  border: 2px dashed var(--border-strong);
  border-radius: 8px;
  padding: 1.6rem 1rem;
  text-align: center;
  cursor: pointer;
  transition: background 0.15s ease, border-color 0.15s ease;
  color: var(--muted);
  font-size: 0.92rem;
}

.dropzone:hover,
.dropzone--active {
  background: var(--primary-weak);
  border-color: var(--primary);
}

.dropzone p {
  margin: 0.2rem 0;
}

.dropzone small {
  font-size: 0.78rem;
}

.dropzone-icon {
  width: 38px;
  height: 38px;
  margin: 0 auto 0.4rem;
  border-radius: 50%;
  background: var(--primary-weak);
  color: var(--primary);
  font-size: 1.5rem;
  line-height: 36px;
  font-weight: 600;
}

.dropzone-count {
  font-weight: 600;
  color: var(--text);
}

.file-list {
  list-style: none;
  margin: 0.5rem 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  text-align: left;
}

.file-list li {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.82rem;
  background: var(--slate-bg);
  border-radius: 6px;
  padding: 4px 8px;
}

.file-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.file-size {
  color: var(--muted);
  white-space: nowrap;
}

.options {
  display: flex;
  align-items: center;
  gap: 1.2rem;
  flex-wrap: wrap;
  margin: 0.9rem 0 0.6rem;
  font-size: 0.9rem;
}

.option {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
}

.option input[type='number'] {
  width: 64px;
  padding: 3px 6px;
  border: 1px solid var(--border-strong);
  border-radius: 5px;
  font: inherit;
}

.option-hint {
  font-size: 0.75rem;
  color: var(--muted);
}

.node-hint {
  font-size: 0.82rem;
  color: var(--amber);
  background: var(--amber-bg);
  border-left: 3px solid var(--amber);
  padding: 0.4rem 0.6rem;
  border-radius: 4px;
}

.submit-row {
  display: flex;
  justify-content: flex-end;
  margin-top: 0.4rem;
}

.alert {
  margin-bottom: 0;
  margin-top: 0.8rem;
}
</style>