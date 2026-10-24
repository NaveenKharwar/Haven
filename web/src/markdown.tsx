import type { ReactNode } from 'react'

// A small, safe Markdown renderer for release notes. It builds React elements
// (never raw HTML), supports headings, bullet lists, **bold**, `code` and
// links, and only turns http(s) addresses into links.

const INLINE = /(\*\*[^*\n]+\*\*|`[^`\n]+`|\[[^\]\n]+\]\(https?:\/\/[^)\s]+\)|https?:\/\/[^\s<>)\]]+)/g

function inline(text: string): ReactNode[] {
  const out: ReactNode[] = []
  let last = 0
  let m: RegExpExecArray | null
  INLINE.lastIndex = 0
  while ((m = INLINE.exec(text))) {
    if (m.index > last) out.push(text.slice(last, m.index))
    const t = m[0]
    const key = `${m.index}`
    if (t.startsWith('**')) {
      out.push(<strong key={key}>{t.slice(2, -2)}</strong>)
    } else if (t.startsWith('`')) {
      out.push(
        <code key={key} className="rounded-[2px] bg-well px-1 font-mono text-[0.85em]">
          {t.slice(1, -1)}
        </code>,
      )
    } else if (t.startsWith('[')) {
      const [, label, url] = /^\[([^\]]+)\]\((.+)\)$/.exec(t)!
      out.push(
        <a key={key} href={url} target="_blank" rel="noreferrer noopener" className="text-accent underline underline-offset-2">
          {label}
        </a>,
      )
    } else {
      out.push(
        <a key={key} href={t} target="_blank" rel="noreferrer noopener" className="break-all text-accent underline underline-offset-2">
          {t}
        </a>,
      )
    }
    last = m.index + t.length
  }
  if (last < text.length) out.push(text.slice(last))
  return out
}

export function Markdown({ text, className }: { text: string; className?: string }) {
  const blocks: ReactNode[] = []
  let list: string[] = []
  let para: string[] = []
  const flushList = () => {
    if (list.length) {
      blocks.push(
        <ul key={`u${blocks.length}`} className="list-disc space-y-1 pl-5">
          {list.map((li, i) => (
            <li key={i}>{inline(li)}</li>
          ))}
        </ul>,
      )
      list = []
    }
  }
  const flushPara = () => {
    if (para.length) {
      blocks.push(<p key={`p${blocks.length}`}>{inline(para.join(' '))}</p>)
      para = []
    }
  }
  for (const raw of text.split('\n')) {
    const line = raw.trimEnd()
    const bullet = /^\s*[-*+]\s+(.*)$/.exec(line)
    const heading = /^#{1,6}\s+(.*)$/.exec(line)
    if (bullet) {
      flushPara()
      list.push(bullet[1])
    } else if (heading) {
      flushList()
      flushPara()
      blocks.push(
        <p key={`h${blocks.length}`} className="font-semibold">
          {inline(heading[1])}
        </p>,
      )
    } else if (line.trim() === '') {
      flushList()
      flushPara()
    } else {
      flushList()
      para.push(line.trim())
    }
  }
  flushList()
  flushPara()
  return <div className={className ?? 'space-y-2'}>{blocks}</div>
}
