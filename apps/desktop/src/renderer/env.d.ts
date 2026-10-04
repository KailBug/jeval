import type { DesktopAPI } from '../../../../contracts/index'
declare global {
  interface Window {
    jeval: DesktopAPI
  }
}
