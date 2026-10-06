import * as wails from '../wailsjs/go/main/App'

// The HTTP server injects a base element at runtime; desktop builds use /.
export const BASE_PATH = document.querySelector('base')?.getAttribute('href') || '/'
export function withBasePath(path) {
  return `${BASE_PATH}${String(path).replace(/^\/+/, '')}`
}

function inWails() {
  return typeof window !== 'undefined' && Boolean(window.go?.main?.App)
}

async function invoke(method, args) {
  if (inWails()) return wails[method](...args)
  const response = await fetch(withBasePath(`api/${method}`), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify({ args }),
  })
  const payload = await response.json().catch(() => ({}))
  if (!response.ok || payload.error) throw new Error(payload.error || 'Local API request failed')
  return payload.result
}

async function auth(path, options = {}) {
  const response = await fetch(withBasePath(`api/auth${path}`), { credentials: 'same-origin', ...options })
  const payload = await response.json().catch(() => ({}))
  if (!response.ok || payload.error) throw new Error(payload.error || 'Authentication request failed')
  return payload.result
}

async function webError(response, fallback) {
  const payload = await response.json().catch(() => ({}))
  return new Error(payload.error || fallback)
}

function downloadBlob(blob, filename) {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  anchor.style.display = 'none'
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}

function chooseVaultZip() {
  return new Promise(resolve => {
    const input = document.createElement('input')
    input.type = 'file'
    input.accept = '.zip,application/zip'
    // Keep the input in the document but visually hidden. Safari can ignore
    // programmatic clicks on an element with display:none.
    input.style.position = 'fixed'
    input.style.left = '-10000px'
    input.style.top = '0'
    input.style.opacity = '0'
    let finished = false
    const finish = file => {
      if (finished) return
      finished = true
      window.removeEventListener('focus', onWindowFocus)
      input.remove()
      resolve(file || null)
    }
    const onWindowFocus = () => window.setTimeout(() => finish(input.files?.[0]), 300)
    input.addEventListener('change', () => finish(input.files?.[0]), { once: true })
    input.addEventListener('cancel', () => finish(null), { once: true })
    window.addEventListener('focus', onWindowFocus, { once: true })
    document.body.appendChild(input)
    input.click()
  })
}

export const IsInitialized = () => invoke('IsInitialized', [])
export const GetAuthStatus = async () => {
  if (inWails()) return null
  return auth('/status')
}
export const BootstrapAdmin = (password, masterPassword, code) => auth('/bootstrap', {
  method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username: "admin", password, masterPassword, code }),
})
export const Login = (password, code) => auth('/login', {
  method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username: "admin", password, code }),
})
export const Logout = () => auth('/logout', { method: 'POST' })
export const IsUnlocked = () => invoke('IsUnlocked', [])
export const Initialize = (...args) => invoke('Initialize', args)
export const Unlock = (...args) => invoke('Unlock', args)
export const Lock = () => invoke('Lock', [])
export const GetCategories = () => invoke('GetCategories', [])
export const AddCategory = (...args) => invoke('AddCategory', args)
export const RenameCategory = (...args) => invoke('RenameCategory', args)
export const DeleteCategory = (...args) => invoke('DeleteCategory', args)
export const ListEntries = (...args) => invoke('ListEntries', args)
export const SearchEntries = (...args) => invoke('SearchEntries', args)
export const SaveVaultEntry = (...args) => invoke('SaveVaultEntry', args)
export const ExportSSHPrivateKey = async entry => {
  if (inWails()) return wails.ExportSSHPrivateKey(entry.Name)
  downloadBlob(new Blob([entry.SSH.PrivateKey], { type: 'application/octet-stream' }), `id_vault_${entry.ID}`)
  return true
}
export const AddEntry = (...args) => invoke('AddEntry', args)
export const UpdateEntry = (...args) => invoke('UpdateEntry', args)
export const DeleteEntries = (...args) => invoke('DeleteEntries', args)
export const GeneratePassword = (...args) => invoke('GeneratePassword', args)
export const GetTOTPCode = (...args) => invoke('GetTOTPCode', args)
export const GetEntryHistory = (...args) => invoke('GetEntryHistory', args)
export const ExportVault = async exportPassword => {
  if (inWails()) return wails.ExportVault(exportPassword)
  const response = await fetch(withBasePath('api/export'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' }, credentials: 'same-origin',
    body: JSON.stringify({ password: exportPassword }),
  })
  if (!response.ok) throw await webError(response, 'Vault export failed')
  const disposition = response.headers.get('Content-Disposition') || ''
  const match = disposition.match(/filename="?([^";]+)"?/i)
  const filename = match?.[1] || 'vault-export.zip'
  const entryCount = Number(response.headers.get('X-DevHub-Entry-Count') || 0)
  downloadBlob(await response.blob(), filename)
  return { Path: filename, EntryCount: entryCount }
}
export const ImportVault = async zipPassword => {
  if (inWails()) return wails.ImportVault(zipPassword)
  const file = await chooseVaultZip()
  if (!file) return null
  const form = new FormData()
  form.append('password', zipPassword)
  form.append('file', file, file.name)
  const response = await fetch(withBasePath('api/import'), {
    method: 'POST',
    credentials: 'same-origin',
    body: form,
  })
  if (!response.ok) throw await webError(response, 'Vault import failed')
  const payload = await response.json()
  if (payload.error) throw new Error(payload.error)
  return payload.result
}
