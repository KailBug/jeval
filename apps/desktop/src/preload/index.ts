import { contextBridge, ipcRenderer } from 'electron'
import type { DesktopAPI } from '../../../../contracts/index'

const api: DesktopAPI = {
  platform: process.platform,
  exportComparison: (query) => ipcRenderer.invoke('jeval:export-comparison', query),
  listSnapshots: (runId, offset = 0) =>
    ipcRenderer.invoke('jeval:runs.snapshots', { runId, offset, limit: 20 }),
  getSnapshot: (runId, snapshotId) =>
    ipcRenderer.invoke('jeval:runs.snapshot', { runId, snapshotId }),
  getAnnotation: (target) => ipcRenderer.invoke('jeval:annotations.get', target),
  listAnnotations: (runId, snapshotId, offset = 0) =>
    ipcRenderer.invoke('jeval:annotations.list', { runId, snapshotId, offset, limit: 20 }),
  saveAnnotation: (input) => ipcRenderer.invoke('jeval:annotations.save', input),
  deleteAnnotation: (target) => ipcRenderer.invoke('jeval:annotations.delete', target),
  importCodex: () => ipcRenderer.invoke('jeval:import-codex'),
  importRecord: () => ipcRenderer.invoke('jeval:import-record'),
  exportRecord: (runId, format) => ipcRenderer.invoke('jeval:export-record', { runId, format }),
  updateCodex: (runId) => ipcRenderer.invoke('jeval:update-codex', runId),
  updateStatus: (id) => ipcRenderer.invoke('jeval:update-status', id),
  cancelUpdate: (id) => ipcRenderer.invoke('jeval:update-cancel', id),
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
  getEventContent: (query) => ipcRenderer.invoke('jeval:event-content', query),
  listEvents: (query) => ipcRenderer.invoke('jeval:events', query),
  restartEngine: () => ipcRenderer.invoke('jeval:restart')
}
contextBridge.exposeInMainWorld('jeval', api)
