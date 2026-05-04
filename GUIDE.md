# 从零到一：用 Go + Wails + React 构建桌面密码管理器

> 本文记录了完整的开发过程，涵盖 Wails 框架原理、项目架构、加密设计、数据库迁移、前端交互等所有细节。

---

## 目录

1. [技术栈概览](#1-技术栈概览)
2. [环境准备](#2-环境准备)
3. [Wails 核心概念](#3-wails-核心概念)
4. [项目结构](#4-项目结构)
5. [后端：加密模块](#5-后端加密模块)
6. [后端：数据库层](#6-后端数据库层)
7. [后端：应用层（绑定层）](#7-后端应用层绑定层)
8. [前端：React + Vite 配置](#8-前端react--vite-配置)
9. [前端：UI 组件设计](#9-前端ui-组件设计)
10. [前端：调用 Go 函数](#10-前端调用-go-函数)
11. [构建与运行](#11-构建与运行)
12. [数据库迁移策略](#12-数据库迁移策略)
13. [安全设计](#13-安全设计)
14. [完整功能清单](#14-完整功能清单)

---

## 1. 技术栈概览

| 层次 | 技术 | 作用 |
|------|------|------|
| 桌面框架 | [Wails v2](https://wails.io) | 将 Go 后端与 Web 前端打包成原生桌面应用 |
| 后端语言 | Go 1.25 | 业务逻辑、加密、数据库操作 |
| 数据库 | SQLite（`modernc.org/sqlite`，纯 Go） | 本地存储，无需 CGO |
| 加密 | AES-256-GCM + Argon2id | 密码字段加密、主密码派生密钥 |
| 前端框架 | React 18 + Vite 5 | UI 渲染 |
| 前端语言 | JSX + CSS Variables | 组件化 UI |

### 为什么选 Wails 而不是 Electron？

- **体积小**：最终产物约 15 MB，Electron 通常超过 100 MB
- **性能好**：使用系统原生 WebView（Windows 用 WebView2/Edge），无额外 Chromium 进程
- **Go 原生**：后端直接写 Go，不需要 Node.js 进程间通信

---

## 2. 环境准备

### 2.1 安装 Go

前往 [go.dev/dl](https://go.dev/dl) 下载安装，验证：

```bash
go version
# go version go1.25.1 windows/amd64
```

### 2.2 安装 Node.js

前往 [nodejs.org](https://nodejs.org) 下载安装，验证：

```bash
node --version   # v25.x.x
npm  --version   # 11.x.x
```

### 2.3 安装 C 编译器（Windows 必需）

Wails 在 Windows 上通过 `go-webview2` 调用 WebView2 COM 接口，**需要 C 编译器**。

推荐安装 **TDM-GCC**：
- 下载地址：https://jmeubank.github.io/tdm-gcc/
- 安装完成后重启终端，确认 `gcc --version` 可用

> **注意**：如果没有 C 编译器，`wails build` 会报错 `cgo: C compiler "gcc" not found`。

### 2.4 安装 Wails CLI

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

验证安装：

```bash
wails version
# Wails CLI v2.12.0
```

检查环境依赖：

```bash
wails doctor
```

---

## 3. Wails 核心概念

### 3.1 工作原理

```
┌─────────────────────────────────────────────────────────┐
│                    vault.exe（单一可执行文件）            │
│                                                         │
│  ┌──────────────────┐        ┌─────────────────────┐   │
│  │   Go 后端         │◄──────►│  WebView2 前端       │   │
│  │                  │  IPC   │  (React/HTML/CSS)   │   │
│  │  app.go          │        │  frontend/dist/     │   │
│  │  store.go        │        │  (嵌入到二进制中)    │   │
│  │  crypto.go       │        │                     │   │
│  └──────────────────┘        └─────────────────────┘   │
│                                                         │
│  前端通过 Wails 自动生成的 JS 绑定调用 Go 函数           │
└─────────────────────────────────────────────────────────┘
```

### 3.2 绑定（Binding）机制

**Go 端**：把一个结构体的方法暴露给前端：

```go
// app.go
type App struct { ... }

// 这个方法会被自动绑定到前端
func (a *App) ListEntries(categoryID int64) ([]*Entry, error) {
    return a.store.ListEntries(categoryID)
}
```

在 `main.go` 中注册：

```go
wails.Run(&options.App{
    Bind: []interface{}{
        app,   // 把 *App 注册进去
    },
})
```

**Wails 自动生成**：运行 `wails dev` 或 `wails build` 时，Wails 扫描 `Bind` 中的类型，自动生成：

```
frontend/wailsjs/go/main/App.js       ← JavaScript 调用函数
frontend/wailsjs/go/main/App.d.ts     ← TypeScript 类型定义
```

**前端调用**：

```js
import { ListEntries } from '../wailsjs/go/main/App'

// 直接调用，返回 Promise
const entries = await ListEntries(0)
```

### 3.3 前端资源嵌入

`wails build` 会先构建 React 前端（`npm run build` → `frontend/dist/`），然后通过 Go 的 `//go:embed` 指令把 `dist/` 目录打包进最终的二进制文件：

```go
//go:embed all:frontend/dist
var assets embed.FS

wails.Run(&options.App{
    AssetServer: &assetserver.Options{
        Assets: assets,   // 从内存中提供前端文件
    },
})
```

结果：**一个 `.exe` 文件包含了全部前端 + 后端代码**，复制即可运行。

---

## 4. 项目结构

```
vault/
├── main.go          # Wails 入口，嵌入前端资源
├── app.go           # 暴露给前端的所有 Go 方法（绑定层）
├── store.go         # SQLite 数据库操作层
├── crypto.go        # 加密/解密/密钥派生
├── utils.go         # 工具函数（随机密码生成）
├── wails.json       # Wails 项目配置
├── go.mod           # Go 模块依赖
├── go.sum
└── frontend/
    ├── index.html
    ├── package.json
    ├── vite.config.js
    └── src/
        ├── main.jsx      # React 入口
        ├── App.jsx       # 所有 UI 组件
        └── App.css       # 样式
```

---

## 5. 后端：加密模块

**文件**：`crypto.go`

### 5.1 主密码 → 加密密钥（Argon2id）

用户的主密码**绝不直接存储**，而是通过 **Argon2id** 派生出一个 32 字节的 AES 密钥：

```go
func deriveKey(password, salt []byte) []byte {
    return argon2.IDKey(
        password,
        salt,
        1,        // time cost（迭代次数）
        64*1024,  // memory cost（64 MB）
        4,        // parallelism（4 线程）
        32,       // 输出密钥长度
    )
}
```

**验证主密码是否正确**：初始化时加密一段已知明文 `"vault-ok"` 并存入数据库。每次解锁时重新派生密钥，尝试解密该验证串——成功则说明密码正确。

### 5.2 字段加密（AES-256-GCM）

每个密码字段单独加密，每次加密使用**随机 nonce（12 字节）**，nonce 拼接在密文前面：

```go
func encrypt(key, plaintext []byte) ([]byte, error) {
    block, _ := aes.NewCipher(key)
    gcm, _   := cipher.NewGCM(block)

    nonce := make([]byte, gcm.NonceSize())
    io.ReadFull(rand.Reader, nonce)          // 随机 nonce

    // 输出格式：[nonce][ciphertext+tag]
    return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func decrypt(key, data []byte) ([]byte, error) {
    block, _ := aes.NewCipher(key)
    gcm, _   := cipher.NewGCM(block)

    nonce     := data[:gcm.NonceSize()]
    ciphertext := data[gcm.NonceSize():]
    return gcm.Open(nil, nonce, ciphertext, nil)
}
```

加密后的字节数组以十六进制字符串存入 SQLite，方便迁移。

---

## 6. 后端：数据库层

**文件**：`store.go`

### 6.1 为什么用 `modernc.org/sqlite`？

标准的 `mattn/go-sqlite3` 需要 CGO（C 代码）。
`modernc.org/sqlite` 是**纯 Go 实现**，可在任何平台直接交叉编译，无需配置 C 工具链。

```go
import _ "modernc.org/sqlite"
db, _ := sql.Open("sqlite", "/path/to/vault.db")
```

### 6.2 数据库 Schema

```sql
-- 主密码验证信息
CREATE TABLE IF NOT EXISTS vault_config (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- 分类表
CREATE TABLE IF NOT EXISTS categories (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE
);

-- 密码条目表
CREATE TABLE IF NOT EXISTS entries (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    category_id INTEGER NOT NULL DEFAULT 1,
    name        TEXT    NOT NULL UNIQUE,
    username    TEXT    NOT NULL DEFAULT '',
    password    TEXT    NOT NULL,          -- AES-GCM 加密后的十六进制
    url         TEXT    NOT NULL DEFAULT '',
    notes       TEXT    NOT NULL DEFAULT '', -- AES-GCM 加密后的十六进制
    created_at  TEXT    NOT NULL,
    updated_at  TEXT    NOT NULL,
    FOREIGN KEY (category_id) REFERENCES categories(id)
);
```

### 6.3 数据结构

```go
type Category struct {
    ID    int64  `json:"ID"`
    Name  string `json:"Name"`
    Count int    `json:"Count"`  // 该分类下的条目数
}

type Entry struct {
    ID           int64  `json:"ID"`
    CategoryID   int64  `json:"CategoryID"`
    CategoryName string `json:"CategoryName"`
    Name         string `json:"Name"`
    Username     string `json:"Username"`
    Password     string `json:"Password"`  // 返回给前端时已解密
    URL          string `json:"URL"`
    Notes        string `json:"Notes"`     // 返回给前端时已解密
    CreatedAt    string `json:"CreatedAt"`
    UpdatedAt    string `json:"UpdatedAt"`
}
```

> **注意**：`json` tag 是 Wails 序列化必需的，字段名必须大写（Go 导出字段），Wails 会自动将其转换为 JS 对象。

### 6.4 初始化流程

```
用户第一次运行
      │
      ▼
OpenStore(path)          → 创建数据库文件，执行 schema，运行迁移
      │
      ▼
IsInitialized() = false  → 显示「创建密码库」界面
      │
  用户输入主密码
      │
      ▼
Initialize(masterPassword)
  1. 生成 32 字节随机 salt
  2. Argon2id(password, salt) → 32 字节 key
  3. AES-GCM 加密 "vault-ok" → verify
  4. 存入 vault_config: salt, verify
  5. 内存中保存 key（会话期间有效）
```

### 6.5 查询带 JOIN 的条目

为了同时返回分类名称，所有查询都 JOIN categories 表：

```go
const entrySelect = `
    SELECT e.id, e.category_id, COALESCE(c.name,'') AS cat_name,
           e.name, e.username, e.password, e.url, e.notes,
           e.created_at, e.updated_at
    FROM entries e
    LEFT JOIN categories c ON e.category_id = c.id`

// categoryID == 0 返回全部，否则按分类过滤
func (s *Store) ListEntries(categoryID int64) ([]*Entry, error) {
    if categoryID == 0 {
        rows, _ = db.Query(entrySelect + " ORDER BY e.name")
    } else {
        rows, _ = db.Query(entrySelect + " WHERE e.category_id=? ORDER BY e.name", categoryID)
    }
    // ...
}
```

---

## 7. 后端：应用层（绑定层）

**文件**：`app.go`

应用层是 Go 后端对前端暴露的唯一接口，职责是：
1. 管理 `Store` 的生命周期
2. 参数校验和错误处理
3. 组合 store 方法，实现业务逻辑

```go
type App struct {
    ctx   context.Context
    store *Store
}

// Wails 在应用启动时调用
func (a *App) startup(ctx context.Context) {
    a.ctx = ctx
    home, _ := os.UserHomeDir()
    a.store, _ = OpenStore(filepath.Join(home, ".vault.db"))
}
```

暴露的所有方法（Wails 会自动生成对应的 JS 函数）：

| Go 方法 | 说明 |
|---------|------|
| `IsInitialized() bool` | 是否已设置主密码 |
| `IsUnlocked() bool` | 当前会话是否已解锁 |
| `Initialize(pw string) error` | 首次创建密码库 |
| `Unlock(pw string) error` | 解锁密码库 |
| `Lock()` | 锁定（清除内存中的密钥） |
| `GetCategories() ([]*Category, error)` | 获取所有分类及条目数 |
| `AddCategory(name string) (int64, error)` | 新增分类 |
| `DeleteCategory(id int64) error` | 删除分类（条目移入 General） |
| `ListEntries(categoryID int64) ([]*Entry, error)` | 按分类列出条目，0=全部 |
| `SearchEntries(query string) ([]*Entry, error)` | 搜索条目 |
| `AddEntry(catID, name, user, pw, url, notes) error` | 新增条目 |
| `UpdateEntry(oldName, catID, name, ...) error` | 更新条目 |
| `DeleteEntry(name string) error` | 删除条目 |
| `GeneratePassword(length int) (string, error)` | 生成随机密码 |

---

## 8. 前端：React + Vite 配置

### 8.1 `package.json`

```json
{
  "scripts": {
    "dev":   "vite",
    "build": "vite build"
  },
  "dependencies": {
    "react":     "^18.3.1",
    "react-dom": "^18.3.1"
  },
  "devDependencies": {
    "@vitejs/plugin-react": "^4.3.1",
    "vite": "^5.4.2"
  }
}
```

### 8.2 `vite.config.js`

```js
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: { outDir: 'dist' },
})
```

### 8.3 `wails.json`（项目根目录）

```json
{
  "$schema": "https://wails.io/schemas/config.v2.json",
  "name": "vault",
  "outputfilename": "vault",
  "frontend:install": "npm install",
  "frontend:build":   "npm run build",
  "frontend:dev:watcher":   "npm run dev",
  "frontend:dev:serverUrl": "auto"
}
```

`wails build` 会按顺序执行：
1. `npm install`
2. 生成 Go → JS 绑定文件（`wailsjs/`）
3. `npm run build`（生成 `frontend/dist/`）
4. 编译 Go 代码并嵌入 `dist/`

---

## 9. 前端：UI 组件设计

### 9.1 整体状态机

```
App（根组件）
  │
  ├─ screen = 'loading' → <Splash />        （检查初始化状态）
  ├─ screen = 'setup'   → <LockScreen mode="setup" />   （首次创建）
  ├─ screen = 'locked'  → <LockScreen mode="unlock" />  （解锁）
  └─ screen = 'vault'   → <VaultScreen />   （主界面）
```

### 9.2 主界面布局

```
┌──────────────────────┬──────────────────────────────────────────┐
│  左侧：分类栏         │  右侧：条目面板                           │
│  （固定宽 230px）     │  （flex: 1，自动填充）                    │
│                      │                                          │
│  CATEGORIES          │  [标题]  [搜索框]  [+ Add Entry]         │
│  ● All      12       │  ──────────────────────────────────────  │
│  ○ Banking   2       │  NAME       USERNAME    URL    CATEGORY  │
│  ○ Email     3       │  ────────────────────────────────────── │
│  ● Social    4       │  [G] GitHub  user@...  git...  [Copy]   │
│  ○ Work      2       │  [T] Twitter @handle   twi...  [Copy]   │
│                      │                                          │
│  ＋ New Category     │                                          │
└──────────────────────┴──────────────────────────────────────────┘
```

点击条目行 → 弹出详情/编辑 Modal：

```
┌──────────────────────────────────────┐
│  [G] GitHub                  [Edit] [Delete] [✕]
│  ● Social                             │
│ ─────────────────────────────────── │
│  Username                             │
│  ┌──────────────────────────── [📋] ┐│
│  │ user@example.com               │ │
│  └────────────────────────────────┘ │
│  Password                            │
│  ┌──────────────── [👁] [📋] ───── ┐│
│  │ ••••••••••••                   │ │
│  └────────────────────────────────┘ │
│  Created 2026/5/3  Updated 2026/5/3  │
└──────────────────────────────────────┘
```

### 9.3 CSS 设计系统

使用 **CSS Variables** 统一管理颜色和间距：

```css
:root {
  --sidebar-bg: #1b1e2d;   /* 深色侧边栏 */
  --accent:     #6366f1;   /* 主色调（靛蓝） */
  --bg:         #f4f5fb;   /* 页面背景 */
  --surface:    #ffffff;   /* 卡片/表格背景 */
  --border:     #e5e7f0;   /* 边框颜色 */
  --text:       #1a1b2e;   /* 主文字 */
  --muted:      #64748b;   /* 次要文字 */
}
```

分类颜色使用哈希算法，相同名称始终显示相同颜色：

```js
const PALETTE = ['#4f46e5','#7c3aed','#2563eb','#0891b2','#059669',...]

function catColor(name = '') {
  let h = 0
  for (let i = 0; i < name.length; i++)
    h = (h * 31 + name.charCodeAt(i)) >>> 0
  return PALETTE[h % PALETTE.length]
}
```

---

## 10. 前端：调用 Go 函数

### 10.1 导入绑定

Wails 生成的绑定路径固定为 `../wailsjs/go/main/App`（`main` 是 Go 的 package 名，`App` 是结构体名）：

```js
import {
  IsInitialized, IsUnlocked, Initialize, Unlock, Lock,
  GetCategories, AddCategory, DeleteCategory,
  ListEntries, SearchEntries,
  AddEntry, UpdateEntry, DeleteEntry,
  GeneratePassword,
} from '../wailsjs/go/main/App'
```

### 10.2 调用示例

**加载分类**：

```js
async function loadCategories() {
  const cats = await GetCategories()   // 返回 [{ID, Name, Count}, ...]
  setCategories(cats || [])
}
```

**按分类加载条目**（0 = 全部）：

```js
const entries = await ListEntries(selectedCategoryID)
```

**防抖搜索**：

```js
useEffect(() => {
  if (!search.trim()) { loadEntries(); return }
  const t = setTimeout(async () => {
    const result = await SearchEntries(search)
    setEntries(result || [])
  }, 260)
  return () => clearTimeout(t)   // 组件重渲染时清除上一个 timer
}, [search])
```

**添加条目**（category 下拉 + 表单）：

```js
await AddEntry(
  form.categoryID,   // int64
  form.name,         // string
  form.username,
  form.password,
  form.url,
  form.notes,
)
```

**错误处理**：Go 返回的 `error` 会变成 Promise rejection，用 try/catch 捕获：

```js
try {
  await AddEntry(...)
} catch (err) {
  setError(String(err))   // 例如："entry already exists"
}
```

---

## 11. 构建与运行

### 11.1 开发模式（热重载）

```bash
wails dev
```

- 启动 Go 后端
- 启动 Vite 开发服务器（前端热重载）
- 自动生成/更新 `wailsjs/` 绑定文件
- 打开桌面窗口，前端修改后自动刷新

### 11.2 生产构建

```bash
wails build
```

产物位于 `build/bin/vault.exe`，无需安装任何运行时，直接双击运行。

### 11.3 构建流程详解

```
wails build
    │
    ├─ 1. 扫描 Go 绑定，生成 frontend/wailsjs/go/main/App.js
    │
    ├─ 2. cd frontend && npm install
    │
    ├─ 3. cd frontend && npm run build
    │      └─ Vite 打包 → frontend/dist/
    │
    ├─ 4. go build（嵌入 frontend/dist/）
    │      └─ //go:embed all:frontend/dist
    │
    └─ 5. 输出 build/bin/vault.exe
```

### 11.4 关于 WebView2

Windows 上 Wails 使用 **Microsoft Edge WebView2** 渲染前端。Windows 10/11 通常已预装，若未安装，首次运行时会自动提示下载（约 2 MB 运行时）。

---

## 12. 数据库迁移策略

项目使用**自动迁移**，兼容旧版本数据库：

```go
func runMigrations(db *sql.DB) error {
    // 1. 初次运行：插入默认分类
    var n int
    db.QueryRow(`SELECT COUNT(*) FROM categories`).Scan(&n)
    if n == 0 {
        for _, name := range []string{"General","Social","Banking","Email","Work"} {
            db.Exec(`INSERT OR IGNORE INTO categories (name) VALUES (?)`, name)
        }
    }

    // 2. 旧数据库升级：添加 category_id 列（如果不存在）
    db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('entries')
                 WHERE name='category_id'`).Scan(&n)
    if n == 0 {
        var genID int64 = 1
        db.QueryRow(`SELECT id FROM categories WHERE name='General'`).Scan(&genID)
        db.Exec(fmt.Sprintf(
            `ALTER TABLE entries ADD COLUMN category_id INTEGER NOT NULL DEFAULT %d`,
            genID,
        ))
    }
    return nil
}
```

**迁移规则**：
- SQLite `ALTER TABLE ADD COLUMN` 支持带 DEFAULT 的新列，已有行自动填充默认值
- 使用 `INSERT OR IGNORE` 保证幂等性（多次执行不会报错）
- 删除分类时自动将其条目移至 General，**永远不会丢失数据**

---

## 13. 安全设计

### 密码库文件（`~/.vault.db`）

| 存储内容 | 方式 |
|---------|------|
| 主密码 | **不存储**，仅用于派生密钥 |
| 随机 salt | 明文存储（用于每次派生密钥） |
| 验证串 | AES-256-GCM 加密的 `"vault-ok"` |
| 条目密码 | AES-256-GCM 加密，每条独立 nonce |
| 条目备注 | AES-256-GCM 加密 |
| 名称/用户名/URL | 明文（便于搜索） |

### 密钥生命周期

```
应用启动
    │
    ▼
OpenStore() → key = nil（锁定状态）
    │
用户输入主密码
    │
    ▼
Unlock()
  Argon2id(password, salt) → key（存于内存）
    │
    ▼
正常使用（加密/解密用 key）
    │
用户点击「锁定」或关闭窗口
    │
    ▼
key = nil（内存清除，无法再访问数据）
```

### Argon2id 参数说明

```
time:    1      → 1次迭代（平衡速度与安全）
memory:  64 MB  → 抵抗 GPU/ASIC 暴力破解
threads: 4      → 并行度
keyLen:  32     → 256位密钥
```

---

## 14. 完整功能清单

### 密码库管理
- [x] 首次运行创建密码库（设置主密码）
- [x] 主密码解锁 / 锁定
- [x] 数据库文件 `~/.vault.db` 直接复制即可迁移

### 分类管理
- [x] 默认分类：General、Social、Banking、Email、Work
- [x] 左侧分类列表，显示每类条目数
- [x] 点击分类过滤右侧条目
- [x] 新增分类（内联输入框，回车确认）
- [x] 删除分类（条目自动移至 General）

### 条目管理
- [x] 条目列表（表格式，含名称、用户名、URL、分类）
- [x] 搜索（防抖，跨名称/用户名/URL 搜索）
- [x] 新增条目（含分类下拉选择）
- [x] 查看条目（弹窗，密码可显隐）
- [x] 复制用户名 / 密码 / URL 到剪贴板
- [x] 编辑条目（含更换分类）
- [x] 删除条目（确认对话框）

### 密码生成
- [x] 随机密码生成（20 位，含大小写字母+数字+特殊字符）
- [x] 生成后直接填入表单密码框

---

*数据库文件路径：`%USERPROFILE%\.vault.db`（Windows）/ `~/.vault.db`（macOS/Linux）*
