async function tulisError(r) {
  let pesan = `HTTP ${r.status}`
  try {
    const body = await r.json()
    if (body && typeof body.error === 'string' && body.error) {
      pesan = body.error
    }
  } catch {
    /* badan respons bukan JSON */
  }
  return new Error(pesan)
}

function cekResponse(r) {
  if (!r.ok) throw tulisError(r)
  if (r.status === 204) return null
  return r.json()
}

export function fetchHealth() {
  return fetch('/healthz').then((r) => r.ok)
}

export function fetchNodes() {
  return fetch('/api/v1/nodes').then((r) => cekResponse(r))
}

export function fetchJobs() {
  return fetch('/api/v1/jobs').then((r) => cekResponse(r))
}

export function fetchJobDetail(id) {
  return fetch(`/api/v1/jobs/${encodeURIComponent(id)}`).then((r) => cekResponse(r))
}

export function createJob(files, options) {
  const fd = new FormData()
  for (const f of files) fd.append('images', f)
  fd.append('options', JSON.stringify(options))
  return fetch('/api/v1/jobs', { method: 'POST', body: fd }).then((r) => cekResponse(r))
}

export function deleteJob(id) {
  return fetch(`/api/v1/jobs/${encodeURIComponent(id)}`, { method: 'DELETE' }).then((r) =>
    cekResponse(r)
  )
}

export function probeNode(nodeId) {
  return fetch(`/api/v1/nodes/${encodeURIComponent(nodeId)}/probe`).then((r) => cekResponse(r))
}


export function resultUrl(jobId, filename) {
  return `/api/v1/jobs/${encodeURIComponent(jobId)}/results/${encodeURIComponent(filename)}`
}

export function statusBadgeClass(status) {
  const map = {
    DONE: 'badge--green',
    FAILED: 'badge--red',
    PROCESSING: 'badge--amber',
    RUNNING: 'badge--amber',
    PENDING: 'badge--slate',
    alive: 'badge--green',
    dead: 'badge--red',
  }
  return map[status] || 'badge--slate'
}