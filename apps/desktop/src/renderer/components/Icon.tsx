import type { ReactNode } from 'react'

type IconName =
  | 'arrow-left'
  | 'arrow-right'
  | 'library'
  | 'folder'
  | 'terminal'
  | 'search'
  | 'refresh'
  | 'monitor'
  | 'chevron'
  | 'link'
  | 'user'
  | 'agent'
  | 'check'
  | 'alert'
  | 'info'

const paths: Record<IconName, ReactNode> = {
  'arrow-left': <path d="M19 12H5m6-6-6 6 6 6" />,
  'arrow-right': <path d="M5 12h14m-6-6 6 6-6 6" />,
  library: (
    <>
      <rect x="3" y="3" width="18" height="18" rx="4" />
      <path d="M9 3v18M13 8h4M13 12h4" />
    </>
  ),
  folder: <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z" />,
  terminal: (
    <>
      <rect x="3" y="4" width="18" height="16" rx="4" />
      <path d="m7 9 3 3-3 3m6 0h4" />
    </>
  ),
  search: (
    <>
      <circle cx="10.5" cy="10.5" r="6.5" />
      <path d="m16 16 4.5 4.5" />
    </>
  ),
  refresh: (
    <>
      <path d="M20 7v5h-5M4 17v-5h5" />
      <path d="M6.1 6.2A8 8 0 0 1 20 12M4 12a8 8 0 0 0 13.9 5.8" />
    </>
  ),
  monitor: (
    <>
      <rect x="3" y="4" width="18" height="13" rx="2" />
      <path d="M8 21h8m-4-4v4" />
    </>
  ),
  chevron: <path d="m9 5 7 7-7 7" />,
  link: (
    <>
      <path
        d="m10 13 4-4m-6 8-1 1a4 4 0 0 1-6-6l4-4a4 4 0 0 1 6 0m2-1 1-1a4 4 0 0 1 6 6l-4 4a4 4 0 0 1-6 0"
        transform="translate(1 0)"
      />
    </>
  ),
  user: (
    <>
      <circle cx="12" cy="8" r="3.5" />
      <path d="M5 21v-2a7 7 0 0 1 14 0v2" />
    </>
  ),
  agent: (
    <>
      <rect x="4" y="6" width="16" height="14" rx="5" />
      <path d="M12 3v3m-4 6h.01M16 12h.01M9 16h6" />
    </>
  ),
  check: <path d="m5 12 4 4L19 6" />,
  alert: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v6m0 4h.01" />
    </>
  ),
  info: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 11v6m0-10h.01" />
    </>
  )
}

export function Icon({ name, className = '' }: { name: IconName; className?: string }) {
  return (
    <svg
      className={`icon ${className}`}
      width="18"
      height="18"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.65"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {paths[name]}
    </svg>
  )
}
