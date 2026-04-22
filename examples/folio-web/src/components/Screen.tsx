import type { ReactNode } from 'react'
import { useEffect, useState } from 'react'
import { formatClock } from '../format'

export function StatusBar() {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 30_000)
    return () => window.clearInterval(id)
  }, [])
  return (
    <div className="statusbar" aria-hidden="true">
      <span>{formatClock(now)}</span>
      <span className="dots" aria-hidden="true">
        <span className="dot" />
        <span className="dot" />
        <span className="dot" />
      </span>
    </div>
  )
}

export function Screen(props: {
  header?: ReactNode
  footer?: ReactNode
  children: ReactNode
}) {
  return (
    <div className="screen">
      {props.header}
      <div className="screen-body">{props.children}</div>
      {props.footer ? (
        <div className="screen-footer">{props.footer}</div>
      ) : null}
    </div>
  )
}

export function Header(props: {
  title: string
  subtitle?: string
  left?: ReactNode
  right?: ReactNode
}) {
  return (
    <div className="screen-header">
      {props.left}
      <div style={{ flex: '1 1 auto', minWidth: 0 }}>
        <h1 className="title" title={props.title}>
          {props.title}
        </h1>
        {props.subtitle ? (
          <p className="subtitle">{props.subtitle}</p>
        ) : null}
      </div>
      {props.right}
    </div>
  )
}

export function BackButton(props: { onClick: () => void; label?: string }) {
  return (
    <button
      id="back"
      type="button"
      className="icon-btn"
      onClick={props.onClick}
      aria-label={props.label ?? 'Back'}
    >
      <svg
        width="16"
        height="16"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2.4"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M15 18l-6-6 6-6" />
      </svg>
    </button>
  )
}
