import { app, BrowserWindow, dialog, ipcMain } from 'electron'
import { join } from 'node:path'
import { pathToFileURL } from 'node:url'
import { EngineClient } from './engine-client'
import type { Hello } from '../../../../contracts/index'

let engine: EngineClient
let ready: Promise<Hello>
let restarting: Promise<Hello> | undefined
let quitting = false
let window: BrowserWindow | undefined
let importing = false
const rendererFile = join(__dirname, '../renderer/index.html')
const developmentURL = !app.isPackaged ? process.env.ELECTRON_RENDERER_URL : undefined

function startEngine(): Promise<Hello> {
  const executable = process.platform === 'win32' ? 'jeval-engine.exe' : 'jeval-engine'
  const enginePath = app.isPackaged
    ? join(process.resourcesPath, 'engine', executable)
    : join(app.getAppPath(), '../../bin', executable)
  engine = new EngineClient(enginePath)
  ready = engine.start()
  void ready.catch(() => undefined) // Retain the rejection for UI recovery without an unhandled rejection.
  return ready
}

function registerIPC(): void {
  const handlers: Record<string, (params: unknown) => Promise<unknown>> = {
    'jeval:import-codex': async () => {
      if (!window || importing || restarting) throw new Error('请等待当前操作完成')
      importing = true
      try {
        await ready
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
    'jeval:hello': async () => {
      await ready
      return engine.request('hello')
    },
    'jeval:runs': async (params) => {
      await ready
      return engine.request('runs.list', params)
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
