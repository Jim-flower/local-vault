import { useState, useEffect, useRef } from 'react'
import { ClawCaptcha } from 'playcaptcha'
import 'playcaptcha/clawcaptcha.css'
import {
  IsInitialized, IsUnlocked, Initialize, Unlock, Lock,
  GetAuthStatus, BootstrapAdmin, Login, Logout,
  withBasePath,
  GetCategories, AddCategory, DeleteCategory,
  ListEntries, SearchEntries, AddEntry, UpdateEntry, DeleteEntries,
  GeneratePassword, GetTOTPCode, GetEntryHistory, ExportVault, ImportVault,
} from './bridge'

async function copyText(text) {
  if (navigator.clipboard?.writeText && window.isSecureContext) {
    await navigator.clipboard.writeText(text)
    return
  }

  const input = document.createElement('textarea')
  input.value = text
  input.setAttribute('readonly', '')
  input.style.cssText = 'position:fixed;top:0;left:-9999px;opacity:0'
  document.body.appendChild(input)
  input.select()
  input.setSelectionRange(0, input.value.length)
  const copied = document.execCommand('copy')
  input.remove()
  if (!copied) throw new Error('Clipboard access was denied')
}

// ── Root ─────────────────────────────────────────────────────────────────────

export default function App() {
  const [screen, setScreen] = useState('loading')
  const [currentUser, setCurrentUser] = useState(null)
  const [twoFARequired, setTwoFARequired] = useState(false)

  useEffect(() => {
    GetAuthStatus()
      .then(async status => {
        if (status) {
          setTwoFARequired(status.login2FARequired === true)
          setCurrentUser(status.user || null)
          if (status.bootstrap) { setScreen('admin-setup'); return }
          if (!status.authenticated) { setScreen('login'); return }
          setScreen(status.vaultUnlocked ? 'vault' : 'locked')
          return
        }
        await openVault()
      })
      .catch(() => setScreen('locked'))
  }, [])

  async function openVault() {
    try {
      const initialized = await IsInitialized()
      if (!initialized) { setScreen('setup'); return }
      setScreen((await IsUnlocked()) ? 'vault' : 'locked')
    } catch { setScreen('locked') }
  }

  if (screen === 'loading') return <Splash />
  if (screen === 'admin-setup') return <AdminSetupScreen twoFARequired={twoFARequired} onDone={user => { setCurrentUser(user); setScreen('vault') }} />
  if (screen === 'login') return <LoginScreen twoFARequired={twoFARequired} onDone={user => { setCurrentUser(user); setScreen('locked') }} />
  if (screen === 'setup')   return <LockScreen mode="setup"  onDone={() => setScreen('vault')} />
  if (screen === 'locked')  return <LockScreen mode="unlock" onDone={() => setScreen('vault')} />
  if (screen === 'vault')   return <VaultScreen user={currentUser} onLogout={async () => { await Logout(); setCurrentUser(null); setScreen('login') }} onLock={() => setScreen('locked')} />
  return <Splash />
}

// ── Splash ────────────────────────────────────────────────────────────────────

function Splash() {
  return (
    <div className="splash">
      <span style={{ fontSize: '3rem' }}>🔐</span>
      <div className="spinner" />
    </div>
  )
}

function AuthShell({ title, subtitle, children }) {
  return <div className="lock-bg"><main className="lock-card auth-card"><div className="lock-mark">&#128272;</div><h1>{title}</h1><p className="lock-sub">{subtitle}</p>{children}</main></div>
}

function LoginCodeField({ value, onChange, disabled }) {
  return <div className="field">
    <label htmlFor="login-2fa-code">Authenticator code</label>
    <input id="login-2fa-code" type="text" inputMode="numeric" autoComplete="one-time-code"
      pattern="[0-9]{6}" maxLength={6} required value={value} disabled={disabled}
      onChange={event => onChange(event.target.value.replace(/\D/g, '').slice(0, 6))}
      placeholder="6-digit code" />
  </div>
}

function LoginScreen({ onDone, twoFARequired }) {
  const [code, setCode] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  async function submit(event) {
    event.preventDefault(); setError(''); setBusy(true)
    try { const result = await Login(password, code); onDone(result.user) }
    catch (err) { setError(String(err)) }
    finally { setBusy(false) }
  }
  return <AuthShell title="Sign in" subtitle="Sign in to your personal Vault.">
    <form onSubmit={submit}>
      <div className="account-name">Account: <strong>admin</strong></div>
      <Field label="Account password" type="password" value={password} onChange={event => setPassword(event.target.value)} autoFocus disabled={busy} />
      {twoFARequired && <LoginCodeField value={code} onChange={setCode} disabled={busy} />}
      {error && <div className="err-box">{error}</div>}
      <button className="btn btn-primary w-full" disabled={busy}>{busy ? 'Signing in...' : 'Sign in'}</button>
    </form>
  </AuthShell>
}

function AdminSetupScreen({ onDone, twoFARequired }) {
  const [accountPassword, setAccountPassword] = useState('')
  const [code, setCode] = useState('')
  const [accountConfirm, setAccountConfirm] = useState('')
  const [masterPassword, setMasterPassword] = useState('')
  const [masterConfirm, setMasterConfirm] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  async function submit(event) {
    event.preventDefault(); setError('')
    if (accountPassword.length < 12) { setError('Account password must be at least 12 characters.'); return }
    if (accountPassword !== accountConfirm) { setError('Account passwords do not match.'); return }
    if (!masterPassword) { setError('Enter a vault master password.'); return }
    if (masterPassword !== masterConfirm) { setError('Master passwords do not match.'); return }
    setBusy(true)
    try { const result = await BootstrapAdmin(accountPassword, masterPassword, code); onDone(result.user) }
    catch (err) { setError(String(err)) }
    finally { setBusy(false) }
  }
  return <AuthShell title="Secure Vault setup" subtitle="Create the admin account and the vault master password. Both are required.">
    <form onSubmit={submit}>
      <div className="account-name">Administrator account: <strong>admin</strong></div>
      <Field label="Admin account password" type="password" value={accountPassword} onChange={event => setAccountPassword(event.target.value)} autoFocus disabled={busy} />
      <Field label="Confirm account password" type="password" value={accountConfirm} onChange={event => setAccountConfirm(event.target.value)} disabled={busy} />
      <div className="form-divider">Vault encryption</div>
      <Field label="Vault master password" type="password" value={masterPassword} onChange={event => setMasterPassword(event.target.value)} disabled={busy} />
      <Field label="Confirm master password" type="password" value={masterConfirm} onChange={event => setMasterConfirm(event.target.value)} disabled={busy} />
      {error && <div className="err-box">{error}</div>}
      {twoFARequired && <LoginCodeField value={code} onChange={setCode} disabled={busy} />}
      <button className="btn btn-primary w-full" disabled={busy}>{busy ? 'Creating...' : 'Create secure vault'}</button>
    </form>
  </AuthShell>
}

// ── LockScreen ────────────────────────────────────────────────────────────────

function LockScreen({ mode, onDone }) {
  const [pw, setPw]           = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError]     = useState('')
  const [busy, setBusy]       = useState(false)

  async function submit(e) {
    e.preventDefault(); setError('')
    if (mode === 'setup') {
      if (!pw.trim())     { setError('Password cannot be empty'); return }
      if (pw !== confirm) { setError('Passwords do not match');   return }
    }
    setBusy(true)
    try {
      mode === 'setup' ? await Initialize(pw) : await Unlock(pw)
      onDone()
    } catch (err) { setError(String(err)) }
    finally       { setBusy(false) }
  }

  return (
    <div className="lock-bg">
      <div className="lock-card">
        <div style={{ fontSize: '2.6rem', textAlign: 'center', marginBottom: 12 }}>🔐</div>
        <h1>{mode === 'setup' ? 'Create Your Vault' : 'Unlock Vault'}</h1>
        <p className="lock-sub">
          {mode === 'setup'
            ? 'Choose a master password to protect your entries'
            : 'Enter your master password to continue'}
        </p>
        <form onSubmit={submit}>
          <Field label="Master password" type="password" value={pw}
            onChange={e => setPw(e.target.value)} placeholder="Enter master password"
            autoFocus disabled={busy} />
          {mode === 'setup' && (
            <Field label="Confirm password" type="password" value={confirm}
              onChange={e => setConfirm(e.target.value)} placeholder="Confirm password"
              disabled={busy} />
          )}
          {error && <div className="err-box">{error}</div>}
          <button className="btn btn-primary w-full" disabled={busy}>
            {busy ? 'Please wait…' : mode === 'setup' ? 'Create Vault' : 'Unlock'}
          </button>
        </form>
      </div>
    </div>
  )
}

// ── VaultScreen ───────────────────────────────────────────────────────────────

function VaultScreen({ user, onLogout, onLock }) {
  const [categories, setCategories] = useState([])
  const [activeCat,  setActiveCat]  = useState(0)     // 0 = All
  const [entries,    setEntries]    = useState([])
  const [search,     setSearch]     = useState('')
  const [modal,      setModal]      = useState(null)   // null | {mode, entry?}
  const [toast,      setToast]      = useState('')
  const [newCatMode, setNewCatMode] = useState(false)
  const [newCatName, setNewCatName] = useState('')
  const [selectedNames, setSelectedNames] = useState([])
  const [deleteTargets, setDeleteTargets] = useState(null)
  const [transferMode, setTransferMode] = useState(null) // null | 'export' | 'import'

  useEffect(() => { loadCategories() }, [])
  useEffect(() => { if (!search.trim()) loadEntries() }, [activeCat])
  useEffect(() => { setSelectedNames([]) }, [activeCat, search])

  // debounced search
  useEffect(() => {
    if (!search.trim()) { loadEntries(); return }
    const t = setTimeout(async () => {
      try { setEntries((await SearchEntries(search)) || []) } catch {}
    }, 260)
    return () => clearTimeout(t)
  }, [search])

  async function loadCategories() {
    try { setCategories((await GetCategories()) || []) } catch {}
  }
  async function loadEntries() {
    try { setEntries((await ListEntries(activeCat)) || []) } catch {}
  }
  function flash(msg) { setToast(msg); setTimeout(() => setToast(''), 2400) }

  // ── category actions ──
  async function submitNewCat() {
    const name = newCatName.trim()
    if (!name) return
    try {
      await AddCategory(name)
      setNewCatName(''); setNewCatMode(false)
      loadCategories()
    } catch (err) { alert(String(err)) }
  }

  async function handleDeleteCat(id, e) {
    e.stopPropagation()
    if (!window.confirm("Delete this category? Its entries will move to General.")) return
    try {
      await DeleteCategory(id)
      if (activeCat === id) setActiveCat(0)
      loadCategories(); loadEntries()
      flash('Category deleted')
    } catch (err) { alert(String(err)) }
  }

  // ── entry actions ──
  async function handleSave(data, oldName) {
    try {
      if (oldName) {
        await UpdateEntry(oldName, data.categoryID, data.name, data.username, data.password, data.url, data.notes, data.totpSecret)
        flash('Entry updated')
      } else {
        await AddEntry(data.categoryID, data.name, data.username, data.password, data.url, data.notes, data.totpSecret)
        flash('Entry added')
      }
      setModal(null); loadEntries(); loadCategories()
    } catch (err) { alert(String(err)) }
  }

  function requestDelete(targets) {
    if (!targets.length) return
    setDeleteTargets(targets)
  }

  async function handleDelete(entriesToDelete, masterPassword) {
    await DeleteEntries(entriesToDelete.map(entry => entry.Name), masterPassword)
    setDeleteTargets(null); setModal(null); setSelectedNames([])
    loadEntries(); loadCategories()
    flash(entriesToDelete.length === 1 ? 'Entry deleted' : `${entriesToDelete.length} entries deleted`)
  }

  async function handleExport(exportPassword) {
    const result = await ExportVault(exportPassword)
    if (!result) return
    setTransferMode(null)
    flash(`Exported ${result.EntryCount} entries`)
  }

  async function handleImport(zipPassword) {
    const result = await ImportVault(zipPassword)
    if (!result) return
    setTransferMode(null); setSelectedNames([])
    loadEntries(); loadCategories()
    const skipped = result.SkippedEntries ? `; skipped ${result.SkippedEntries} duplicate entries` : ''
    flash(`Imported ${result.ImportedEntries} entries${skipped}`)
  }

  function toggleSelected(name) {
    setSelectedNames(names => names.includes(name)
      ? names.filter(selectedName => selectedName !== name)
      : [...names, name])
  }

  const allVisibleSelected = entries.length > 0 && entries.every(entry => selectedNames.includes(entry.Name))
  function toggleAllVisible() {
    setSelectedNames(allVisibleSelected
      ? names => names.filter(name => !entries.some(entry => entry.Name === name))
      : names => [...new Set([...names, ...entries.map(entry => entry.Name)])])
  }

  const totalCount = categories.reduce((s, c) => s + c.Count, 0)
  const activeCatName = activeCat === 0
    ? 'All Entries'
    : (categories.find(c => c.ID === activeCat)?.Name || '')

  // default category for new entries: active category or first available
  const defaultCatID = activeCat !== 0
    ? activeCat
    : (categories[0]?.ID || 1)

  return (
    <div className="layout">

      {/* ── Left sidebar: categories ── */}
      <aside className="sidebar">
        <div className="sb-top">
          <span className="brand">◈ DevHub</span>
          <div className="sidebar-actions">
            <button className="icon-btn" title="Lock vault" onClick={async () => { await Lock(); onLock() }}>⏏</button>
          </div>
        </div>

        <div className="sb-label">CATEGORIES</div>

        <nav className="cat-list">
          <button
            className={`cat-item ${activeCat === 0 ? 'active' : ''}`}
            onClick={() => { setActiveCat(0); setSearch('') }}
          >
            <span className="cat-dot all-dot" />
            <span className="cat-name">All Entries</span>
            <span className="cat-badge">{totalCount}</span>
          </button>

          {categories.map(cat => (
            <button
              key={cat.ID}
              className={`cat-item ${activeCat === cat.ID ? 'active' : ''}`}
              onClick={() => { setActiveCat(cat.ID); setSearch('') }}
            >
              <span className="cat-dot" style={{ background: catColor(cat.Name) }} />
              <span className="cat-name">{cat.Name}</span>
              <span className="cat-right">
                <span className="cat-badge">{cat.Count}</span>
                <span className="cat-del" title="Delete" onClick={e => handleDeleteCat(cat.ID, e)}>×</span>
              </span>
            </button>
          ))}
        </nav>

        {/* New category */}
        <div className="sb-footer">
          {newCatMode ? (
            <div className="new-cat-row">
              <input
                className="new-cat-input"
                placeholder="Category name…"
                value={newCatName}
                autoFocus
                onChange={e => setNewCatName(e.target.value)}
                onKeyDown={e => {
                  if (e.key === 'Enter')  submitNewCat()
                  if (e.key === 'Escape') { setNewCatMode(false); setNewCatName('') }
                }}
              />
              <button className="icon-btn sm" onClick={submitNewCat}>✓</button>
              <button className="icon-btn sm" onClick={() => { setNewCatMode(false); setNewCatName('') }}>✕</button>
            </div>
          ) : (
            <button className="new-cat-btn" onClick={() => setNewCatMode(true)}>＋ New Category</button>
          )}
          {user && <div className="signed-in-user"><span>{user.Username}</span><button onClick={onLogout}>Sign out</button></div>}
        </div>
      </aside>

      {/* ── Right panel: entries ── */}
      <div className="main-panel">

        {/* Top bar */}
        <div className="panel-bar">
          <h2 className="panel-title">{activeCatName}</h2>
          <input
            className="panel-search"
            placeholder="Search entries…"
            value={search}
            onChange={e => setSearch(e.target.value)}
          />
          <button className="btn btn-ghost" onClick={() => setTransferMode('import')}>Import</button>
          <button className="btn btn-ghost" onClick={() => setTransferMode('export')}>Export</button>
          <button className="btn btn-primary" onClick={() => setModal({ mode: 'add' })}>
            + Add Entry
          </button>
          <button
            className="btn btn-danger"
            disabled={selectedNames.length === 0}
            onClick={() => requestDelete(entries.filter(entry => selectedNames.includes(entry.Name)))}
          >
            Delete selected{selectedNames.length ? ` (${selectedNames.length})` : ''}
          </button>
        </div>

        {/* Entries */}
        {entries.length === 0 ? (
          <div className="empty-state">
            {search
              ? `No results for "${search}"`
              : 'No entries here yet. Click "+ Add Entry" to get started.'}
          </div>
        ) : (
          <div className="table-wrap">
            <table className="entry-table">
              <thead>
                <tr>
                  <th className="selection-cell">
                    <input
                      type="checkbox"
                      aria-label="Select all visible entries"
                      checked={allVisibleSelected}
                      onChange={toggleAllVisible}
                    />
                  </th>
                  <th>Name</th>
                  <th>Username</th>
                  <th>URL</th>
                  {activeCat === 0 && <th>Category</th>}
                  <th />
                </tr>
              </thead>
              <tbody>
                {entries.map(e => (
                  <tr key={e.ID} className="entry-row" onClick={() => setModal({ mode: 'view', entry: e })}>
                    <td className="selection-cell" onClick={ev => ev.stopPropagation()}>
                      <input
                        type="checkbox"
                        aria-label={`Select ${e.Name}`}
                        checked={selectedNames.includes(e.Name)}
                        onChange={() => toggleSelected(e.Name)}
                      />
                    </td>
                    <td>
                      <div className="name-cell">
                        <Avatar name={e.Name} size={30} />
                        <span className="entry-name-text">{e.Name}</span>
                      </div>
                    </td>
                    <td className="cell-muted">{e.Username || '—'}</td>
                    <td className="cell-muted cell-url">{e.URL || '—'}</td>
                    {activeCat === 0 && (
                      <td>
                        <span className="cat-pill" style={{ background: catColor(e.CategoryName) + '22', color: catColor(e.CategoryName) }}>
                          {e.CategoryName}
                        </span>
                      </td>
                    )}
                    <td className="cell-action" onClick={ev => ev.stopPropagation()}>
                      <button
                        className="copy-btn"
                        title="Copy password"
                        onClick={async () => {
                          try { await copyText(e.Password); flash('Password copied') }
                          catch { flash('Could not copy password') }
                        }}
                      >
                        Copy
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Modal */}
      {modal && (
        <EntryModal
          mode={modal.mode}
          entry={modal.entry || null}
          categories={categories}
          defaultCategoryID={defaultCatID}
          onSave={handleSave}
          onDelete={entry => requestDelete([entry])}
          onEdit={entry => setModal({ mode: 'edit', entry })}
          onClose={() => setModal(null)}
          flash={flash}
        />
      )}

      {deleteTargets && (
        <DeleteConfirmationModal
          entries={deleteTargets}
          onConfirm={handleDelete}
          onClose={() => setDeleteTargets(null)}
        />
      )}

      {transferMode && (
        <TransferModal
          mode={transferMode}
          onExport={handleExport}
          onImport={handleImport}
          onClose={() => setTransferMode(null)}
        />
      )}


      {toast && <div className="toast">{toast}</div>}
    </div>
  )
}

function TransferModal({ mode, onExport, onImport, onClose }) {
  const isExport = mode === 'export'
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e) {
    e.preventDefault()
    setError('')
    if (isExport && password.length < 8) {
      setError('Export password must contain at least 8 characters.')
      return
    }
    if (!password) {
      setError('Enter the ZIP password.')
      return
    }
    if (isExport && password !== confirmation) {
      setError('The two passwords do not match.')
      return
    }
    setBusy(true)
    try {
      isExport ? await onExport(password) : await onImport(password)
    } catch (err) {
      setError(String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="backdrop" role="dialog" aria-modal="true" aria-labelledby="transfer-title">
      <div className="modal transfer-modal">
        <div className="modal-head">
          <h3 id="transfer-title">{isExport ? 'Export encrypted ZIP' : 'Import encrypted ZIP'}</h3>
          <button className="icon-btn dark" onClick={onClose} disabled={busy}>✕</button>
        </div>
        <form className="modal-body" onSubmit={submit}>
          <p className="transfer-note">
            {isExport
              ? 'All entries are exported in an AES-256 encrypted ZIP. Keep this password safe; exported data cannot be recovered without it.'
              : 'Enter the password used for the export, then choose the ZIP file. Existing entries with the same name are kept and skipped.'}
          </p>
          <Field
            label={isExport ? 'Export ZIP password (at least 8 characters)' : 'ZIP password'}
            type="password"
            value={password}
            onChange={e => setPassword(e.target.value)}
            placeholder={isExport ? 'Set an export password' : 'Enter the export password'}
            autoFocus
            disabled={busy}
          />
          {isExport && (
            <Field
              label="Confirm export ZIP password"
              type="password"
              value={confirmation}
              onChange={e => setConfirmation(e.target.value)}
              placeholder="Enter the export password again"
              disabled={busy}
            />
          )}
          {error && <div className="err-box">{error}</div>}
          <div className="modal-foot">
            <button type="button" className="btn btn-ghost" onClick={onClose} disabled={busy}>Cancel</button>
            <button type="submit" className="btn btn-primary" disabled={busy}>
              {busy ? 'Working…' : isExport ? 'Choose save location' : 'Choose ZIP file'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

// ── Delete confirmation ──────────────────────────────────────────────────────

function DeleteConfirmationModal({ entries, onConfirm, onClose }) {
  const [masterPassword, setMasterPassword] = useState('')
  const [captchaVerified, setCaptchaVerified] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e) {
    e.preventDefault()
    setError('')
    if (!masterPassword) {
      setError('Enter the master password to continue.')
      return
    }
    if (!captchaVerified) {
      setError('Complete the claw verification first.')
      return
    }
    setBusy(true)
    try {
      await onConfirm(entries, masterPassword)
    } catch (err) {
      setError(String(err))
    } finally {
      setBusy(false)
    }
  }

  const count = entries.length
  return (
    <div className="backdrop" role="dialog" aria-modal="true" aria-labelledby="delete-confirm-title">
      <div className="modal delete-confirm-modal">
        <div className="modal-head">
          <h3 id="delete-confirm-title">Confirm deletion</h3>
          <button className="icon-btn dark" onClick={onClose} disabled={busy}>✕</button>
        </div>
        <form className="modal-body" onSubmit={submit}>
          <p className="delete-warning">
            This cannot be undone. {count === 1 ? `“${entries[0].Name}” will be deleted.` : `${count} entries will be deleted.`}
          </p>
          {count > 1 && (
            <ul className="delete-entry-list">
              {entries.slice(0, 5).map(entry => <li key={entry.ID}>{entry.Name}</li>)}
              {count > 5 && <li>and {count - 5} more entries</li>}
            </ul>
          )}
          <Field
            label="Master password"
            type="password"
            value={masterPassword}
            onChange={e => setMasterPassword(e.target.value)}
            placeholder="Enter master password"
            autoFocus
            disabled={busy}
          />
          <div className="captcha-section">
            <div className="captcha-label">Claw verification</div>
            {captchaVerified ? (
              <div className="captcha-success">✓ Verified</div>
            ) : (
              <ClawCaptcha
                title="Catch the specified toy to confirm deletion"
                assetBase={withBasePath('playcaptcha/toys/')}
                onVerify={() => { setCaptchaVerified(true); setError('') }}
              />
            )}
          </div>
          {error && <div className="err-box">{error}</div>}
          <div className="modal-foot">
            <button type="button" className="btn btn-ghost" onClick={onClose} disabled={busy}>Cancel</button>
            <button type="submit" className="btn btn-danger" disabled={busy || !captchaVerified}>
              {busy ? 'Deleting…' : `Delete${count > 1 ? ` ${count} entries` : ' entry'}`}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

// ── EntryModal ────────────────────────────────────────────────────────────────

function EntryModal({ mode, entry, categories, defaultCategoryID, onSave, onDelete, onEdit, onClose, flash }) {
  const isView = mode === 'view'
  const isEdit = mode === 'edit'

  const [form, setForm] = useState({
    categoryID:  entry?.CategoryID  || defaultCategoryID,
    name:        entry?.Name        || '',
    username:    entry?.Username    || '',
    password:    entry?.Password    || '',
    url:         entry?.URL         || '',
    notes:       entry?.Notes       || '',
    totpSecret:  entry?.TOTPSecret  || '',
  })
  const [showPw,    setShowPw]    = useState(false)
  const [showTOTP,  setShowTOTP]  = useState(false)
  const [genBusy,   setGenBusy]   = useState(false)
  const [error,     setError]     = useState('')
  const [totpCode,  setTotpCode]  = useState(null)   // { Code, SecondsLeft }
  const [history,   setHistory]   = useState(null)
  const [historyError, setHistoryError] = useState('')
  const [historyLoading, setHistoryLoading] = useState(false)
  const totpTimer = useRef(null)

  useEffect(() => {
    if (isView && entry?.TOTPSecret) {
      fetchTOTP()
      totpTimer.current = setInterval(fetchTOTP, 1000)
    }
    return () => clearInterval(totpTimer.current)
  }, [])

  async function fetchTOTP() {
    try {
      const result = await GetTOTPCode(entry.Name)
      setTotpCode(result)
    } catch {}
  }

  async function toggleHistory() {
    if (history !== null) {
      setHistory(null)
      return
    }
    setHistoryError(''); setHistoryLoading(true)
    try {
      setHistory((await GetEntryHistory(entry.Name)) || [])
    } catch (err) {
      setHistoryError(String(err))
    } finally {
      setHistoryLoading(false)
    }
  }

  function set(k)    { return e => setForm(f => ({ ...f, [k]: e.target.value })) }
  function setInt(k) { return e => setForm(f => ({ ...f, [k]: parseInt(e.target.value) || 0 })) }

  async function gen() {
    setGenBusy(true)
    try { const pw = await GeneratePassword(20); setForm(f => ({ ...f, password: pw })); setShowPw(true) }
    catch {}
    setGenBusy(false)
  }

  async function submit(e) {
    e.preventDefault(); setError('')
    if (!form.name.trim())     { setError('Name is required');     return }
    if (!form.password.trim()) { setError('Password is required'); return }
    await onSave(form, isEdit ? entry.Name : null)
  }

  function copy(text, label) {
    copyText(text)
      .then(() => flash(`${label} copied`))
      .catch(() => flash(`Could not copy ${label.toLowerCase()}`))
  }

  return (
    <div className="backdrop">
      <div className="modal">

        {/* Header */}
        <div className="modal-head">
          {isView ? (
            <div className="modal-head-left">
              <Avatar name={entry.Name} size={34} />
              <div>
                <div className="modal-entry-name">{entry.Name}</div>
                <span className="cat-pill sm" style={{ background: catColor(entry.CategoryName) + '22', color: catColor(entry.CategoryName) }}>
                  {entry.CategoryName}
                </span>
              </div>
            </div>
          ) : (
            <h3>{isEdit ? 'Edit Entry' : 'New Entry'}</h3>
          )}
          <div className="modal-head-right">
            {isView && <button className="btn btn-ghost btn-sm" onClick={() => onEdit(entry)}>Edit</button>}
            {isView && <button className="btn btn-ghost btn-sm" onClick={toggleHistory}>{history !== null ? 'Hide History' : 'History'}</button>}
            {isView && <button className="btn btn-danger btn-sm" onClick={() => onDelete(entry)}>Delete</button>}
            <button className="icon-btn dark" onClick={onClose}>✕</button>
          </div>
        </div>

        {/* View body */}
        {isView && (
          <div className="modal-body">
            {entry.Username && <ViewRow label="Username" value={entry.Username} onCopy={() => copy(entry.Username, 'Username')} />}
            <div className="view-row">
              <span className="vr-label">Password</span>
              <div className="vr-val">
                <span className="mono flex-1">{showPw ? entry.Password : '••••••••••••'}</span>
                <button className="icon-btn dark" onClick={() => setShowPw(v => !v)}>{showPw ? '🙈' : '👁'}</button>
                <button className="icon-btn dark" onClick={() => copy(entry.Password, 'Password')}>📋</button>
              </div>
            </div>
            {entry.URL && <ViewRow label="URL" value={entry.URL} onCopy={() => copy(entry.URL, 'URL')} />}
            {entry.Notes && <ViewRow label="Notes" value={entry.Notes} />}
            {entry.TOTPSecret && totpCode && (
              <div className="view-row totp-row">
                <span className="vr-label">2FA code</span>
                <div className="vr-val totp-val">
                  <span className="totp-code">{totpCode.Code.slice(0, 3)} {totpCode.Code.slice(3)}</span>
                  <div className="totp-timer">
                    <div
                      className="totp-ring"
                      style={{ '--pct': `${(totpCode.SecondsLeft / 30) * 100}%`, '--color': totpCode.SecondsLeft <= 5 ? '#ef4444' : totpCode.SecondsLeft <= 10 ? '#f59e0b' : '#10b981' }}
                    >
                      <span className="totp-secs">{totpCode.SecondsLeft}</span>
                    </div>
                  </div>
                  <button className="icon-btn dark" onClick={() => copy(totpCode.Code, '2FA code')}>📋</button>
                </div>
              </div>
            )}
            <div className="view-meta">
              <span>Created {fmt(entry.CreatedAt)}</span>
              <span>Updated {fmt(entry.UpdatedAt)}</span>
            </div>
            {(historyLoading || historyError || history !== null) && (
              <div className="history-panel">
                <div className="history-title">Version history</div>
                {historyLoading && <div className="history-empty">Loading history…</div>}
                {historyError && <div className="err-box">{historyError}</div>}
                {history && history.length === 0 && <div className="history-empty">No previous versions yet. The content before your next edit will be saved here.</div>}
                {history && history.map(snapshot => <HistorySnapshot key={snapshot.ID} snapshot={snapshot} />)}
              </div>
            )}
          </div>
        )}

        {/* Add / Edit form */}
        {!isView && (
          <form className="modal-body" onSubmit={submit}>
            <div className="field">
              <label>Category</label>
              <select value={form.categoryID} onChange={setInt('categoryID')}>
                {categories.map(c => <option key={c.ID} value={c.ID}>{c.Name}</option>)}
              </select>
            </div>
            <Field label="Name *"   value={form.name}     onChange={set('name')}     placeholder="e.g. GitHub" autoFocus />
            <Field label="Username" value={form.username} onChange={set('username')} placeholder="user@example.com" />

            <div className="field">
              <label>Password *</label>
              <div className="pw-row">
                <input type={showPw ? 'text' : 'password'} value={form.password}
                  onChange={set('password')} placeholder="Enter or generate" />
                <button type="button" className="icon-btn dark" onClick={() => setShowPw(v => !v)}>
                  {showPw ? '🙈' : '👁'}
                </button>
                <button type="button" className="btn btn-ghost btn-sm" onClick={gen} disabled={genBusy}>
                  {genBusy ? '…' : 'Generate'}
                </button>
              </div>
            </div>

            <Field label="URL"   value={form.url}   onChange={set('url')}   placeholder="https://" />
            <Field label="Notes" value={form.notes} onChange={set('notes')} textarea placeholder="Optional notes…" />

            <div className="field">
              <label>2FA secret (optional)</label>
              <div className="pw-row">
                <input
                  type={showTOTP ? 'text' : 'password'}
                  value={form.totpSecret}
                  onChange={set('totpSecret')}
                  placeholder="Enter a Base32 secret, e.g. JBSWY3DPEHPK3PXP"
                  style={{ fontFamily: form.totpSecret && showTOTP ? 'monospace' : undefined }}
                />
                <button type="button" className="icon-btn dark" onClick={() => setShowTOTP(v => !v)}>
                  {showTOTP ? '🙈' : '👁'}
                </button>
              </div>
              <div style={{ fontSize: '0.72rem', color: 'var(--text-3)', marginTop: 4 }}>
                Get this secret from the two-step verification settings of GitHub, Google, or another service.
              </div>
            </div>

            {error && <div className="err-box">{error}</div>}
            <div className="modal-foot">
              <button type="button" className="btn btn-ghost" onClick={onClose}>Cancel</button>
              <button type="submit" className="btn btn-primary">{isEdit ? 'Save Changes' : 'Add Entry'}</button>
            </div>
          </form>
        )}
      </div>
    </div>
  )
}

function HistorySnapshot({ snapshot }) {
  const [showPassword, setShowPassword] = useState(false)
  return (
    <details className="history-snapshot">
      <summary>
        <span>Before edit</span>
        <span>{fmtDateTime(snapshot.ArchivedAt)}</span>
      </summary>
      <div className="history-snapshot-body">
        <ViewRow label="Category" value={snapshot.CategoryName} />
        <ViewRow label="Name" value={snapshot.Name} />
        <ViewRow label="Username" value={snapshot.Username} />
        <div className="view-row">
          <span className="vr-label">Password</span>
          <div className="vr-val">
            <span className="mono flex-1">{showPassword ? snapshot.Password : '••••••••••••'}</span>
            <button className="icon-btn dark" onClick={() => setShowPassword(show => !show)}>{showPassword ? '🙈' : '👁'}</button>
          </div>
        </div>
        <ViewRow label="URL" value={snapshot.URL} />
        <ViewRow label="Notes" value={snapshot.Notes} />
        {snapshot.TOTPSecret && <ViewRow label="2FA secret" value={snapshot.TOTPSecret} />}
      </div>
    </details>
  )
}

function ViewRow({ label, value, onCopy }) {
  return (
    <div className="view-row">
      <span className="vr-label">{label}</span>
      <div className="vr-val">
        <span className="flex-1">{value || '—'}</span>
        {onCopy && value && <button className="icon-btn dark" onClick={onCopy}>📋</button>}
      </div>
    </div>
  )
}

// ── Shared primitives ─────────────────────────────────────────────────────────

function Field({ label, value, onChange, placeholder, type = 'text', textarea, autoFocus, disabled }) {
  return (
    <div className="field">
      <label>{label}</label>
      {textarea
        ? <textarea value={value} onChange={onChange} placeholder={placeholder} rows={3} />
        : <input type={type} value={value} onChange={onChange} placeholder={placeholder}
            autoFocus={autoFocus} disabled={disabled} />
      }
    </div>
  )
}

function Avatar({ name, size = 36 }) {
  return (
    <div className="avatar"
      style={{ width: size, height: size, fontSize: size * 0.42, background: catColor(name) }}>
      {name[0].toUpperCase()}
    </div>
  )
}

const PALETTE = ['#4f46e5','#7c3aed','#2563eb','#0891b2','#059669','#d97706','#dc2626','#db2777','#0d9488','#65a30d']
function catColor(name = '') {
  let h = 0
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0
  return PALETTE[h % PALETTE.length]
}

function fmt(iso) {
  try { return new Date(iso).toLocaleDateString() } catch { return iso }
}

function fmtDateTime(iso) {
  try { return new Date(iso).toLocaleString() } catch { return iso }
}
