import { app, BrowserWindow, dialog, ipcMain } from 'electron'
import { isAbsolute, join, resolve } from 'node:path'
import { mkdirSync } from 'node:fs'
import { pathToFileURL } from 'node:url'
import { EngineClient } from './engine-client'
import type { CodexDirectory, Hello, Run, ScanStatus } from '../../../../contracts/index'

let engine: EngineClient
let ready: Promise<Hello>
let restarting: Promise<Hello> | undefined
let quitting = false
let window: BrowserWindow | undefined
let importing = false
let scanID: string | undefined
const rendererFile = join(__dirname, '../renderer/index.html')
const developmentURL = !app.isPackaged ? process.env.ELECTRON_RENDERER_URL : undefined
// Electron's Chromium switch doesn't consistently change app.getPath('userData').
// Use the same explicit data directory for both Chromium and the persisted library.
const userDataArgument = process.argv.find((argument) => argument.startsWith('--user-data-dir='))
if (userDataArgument)
  app.setPath('userData', resolve(userDataArgument.slice('--user-data-dir='.length)))
const primaryInstance = app.requestSingleInstanceLock()
if (!primaryInstance) app.quit()
else
  app.on('second-instance', () => {
    if (window?.isMinimized()) window.restore()
    window?.show()
    window?.focus()
  })

function startEngine(): Promise<Hello> {
  scanID = undefined
  const executable = process.platform === 'win32' ? 'jeval-engine.exe' : 'jeval-engine'
  const enginePath = app.isPackaged
    ? join(process.resourcesPath, 'engine', executable)
    : join(app.getAppPath(), '../../bin', executable)
  const dataDirectory = app.getPath('userData')
  engine = new EngineClient(enginePath, 5000, ['--database', join(dataDirectory, 'library.sqlite')])
  ready = Promise.resolve().then(() => {
    mkdirSync(dataDirectory, { recursive: true })
    return engine.start()
  })
  void ready.catch(() => undefined) // Retain the rejection for UI recovery without an unhandled rejection.
  return ready
}

function registerIPC(): void {
  const requireID = (id: unknown): string => {
    if (typeof id !== 'string' || id.length === 0 || id.length > 128 || isAbsolute(id))
      throw new Error('无效的记录或目录 ID')
    return id
  }
  const savedDirectory = async (id: unknown) => {
    const directoryID = requireID(id)
    await ready
    const directories = await engine.request<{ items: CodexDirectory[] }>('codex.directories.list')
    if (!directories.items.some((directory) => directory.id === directoryID))
      throw new Error('保存的目录不存在')
    return directoryID
  }
  const scanRequest = async (method: string, id: unknown) => {
    if (typeof id !== 'string' || id !== scanID) throw new Error('扫描不存在或引擎已重启')
    await ready
    return engine.request<ScanStatus>(method, { id })
  }
  const ensureScanIdle = async () => {
    if (!scanID) return
    const status = await scanRequest('codex.scan.status', scanID)
    if (status.state === 'running' || status.state === 'cancelling')
      throw new Error('请等待当前扫描完成或取消扫描')
  }
  const handlers: Record<string, (params: unknown) => Promise<unknown>> = {
    'jeval:codex-directories': async () => {
      await ready
      return engine.request('codex.directories.list')
    },
    'jeval:remove-codex-directory': async (id) => {
      if (importing || restarting) throw new Error('请等待当前操作完成')
      await ensureScanIdle()
      return engine.request('codex.directories.remove', { id: await savedDirectory(id) })
    },
    'jeval:scan-codex': async (directoryId) => {
      if (!window || importing || restarting) throw new Error('请等待当前操作完成')
      importing = true
      try {
        await ready
        await ensureScanIdle()
        let params: { path: string } | { directoryId: string }
        if (directoryId !== undefined) params = { directoryId: await savedDirectory(directoryId) }
        else {
          const selection = await dialog.showOpenDialog(window, {
            title: '选择 Codex 记录目录',
            properties: ['openDirectory']
          })
          if (selection.canceled || !selection.filePaths[0]) return null
          params = { path: selection.filePaths[0] }
        }
        const status = await engine.request<ScanStatus>('codex.scan.start', params)
        scanID = status.id
        return status
      } finally {
        importing = false
      }
    },
    'jeval:scan-status': (id) => scanRequest('codex.scan.status', id),
    'jeval:scan-cancel': (id) => scanRequest('codex.scan.cancel', id),
    'jeval:scan-candidates': async (params) => {
      const query = params as { id?: unknown; offset?: unknown } | null
      if (
        !query ||
        query.id !== scanID ||
        typeof query.id !== 'string' ||
        !Number.isInteger(query.offset) ||
        (query.offset as number) < 0
      )
        throw new Error('无效的候选查询')
      await ready
      return engine.request('codex.scan.candidates', {
        id: query.id,
        offset: query.offset,
        limit: 50
      })
    },
    'jeval:scan-import': async (params) => {
      const selection = params as { id?: unknown; ids?: unknown } | null
      if (
        !selection ||
        typeof selection.id !== 'string' ||
        selection.id !== scanID ||
        !Array.isArray(selection.ids) ||
        selection.ids.length === 0 ||
        selection.ids.length > 200 ||
        !selection.ids.every((id) => typeof id === 'string' && id.length <= 128)
      )
        throw new Error('无效的候选选择')
      if (importing || restarting) throw new Error('请等待当前操作完成')
      importing = true
      try {
        await ready
        return await engine.request('codex.scan.import', { id: selection.id, ids: selection.ids })
      } finally {
        importing = false
      }
    },
    'jeval:import-codex': async () => {
      if (!window || importing || restarting) throw new Error('请等待当前操作完成')
      importing = true
      try {
        await ready
        await ensureScanIdle()
        const selection = await dialog.showOpenDialog(window, {
          title: '导入 Codex 记录',
          properties: ['openFile'],
          filters: [{ name: 'Codex rollout', extensions: ['jsonl'] }]
        })
        if (selection.canceled || !selection.filePaths[0]) return null
        return await engine.request('codex.import', { path: selection.filePaths[0] }, 30000)
      } finally {
        importing = false
      }
    },
    'jeval:update-codex': async (runId) => {
      const id = requireID(runId)
      if (importing || restarting) throw new Error('请等待当前操作完成')
      importing = true
      try {
        await ready
        await ensureScanIdle()
        const run = await engine.request<Run>('runs.get', { runId: id })
        if (run.demo || !run.importInfo) throw new Error('只能更新已登记的 Codex 记录')
        return await engine.request('codex.update', { runId: id }, 30000)
      } finally {
        importing = false
      }
    },
    'jeval:hello': async () => {
      await ready
      return engine.request('hello')
    },
    'jeval:runs': async (params) => {
      await ready
      return engine.request('runs.list', params)
    },
    'jeval:run': async (runId) => {
      const id = requireID(runId)
      await ready
      return engine.request('runs.get', { runId: id })
    },
    'jeval:events': async (params) => {
      await ready
      return engine.request('runs.events', params)
    },
    'jeval:restart': async () => {
      if (importing) throw new Error('请等待导入完成')
      if (!restarting)
        restarting = (async () => {
          await engine.stop()
          return startEngine()
        })().finally(() => {
          restarting = undefined
        })
      return restarting
    }
  }
  for (const [channel, handler] of Object.entries(handlers)) {
    ipcMain.handle(channel, (event, params: unknown) => {
      const expected = developmentURL
        ? new URL(developmentURL).href
        : pathToFileURL(rendererFile).href
      if (
        !window ||
        event.sender !== window.webContents ||
        event.senderFrame !== event.sender.mainFrame ||
        event.senderFrame.url !== expected
      )
        throw new Error('不允许的调用来源')
      return handler(params)
    })
  }
}

function createWindow(): void {
  const icon = app.isPackaged
    ? join(process.resourcesPath, 'branding/jeval.png')
    : join(app.getAppPath(), 'resources/jeval.png')
  window = new BrowserWindow({
    icon,
    width: 1440,
    height: 940,
    minWidth: 980,
    minHeight: 680,
    backgroundColor: '#f5f5f3',
    title: 'jeval · Agent 工作台',
    titleBarStyle: 'hidden',
    ...(process.platform === 'darwin'
      ? { trafficLightPosition: { x: 12, y: 13 } }
      : { titleBarOverlay: { color: '#f5f5f3', symbolColor: '#626560', height: 40 } }),
    autoHideMenuBar: true,
    webPreferences: {
      preload: join(__dirname, '../preload/index.js'),
      contextIsolation: true,
      sandbox: true,
      nodeIntegration: false
    }
  })
  window.webContents.setWindowOpenHandler(() => ({ action: 'deny' }))
  window.webContents.on('will-navigate', (event) => event.preventDefault())
  window.webContents.session.setPermissionRequestHandler((_webContents, _permission, callback) =>
    callback(false)
  )
  if (developmentURL) void window.loadURL(developmentURL)
  else void window.loadFile(rendererFile)
}

void app.whenReady().then(() => {
  if (!primaryInstance) return
  app.setAppUserModelId('dev.jeval.desktop')
  void startEngine().catch(() => undefined)
  registerIPC()
  createWindow()
  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow()
  })
})
app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit()
})
app.on('before-quit', (event) => {
  if (quitting || !engine) return
  event.preventDefault()
  quitting = true
  void engine.stop().finally(() => app.quit())
})
