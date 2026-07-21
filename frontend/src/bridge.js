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
export const ExportVault = (...args) => invoke('ExportVault', args)
export const ImportVault = (...args) => invoke('ImportVault', args)
export const ListProjects = () => invoke('ListProjects', [])
export const AddProject = (...args) => invoke('AddProject', args)
export const UpdateProject = (...args) => invoke('UpdateProject', args)
export const DeleteProject = (...args) => invoke('DeleteProject', args)
export const ChooseProjectDirectory = () => invoke('ChooseProjectDirectory', [])
export const OpenProject = (...args) => invoke('OpenProject', args)
export const OpenProjectWith = (...args) => invoke('OpenProjectWith', args)
