import * as wails from '../wailsjs/go/main/App'

function inWails() {
  return typeof window !== 'undefined' && Boolean(window.go?.main?.App)
}

let webSessionPromise

async function webSession() {
  if (!webSessionPromise) {
    webSessionPromise = fetch('/api/session', { cache: 'no-store' })
      .then(async response => {
        const payload = await response.json().catch(() => ({}))
        if (!response.ok || !payload.token) throw new Error('Could not start the local web session')
        return payload
      })
      .catch(error => {
        webSessionPromise = undefined
        throw error
      })
  }
  return webSessionPromise
}

async function webToken() {
  const session = await webSession()
  return session.token
}

async function invoke(method, args) {
  if (inWails()) return wails[method](...args)
  const token = await webToken()
  const response = await fetch(`/api/${method}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-DevHub-Token': token },
    body: JSON.stringify({ args }),
  })
  const payload = await response.json().catch(() => ({}))
  if (!response.ok || payload.error) throw new Error(payload.error || 'Local API request failed')
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
    input.style.display = 'none'
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
export const GetWebCapabilities = async () => {
  if (inWails()) return { workspace: true }
  const session = await webSession()
  return { workspace: session.workspace !== false }
}
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
export const AddEntry = (...args) => invoke('AddEntry', args)
export const UpdateEntry = (...args) => invoke('UpdateEntry', args)
export const DeleteEntries = (...args) => invoke('DeleteEntries', args)
export const GeneratePassword = (...args) => invoke('GeneratePassword', args)
export const GetTOTPCode = (...args) => invoke('GetTOTPCode', args)
export const GetEntryHistory = (...args) => invoke('GetEntryHistory', args)
export const ExportVault = async exportPassword => {
  if (inWails()) return wails.ExportVault(exportPassword)
  const token = await webToken()
  const response = await fetch('/api/export', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-DevHub-Token': token },
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
  const token = await webToken()
  const form = new FormData()
  form.append('password', zipPassword)
  form.append('file', file, file.name)
  const response = await fetch('/api/import', {
    method: 'POST',
    headers: { 'X-DevHub-Token': token },
    body: form,
  })
  if (!response.ok) throw await webError(response, 'Vault import failed')
  const payload = await response.json()
  if (payload.error) throw new Error(payload.error)
  return payload.result
}
export const ListProjects = () => invoke('ListProjects', [])
export const AddProject = (...args) => invoke('AddProject', args)
export const UpdateProject = (...args) => invoke('UpdateProject', args)
export const DeleteProject = (...args) => invoke('DeleteProject', args)
export const ChooseProjectDirectory = () => invoke('ChooseProjectDirectory', [])
export const OpenProject = (...args) => invoke('OpenProject', args)
export const OpenProjectWith = (...args) => invoke('OpenProjectWith', args)
