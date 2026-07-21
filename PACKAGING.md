# Vault Windows 打包指南

本项目基于 Wails v2。开发构建可生成一个可直接运行的 `vault.exe`；发布给普通用户时，建议额外生成带卸载入口和开始菜单快捷方式的 NSIS 安装程序。

## 1. 一次性准备

在项目根目录使用 PowerShell 7 执行以下检查：

```powershell
go version
node --version
npm --version
wails version
wails doctor
```

如果 `wails` 命令不可用，安装 Wails CLI：

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

把 Go 的 bin 目录加入 `Path` 后重新打开终端。

### 安装 NSIS（生成安装程序时必需）

```powershell
winget install --id NSIS.NSIS -e --source winget --silent
```

安装完成后关闭并重新打开 PowerShell，然后确认：

```powershell
makensis /VERSION
```

若仍然找不到 `makensis`，请把其所在目录（通常为 `C:\Program Files (x86)\NSIS`）加入用户或系统的 `Path`，随后重开终端。

## 2. 先运行测试

```powershell
go test ./...
```

这会验证包括加密 ZIP 导入/导出和删除保护在内的后端关键逻辑。

## 3. 生成可执行文件

```powershell
wails build
```

产物：`build\bin\vault.exe`。

该文件已嵌入 React 前端和 Go 后端；在已具备 WebView2 Runtime 的 Windows 10/11 电脑上可直接运行。

## 4. 生成安装程序（推荐发布方式）

```powershell
wails build -clean -nsis
```

安装程序会生成在 `build\bin` 下。文件名可能随 Wails 版本与项目配置略有不同，请以本次构建输出为准。

`-clean` 会清理上一次的构建文件，避免发布目录混入旧版本产物。不要在其中存放需要保留的文件。

## 5. 发布前检查

在一台测试机或 Windows 沙盒中执行：

1. 运行生成的安装程序。
2. 启动 Vault，创建测试密码库，新增一个条目。
3. 测试加密 ZIP 的导出和导入；确认错误 ZIP 密码会被拒绝。
4. 测试单条与批量删除均要求主密码和抓娃娃验证。
5. 通过“应用和功能”卸载，并确认程序可正常移除。

> 密码库数据库默认位于 `%USERPROFILE%\.vault.db`。卸载应用通常不会删除此数据库；若要彻底移除个人数据，请在确认不再需要后手动删除该文件及自己保存的导出 ZIP。

## 6. 可选：减小体积与签名

- 已安装 UPX 时可使用 `wails build -clean -nsis -upx` 压缩最终二进制；发布前务必完整测试，且部分杀毒软件可能对 UPX 压缩文件更敏感。
- 面向外部用户发布时，建议使用代码签名证书签署安装程序和 `vault.exe`，可减少 Windows SmartScreen 警告。
- 每次发布前更新 `wails.json` 中的产品名称、作者和版本相关信息，并保留对应的 Git tag 或发布记录。
