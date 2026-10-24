import { useEffect, useState } from 'react'
import { Coffee, Heart, Star } from 'lucide-react'
import { Sections, cx } from './components'
import { useHaven } from './context'

const ext = 'text-accent underline underline-offset-2'

/** The GitHub address of this Haven's repository, or null when the build doesn't know it. */
export function repoUrl(repo?: string, path = ''): string | null {
  return repo && /^[\w.-]+\/[\w.-]+$/.test(repo) ? `https://github.com/${repo}${path}` : null
}

export function StarLink({ className }: { className?: string }) {
  const { repo } = useHaven()
  const url = repoUrl(repo)
  if (!url) return null
  return (
    <a href={url} target="_blank" rel="noreferrer noopener" className={cx('inline-flex items-center gap-1.5 hover:text-ink', className)}>
      <Star size={12} /> Star on GitHub
    </a>
  )
}

/** "Made with love, coffee and India by <name>", with the name linking to the author's profile. */
export function MadeBy({ className }: { className?: string }) {
  const { author, authorUrl } = useHaven()
  const safe = authorUrl && /^https:\/\/github\.com\/[\w.-]+$/.test(authorUrl) ? authorUrl : null
  return (
    <span className={className}>
      Made with <Heart size={12} role="img" aria-label="love" className="inline-block -translate-y-px fill-current text-accent" />, <Coffee size={12} role="img" aria-label="coffee" className="inline-block -translate-y-px" /> and India by{' '}
      {safe ? (
        <a href={safe} target="_blank" rel="noreferrer noopener" className="font-semibold underline underline-offset-2 hover:text-ink">
          {author}
        </a>
      ) : (
        <span className="font-semibold">{author}</span>
      )}
    </span>
  )
}

/** A quiet line at the bottom of every page. */
export function Footer() {
  const { version } = useHaven()
  // The page above is still filling in for a moment. A footer that is visible meanwhile
  // would sit near the bottom and then jump away, so it appears once things have settled.
  const [settled, setSettled] = useState(false)
  useEffect(() => {
    const t = setTimeout(() => setSettled(true), 1800)
    return () => clearTimeout(t)
  }, [])
  return (
    <footer className={cx('mt-12 flex w-full flex-wrap items-center justify-center gap-x-3 gap-y-1 border-t border-rule-2 pt-5 text-xs text-muted', !settled && 'invisible')}>
      <span>Haven {version}</span>
      <span aria-hidden>·</span>
      <MadeBy />
      <span aria-hidden>·</span>
      <StarLink className="text-accent" />
    </footer>
  )
}

const CREDITS: { name: string; what: string; url: string }[] = [
  { name: 'Open-Meteo', what: 'Weather data (CC BY 4.0)', url: 'https://open-meteo.com' },
  { name: 'Google News', what: 'Headlines and story summaries, read from public feeds', url: 'https://news.google.com' },
  { name: 'European Central Bank', what: 'Currency reference rates, through Frankfurter', url: 'https://frankfurter.dev' },
  { name: 'Yahoo Finance', what: 'Market prices (can be delayed)', url: 'https://finance.yahoo.com' },
  { name: 'Paper Design shaders', what: 'Animated backgrounds (Apache-2.0)', url: 'https://github.com/paper-design/shaders' },
  { name: 'Lucide', what: 'Icons (ISC)', url: 'https://lucide.dev' },
  { name: 'IBM Plex Sans', what: 'Typeface (SIL Open Font License)', url: 'https://github.com/IBM/plex' },
  { name: 'JetBrains Mono', what: 'Typeface (SIL Open Font License)', url: 'https://www.jetbrains.com/lp/mono/' },
  { name: 'selfh.st icons', what: 'App logos in the store', url: 'https://selfh.st/icons/' },
]

export default function AboutPanel() {
  const { version, repo } = useHaven()
  const home = repoUrl(repo)
  return (
    <Sections>
      <section className="plate overflow-hidden" aria-label="About Haven">
        <div className="plate-head">
          <h2 className="etch">About Haven</h2>
          <span className="font-mono text-xs text-muted">{version}</span>
        </div>
        <div className="space-y-4 p-4 sm:p-5">
          <p className="text-sm">
            Haven is a simple, modern way to run apps on your own server: install, update and look after them from one place.
          </p>
          <p className="text-sm text-ink-2">
            <MadeBy />
          </p>
          {home ? (
            <>
              <p className="text-sm text-ink-2">If Haven is useful to you, a star on GitHub helps other people find it. Thank you!</p>
              <div className="flex flex-wrap gap-2">
                <a className="key key-primary" href={home} target="_blank" rel="noreferrer noopener">
                  <Star size={14} /> Star on GitHub
                </a>
                <a className="key" href={repoUrl(repo, '/issues')!} target="_blank" rel="noreferrer noopener">
                  Report a problem
                </a>
                <a className="key" href={repoUrl(repo, '/releases')!} target="_blank" rel="noreferrer noopener">
                  Release notes
                </a>
              </div>
            </>
          ) : (
            <p className="text-sm text-muted">This copy of Haven doesn't know its GitHub address.</p>
          )}
        </div>
      </section>

      <section className="plate overflow-hidden" aria-label="Credits">
        <div className="plate-head">
          <h2 className="etch">Credits and data sources</h2>
        </div>
        <ul className="divide-y divide-rule-2">
          {CREDITS.map((c) => (
            <li key={c.name} className="px-4 py-2.5 text-sm sm:px-5">
              <a href={c.url} target="_blank" rel="noreferrer noopener" className={ext}>
                {c.name}
              </a>
              <span className="text-ink-2"> · {c.what}</span>
            </li>
          ))}
          <li className="px-4 py-2.5 text-xs text-muted sm:px-5">App names and logos belong to their owners. Haven only installs software that its makers publish.</li>
        </ul>
      </section>
    </Sections>
  )
}
