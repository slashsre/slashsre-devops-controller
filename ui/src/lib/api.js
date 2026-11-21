import { writable } from 'svelte/store'

const TOKEN_KEY = 'slashsre-api-token'

export const resources = writable({ loading: true, error: null, data: null, updatedAt: null })

function readToken() {
  try {
    return sessionStorage.getItem(TOKEN_KEY) || ''
  } catch {
    return ''
  }
}

function saveToken(token) {
  try {
    sessionStorage.setItem(TOKEN_KEY, token)
  } catch {
    /* storage unavailable: token is only kept for this request */
  }
}

export async function refreshResources() {
  try {
    const res = await fetch('/api/v1/resources')
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    const data = await res.json()
    resources.set({ loading: false, error: null, data, updatedAt: new Date() })
  } catch (err) {
    resources.update((s) => ({ ...s, loading: false, error: err.message }))
  }
}

let timer
export function startPolling(intervalMs = 15000) {
  refreshResources()
  clearInterval(timer)
  timer = setInterval(refreshResources, intervalMs)
  return () => clearInterval(timer)
}

// runResource calls a mutating /run endpoint, asking for the API token on 401.
export async function runResource(plural, namespace, name, promptText) {
  const url = `/api/v1/namespaces/${encodeURIComponent(namespace)}/${plural}/${encodeURIComponent(name)}/run`
  const call = (token) =>
    fetch(url, { method: 'POST', headers: token ? { Authorization: `Bearer ${token}` } : {} })

  let res = await call(readToken())
  if (res.status === 401) {
    const token = window.prompt(promptText)
    if (!token) throw new Error('unauthorized')
    saveToken(token)
    res = await call(token)
  }
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.message || `HTTP ${res.status}`)
  refreshResources()
  return body
}

export function readyCondition(obj) {
  const cond = (obj.status?.conditions || []).find((c) => c.type === 'Ready')
  if (!cond) return { status: 'Unknown', message: '' }
  return { status: cond.status, message: cond.message || cond.reason }
}

export function since(ts) {
  if (!ts) return '—'
  const secs = Math.max(0, Math.round((Date.now() - new Date(ts).getTime()) / 1000))
  if (secs < 60) return `${secs}s`
  if (secs < 3600) return `${Math.round(secs / 60)}m`
  if (secs < 86400) return `${Math.round(secs / 3600)}h`
  return `${Math.round(secs / 86400)}d`
}
