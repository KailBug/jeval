import { contextBridge, ipcRenderer } from 'electron'
import type { DesktopAPI } from '../../../../contracts/index'

const api: DesktopAPI = {
  platform: process.platform,
  importCodex: () => ipcRenderer.invoke('jeval:import-codex'),
  scanCodex: () => ipcRenderer.invoke('jeval:scan-codex'),
  scanStatus: (id) => ipcRenderer.invoke('jeval:scan-status', id),
  cancelScan: (id) => ipcRenderer.invoke('jeval:scan-cancel', id),
  scanCandidates: (id, offset = 0) => ipcRenderer.invoke('jeval:scan-candidates', { id, offset }),
  importScanSelection: (id, ids) => ipcRenderer.invoke('jeval:scan-import', { id, ids }),
  hello: () => ipcRenderer.invoke('jeval:hello'),
  listRuns: (query) => ipcRenderer.invoke('jeval:runs', query),
  listEvents: (query) => ipcRenderer.invoke('jeval:events', query),
  restartEngine: () => ipcRenderer.invoke('jeval:restart')
}
contextBridge.exposeInMainWorld('jeval', api)
