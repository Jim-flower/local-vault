import { useState, useEffect } from 'react'
import {
  IsInitialized, IsUnlocked, Initialize, Unlock, Lock,
  GetCategories, AddCategory, DeleteCategory,
  ListEntries, SearchEntries, AddEntry, UpdateEntry, DeleteEntry,
  GeneratePassword,
} from '../wailsjs/go/main/App'

// ── Root ─────────────────────────────────────────────────────────────────────

export default function App() {
  const [screen, setScreen] = useState('loading')

  useEffect(() => { checkAuth() }, [])

  async function checkAuth() {
    try {
      const initialized = await IsInitialized()
      if (!initialized) { setScreen('setup'); return }
      setScreen((await IsUnlocked()) ? 'vault' : 'locked')
    } catch { setScreen('locked') }
  }

  if (screen === 'loading') return <Splash />
  if (screen === 'setup')   return <LockScreen mode="setup"  onDone={() => setScreen('vault')} />
  if (screen === 'locked')  return <LockScreen mode="unlock" onDone={() => setScreen('vault')} />
  return <VaultScreen onLock={() => setScreen('locked')} />
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

function VaultScreen({ onLock }) {
  const [categories, setCategories] = useState([])
  const [activeCat,  setActiveCat]  = useState(0)     // 0 = All
  const [entries,    setEntries]    = useState([])
  const [search,     setSearch]     = useState('')
  const [modal,      setModal]      = useState(null)   // null | {mode, entry?}
  const [toast,      setToast]      = useState('')
  const [newCatMode, setNewCatMode] = useState(false)
  const [newCatName, setNewCatName] = useState('')

  useEffect(() => { loadCategories() }, [])
  useEffect(() => { if (!search.trim()) loadEntries() }, [activeCat])

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
        await UpdateEntry(oldName, data.categoryID, data.name, data.username, data.password, data.url, data.notes)
        flash('Entry updated')
      } else {
        await AddEntry(data.categoryID, data.name, data.username, data.password, data.url, data.notes)
        flash('Entry added')
      }
      setModal(null); loadEntries(); loadCategories()
    } catch (err) { alert(String(err)) }
  }

  async function handleDelete(entry) {
    if (!window.confirm(`Delete "${entry.Name}"?`)) return
    try {
      await DeleteEntry(entry.Name)
      setModal(null); loadEntries(); loadCategories()
      flash('Entry deleted')
    } catch (err) { alert(String(err)) }
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
          <span className="brand">🔐 Vault</span>
          <button className="icon-btn" title="Lock vault" onClick={async () => { await Lock(); onLock() }}>⏏</button>
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
          <button className="btn btn-primary" onClick={() => setModal({ mode: 'add' })}>
            + Add Entry
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
                        onClick={() => { navigator.clipboard.writeText(e.Password); flash('Password copied') }}
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
          onDelete={handleDelete}
          onEdit={entry => setModal({ mode: 'edit', entry })}
          onClose={() => setModal(null)}
          flash={flash}
        />
      )}

      {toast && <div className="toast">{toast}</div>}
    </div>
  )
}

// ── EntryModal ────────────────────────────────────────────────────────────────

function EntryModal({ mode, entry, categories, defaultCategoryID, onSave, onDelete, onEdit, onClose, flash }) {
  const isView = mode === 'view'
  const isEdit = mode === 'edit'

  const [form, setForm] = useState({
    categoryID: entry?.CategoryID || defaultCategoryID,
    name:       entry?.Name       || '',
    username:   entry?.Username   || '',
    password:   entry?.Password   || '',
    url:        entry?.URL        || '',
    notes:      entry?.Notes      || '',
  })
  const [showPw,  setShowPw]  = useState(false)
  const [genBusy, setGenBusy] = useState(false)
  const [error,   setError]   = useState('')

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
    navigator.clipboard.writeText(text).then(() => flash(`${label} copied`))
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
            {entry.URL   && <ViewRow label="URL"   value={entry.URL}   onCopy={() => copy(entry.URL, 'URL')} />}
            {entry.Notes && <ViewRow label="Notes" value={entry.Notes} />}
            <div className="view-meta">
              <span>Created {fmt(entry.CreatedAt)}</span>
              <span>Updated {fmt(entry.UpdatedAt)}</span>
            </div>
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
