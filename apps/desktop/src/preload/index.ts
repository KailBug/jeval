import { contextBridge, ipcRenderer } from 'electron'
import type { DesktopAPI } from '../../../../contracts/index'

const api: DesktopAPI = {
  platform: process.platform,
  importCodex: () => ipcRenderer.invoke('jeval:import-codex'),
  updateCodex: (runId) => ipcRenderer.invoke('jeval:update-codex', runId),
  scanCodex: (directoryId) => ipcRenderer.invoke('jeval:scan-codex', directoryId),
  listCodexDirectories: () => ipcRenderer.invoke('jeval:codex-directories'),
  removeCodexDirectory: (id) => ipcRenderer.invoke('jeval:remove-codex-directory', id),
  scanStatus: (id) => ipcRenderer.invoke('jeval:scan-status', id),
  cancelScan: (id) => ipcRenderer.invoke('jeval:scan-cancel', id),
  scanCandidates: (id, offset = 0) => ipcRenderer.invoke('jeval:scan-candidates', { id, offset }),
  importScanSelection: (id, ids) => ipcRenderer.invoke('jeval:scan-import', { id, ids }),
  hello: () => ipcRenderer.invoke('jeval:hello'),
  listRuns: (query) => ipcRenderer.invoke('jeval:runs', query),
  getRun: (runId) => ipcRenderer.invoke('jeval:run', runId),
  listEvents: (query) => ipcRenderer.invoke('jeval:events', query),
  restartEngine: () => ipcRenderer.invoke('jeval:restart')
}
contextBridge.exposeInMainWorld('jeval', api)
