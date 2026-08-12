import type { ReactNode } from 'react'

const LINK = /\[([^\]]+)\]\(([^\s)]+)(?:\s+"[^"]*")?\)/g

function safeLink(value: string) {
  try {
    const url = new URL(value)
    return url.protocol === 'https:' || url.protocol === 'http:' ? url.href : undefined
  } catch {
    return undefined
  }
}

function inline(text: string): ReactNode[] {
  const nodes: ReactNode[] = []
  let cursor = 0
  for (const match of text.matchAll(LINK)) {
    const index = match.index ?? 0
    nodes.push(text.slice(cursor, index))
    const href = safeLink(match[2])
    nodes.push(
      href ? (
        <a
          className="underline decoration-border underline-offset-4 hover:decoration-foreground"
          href={href}
          key={`${href}-${index}`}
          rel="nofollow noopener noreferrer"
          target="_blank"
        >
          {match[1]}
          <span className="sr-only"> (opens in a new tab)</span>
        </a>
      ) : (
        match[1]
      ),
    )
    cursor = index + match[0].length
  }
  nodes.push(text.slice(cursor))
  return nodes
}

export function SafeMarkdown({ source }: { source: string }) {
  const blocks: ReactNode[] = []
  const lines = source.replace(/\r\n?/g, '\n').split('\n')
  let inCode = false
  let code: string[] = []
  let list: ReactNode[] = []

  function flushList(key: number | string) {
    if (list.length === 0) return
    blocks.push(
      <ul className="my-4 ml-5 list-disc space-y-1 pl-1" key={`list-${key}`}>
        {list}
      </ul>,
    )
    list = []
  }

  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index]
    if (line.startsWith('```')) {
      flushList(index)
      if (inCode) {
        blocks.push(
          <pre className="overflow-x-auto border-y bg-muted/40 p-4 text-sm" key={`code-${index}`}>
            <code>{code.join('\n')}</code>
          </pre>,
        )
        code = []
      }
      inCode = !inCode
      continue
    }
    if (inCode) {
      code.push(line)
      continue
    }
    const heading = /^(#{1,3})\s+(.+)$/.exec(line)
    if (heading) {
      flushList(index)
      const level = heading[1].length
      const className = 'mt-7 scroll-mt-4 font-serif font-semibold tracking-tight'
      blocks.push(
        level === 1 ? (
          <h2 className={`${className} text-2xl`} key={index}>
            {inline(heading[2])}
          </h2>
        ) : level === 2 ? (
          <h3 className={`${className} text-xl`} key={index}>
            {inline(heading[2])}
          </h3>
        ) : (
          <h4 className={`${className} text-lg`} key={index}>
            {inline(heading[2])}
          </h4>
        ),
      )
      continue
    }
    if (/^[-*+]\s+/.test(line)) {
      list.push(<li key={index}>{inline(line.replace(/^[-*+]\s+/, ''))}</li>)
      continue
    }
    flushList(index)
    if (line.trim()) {
      blocks.push(
        <p className="my-4 leading-7" key={index}>
          {inline(line)}
        </p>,
      )
    }
  }
  flushList('final')
  if (code.length > 0) {
    blocks.push(
      <pre className="overflow-x-auto border-y bg-muted/40 p-4 text-sm" key="code-final">
        <code>{code.join('\n')}</code>
      </pre>,
    )
  }
  return <article className="min-w-0 break-words px-5 pb-6 sm:px-7">{blocks}</article>
}
