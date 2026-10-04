import { contextBridge, ipcRenderer } from 'electron'
import type { DesktopAPI } from '../../../../contracts/index'

const api: DesktopAPI = {
  platform: process.platform,
  importCodex: () => ipcRenderer.invoke('jeval:import-codex'),
  hello: () => ipcRenderer.invoke('jeval:hello'),
  listRuns: (query) => ipcRenderer.invoke('jeval:runs', query),
  listEvents: (query) => ipcRenderer.invoke('jeval:events', query),
  restartEngine: () => ipcRenderer.invoke('jeval:restart')
}
contextBridge.exposeInMainWorld('jeval', api)
