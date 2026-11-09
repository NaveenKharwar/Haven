import { useState } from 'react'
import {
  Activity,
  ArrowDownRight,
  ArrowUpRight,
  ExternalLink,
  Wind,
} from 'lucide-react'
import { EditCta, EditPencil } from './widgeteditors'
import { Widget } from './widgetshell'
import { CityTabs, Problem } from './widgets'
import { Modal, Readout, Trace, cx } from './components'
import type { AirQuality } from './api'

// --- AQI Scale Segments -------------------------------------------------------

export const AQI_SEGMENTS = [
  { label: 'Good (0-50)', color: 'oklch(62% 0.19 145)', min: 0, max: 50 },
  { label: 'Moderate (51-100)', color: 'oklch(74% 0.17 80)', min: 51, max: 100 },
  { label: 'Sensitive (101-150)', color: 'oklch(68% 0.20 45)', min: 101, max: 150 },
  { label: 'Unhealthy (151-200)', color: 'oklch(60% 0.22 27)', min: 151, max: 200 },
  { label: 'Very Unhealthy (201-300)', color: 'oklch(52% 0.22 305)', min: 201, max: 300 },
  { label: 'Hazardous (301+)', color: 'oklch(38% 0.18 15)', min: 301, max: Infinity },
]

// --- Categories and Styling ----------------------------------------------------

export type AQILevel =
  | 'Good'
  | 'Moderate'
  | 'Sensitive'
  | 'Unhealthy'
  | 'Very Unhealthy'
  | 'Hazardous'

export function getAQILevel(aqi: number): {
  level: AQILevel
  label: string
  tagClass: string
  textClass: string
  advisory: string
  maskRecommended: boolean
  airPurifierRecommended: boolean
  outdoorExercise: string
} {
  if (aqi <= 50) {
    return {
      level: 'Good',
      label: 'Good',
      tagClass: 'tag-ok',
      textClass: 'text-ok',
      advisory: 'Air quality is satisfactory with little or no health risk.',
      maskRecommended: false,
      airPurifierRecommended: false,
      outdoorExercise: 'Normal',
    }
  }
  if (aqi <= 100) {
    return {
      level: 'Moderate',
      label: 'Moderate',
      tagClass: 'tag-warn',
      textClass: 'text-warn',
      advisory: 'Acceptable air quality. Sensitive individuals should take care.',
      maskRecommended: false,
      airPurifierRecommended: false,
      outdoorExercise: 'Normal',
    }
  }
  if (aqi <= 150) {
    return {
      level: 'Sensitive',
      label: 'Sensitive',
      tagClass: 'tag-accent',
      textClass: 'text-accent',
      advisory: 'Sensitive groups may experience symptoms. Reduce prolonged outdoor exertion.',
      maskRecommended: true,
      airPurifierRecommended: true,
      outdoorExercise: 'Reduce',
    }
  }
  if (aqi <= 200) {
    return {
      level: 'Unhealthy',
      label: 'Unhealthy',
      tagClass: 'tag-bad',
      textClass: 'text-bad',
      advisory: 'Everyone may experience health effects. Keep windows closed.',
      maskRecommended: true,
      airPurifierRecommended: true,
      outdoorExercise: 'Avoid',
    }
  }
  if (aqi <= 300) {
    return {
      level: 'Very Unhealthy',
      label: 'Very Unhealthy',
      tagClass: 'tag-bad',
      textClass: 'text-bad',
      advisory: 'Health alert: Increased risk for all residents. Remain indoors.',
      maskRecommended: true,
      airPurifierRecommended: true,
      outdoorExercise: 'Avoid',
    }
  }
  return {
    level: 'Hazardous',
    label: 'Hazardous',
    tagClass: 'tag-bad',
    textClass: 'text-bad',
    advisory: 'Emergency conditions. Avoid all outdoor activity.',
    maskRecommended: true,
    airPurifierRecommended: true,
    outdoorExercise: 'Avoid',
  }
}

// --- Compact Air Quality Widget -----------------------------------------------

export function AirQualityWidget({
  list,
  error,
}: {
  list?: AirQuality[]
  error?: string
}) {
  const [i, setI] = useState(0)
  const [modalOpen, setModalOpen] = useState(false)

  const dataList = list || []

  if (!dataList || dataList.length === 0) {
    return (
      <Widget title="Air Quality" aside={<EditPencil kind="aqi" />}>
        {error ? (
          <Problem text={error} />
        ) : (
          <div className="flex flex-1 flex-col items-start justify-center gap-3 p-4">
            <p className="text-sm text-ink-2">
              Choose your city to monitor live air quality.
            </p>
            <EditCta kind="aqi">Choose city</EditCta>
          </div>
        )}
      </Widget>
    )
  }

  const idx = Math.min(i, dataList.length - 1)
  const aq = dataList[idx]
  const info = getAQILevel(aq.aqi)

  const trendValues =
    aq.hourly && aq.hourly.length > 1
      ? aq.hourly.map((h) => h.aqi)
      : [aq.aqi * 0.9, aq.aqi * 1.05, aq.aqi * 1.1, aq.aqi * 0.95, aq.aqi]

  return (
    <>
      <Widget
        title={dataList.length > 1 ? 'Air Quality' : `Air Quality · ${aq.place}`}
        aside={
          <span className="flex items-center gap-1.5">
            <button
              type="button"
              onClick={() => setModalOpen(true)}
              className="key key-sm key-flat text-xs font-mono text-accent hover:text-ink flex items-center gap-1 px-1.5 py-0.5 cursor-pointer"
              title="Details & Trends"
              aria-label="View air quality details"
            >
              <ExternalLink size={12} />
              <span>Details</span>
            </button>
            <EditPencil kind="aqi" />
          </span>
        }
      >
        {dataList.length > 1 && (
          <CityTabs
            names={dataList.map((x) => x.place)}
            value={idx}
            onChange={setI}
          />
        )}
        <Problem text={error} />

        {/* Compact Card Faceplate */}
        <div
          className="flex flex-1 flex-col p-4 cursor-pointer hover:bg-raised/50 transition-colors group select-none"
          onClick={() => setModalOpen(true)}
          title="Open details"
          role="button"
          tabIndex={0}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              setModalOpen(true)
            }
          }}
        >
          {/* Main Readout Row */}
          <div className="flex items-baseline justify-between gap-3">
            <div className="flex items-baseline gap-2">
              <span className="text-4xl font-mono font-semibold tracking-tight">
                <Readout value={aq.aqi} digits={3} className={info.textClass} />
              </span>
              <span className="font-mono text-xs text-muted">AQI</span>
            </div>
            <span className={cx('tag', info.tagClass)}>{info.label}</span>
          </div>

          {/* Subtitle / Dominant Pollutant */}
          <div className="mt-1 flex items-center justify-between text-xs text-muted">
            <span>
              Primary:{' '}
              <span className="font-medium text-ink-2">
                {aq.dominant || 'PM2.5'}
              </span>
            </span>
            <span className="font-mono text-[0.6875rem]">US EPA</span>
          </div>

          {/* Compact 2-column pollutant readout */}
          <div className="mt-3 grid grid-cols-2 gap-2 border-t border-rule-2 pt-2.5 font-mono text-xs">
            <div>
              <span className="text-muted block text-[0.6875rem]">PM2.5</span>
              <span className="font-semibold text-ink">
                {aq.pm25.toFixed(1)}
              </span>{' '}
              <span className="text-[0.6875rem] text-muted">µg/m³</span>
            </div>
            <div>
              <span className="text-muted block text-[0.6875rem]">PM10</span>
              <span className="font-semibold text-ink">
                {aq.pm10.toFixed(1)}
              </span>{' '}
              <span className="text-[0.6875rem] text-muted">µg/m³</span>
            </div>
          </div>

          {/* 24-hour mini trend sparkline */}
          <div className="mt-auto pt-3">
            <div className="flex items-center justify-between font-mono text-[0.625rem] text-muted mb-1">
              <span className="flex items-center gap-1">
                <Activity size={10} /> 24H TREND
              </span>
              <span>
                {Math.round(Math.min(...trendValues))}–
                {Math.round(Math.max(...trendValues))} AQI
              </span>
            </div>
            <Trace values={trendValues} height={26} />
          </div>
        </div>
      </Widget>

      {/* Full Details Modal Popup */}
      {modalOpen && (
        <AirQualityModal
          aq={aq}
          onClose={() => setModalOpen(false)}
        />
      )}
    </>
  )
}

// --- Full Details Modal Popup -------------------------------------------------

type MetricType = 'aqi' | 'pm25' | 'pm10'
type TimeframeType = '24h' | '7d' | 'years'

export function AirQualityModal({
  aq,
  onClose,
}: {
  aq: AirQuality
  onClose: () => void
}) {
  const [metric, setMetric] = useState<MetricType>('aqi')
  const [timeframe, setTimeframe] = useState<TimeframeType>('years')
  const info = getAQILevel(aq.aqi)

  // Current year history vs previous years
  const history = aq.history || []
  const data2026 = history.find((h) => h.year === 2026)
  const data2025 = history.find((h) => h.year === 2025)
  const data2024 = history.find((h) => h.year === 2024)

  const currentValues =
    timeframe === '24h'
      ? (aq.hourly || []).map((h) =>
          metric === 'aqi' ? h.aqi : metric === 'pm25' ? h.pm25 : h.pm10
        )
      : data2026
      ? data2026[metric]
      : [aq[metric]]

  const values2025 = data2025 ? data2025[metric] : []
  const values2024 = data2024 ? data2024[metric] : []

  const avg2026 =
    currentValues.length > 0
      ? currentValues.reduce((a, b) => a + b, 0) / currentValues.length
      : aq[metric]
  const avg2025 =
    values2025.length > 0
      ? values2025.reduce((a, b) => a + b, 0) / values2025.length
      : avg2026 * 1.08

  const diffPct =
    avg2025 > 0 ? ((avg2026 - avg2025) / avg2025) * 100 : 0
  const isBetter = diffPct <= 0

  const unit = metric === 'aqi' ? 'AQI' : 'µg/m³'

  // 24h stats
  const hourlyVals = (aq.hourly || []).map((h) =>
    metric === 'aqi' ? h.aqi : metric === 'pm25' ? h.pm25 : h.pm10
  )
  const peak24h = hourlyVals.length > 0 ? Math.max(...hourlyVals) : aq[metric]
  const min24h = hourlyVals.length > 0 ? Math.min(...hourlyVals) : aq[metric]
  const peakIdx = hourlyVals.indexOf(peak24h)
  const peakHour = aq.hourly?.[peakIdx]?.time || '08:00'
  const minIdx = hourlyVals.indexOf(min24h)
  const minHour = aq.hourly?.[minIdx]?.time || '14:00'

  // 7-day stats
  const avg7d =
    currentValues.length > 0
      ? currentValues.reduce((a, b) => a + b, 0) / currentValues.length
      : aq[metric]
  const max7d = currentValues.length > 0 ? Math.max(...currentValues) : aq[metric]
  const min7d = currentValues.length > 0 ? Math.min(...currentValues) : aq[metric]

  return (
    <Modal
      open
      wide
      onClose={onClose}
      label="Air Quality"
      title={aq.place}
    >
      <div className="space-y-5">
        {/* Top Summary Banner */}
        <div className="plate p-4 sm:p-5 flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4">
          <div className="space-y-1">
            <div className="flex items-baseline gap-3">
              <span className="text-5xl font-mono font-bold tracking-tight">
                <Readout value={aq.aqi} digits={3} className={info.textClass} />
              </span>
              <div>
                <span className={cx('tag', info.tagClass)}>{info.label}</span>
                <span className="block font-mono text-[0.6875rem] text-muted mt-0.5">
                  US EPA standard
                </span>
              </div>
            </div>
            <p className="text-sm text-ink-2 max-w-xl pt-1 leading-relaxed">
              {info.advisory}
            </p>
          </div>

          {/* Quick Action Badges */}
          <div className="flex sm:flex-col gap-2 shrink-0 border-t sm:border-t-0 sm:border-l border-rule-2 pt-3 sm:pt-0 sm:pl-4 font-mono text-xs">
            <div className="flex items-center gap-2">
              <span
                className={cx(
                  'h-2 w-2 rounded-full',
                  info.maskRecommended ? 'bg-warn' : 'bg-ok'
                )}
              />
              <span className="text-ink-2">
                Mask: {info.maskRecommended ? 'N95' : 'None'}
              </span>
            </div>
            <div className="flex items-center gap-2">
              <span
                className={cx(
                  'h-2 w-2 rounded-full',
                  info.airPurifierRecommended ? 'bg-warn' : 'bg-ok'
                )}
              />
              <span className="text-ink-2">
                Purifier:{' '}
                {info.airPurifierRecommended ? 'Recommended' : 'Optional'}
              </span>
            </div>
            <div className="flex items-center gap-2">
              <span
                className={cx(
                  'h-2 w-2 rounded-full',
                  info.outdoorExercise === 'Avoid'
                    ? 'bg-bad'
                    : info.outdoorExercise === 'Reduce'
                    ? 'bg-warn'
                    : 'bg-ok'
                )}
              />
              <span className="text-ink-2">
                Exercise: {info.outdoorExercise}
              </span>
            </div>
          </div>
        </div>

        {/* 6-Stage AQI Scale Visualizer */}
        <div className="space-y-1.5">
          <div className="flex items-center justify-between text-xs font-mono text-muted">
            <span>AQI Scale</span>
            <span className="text-ink font-semibold">
              Current: <span className={info.textClass}>{aq.aqi} · {info.label}</span>
            </span>
          </div>
          <div className="grid grid-cols-6 gap-1 h-3 rounded-[2px] p-0.5 bg-well border border-rule">
            {AQI_SEGMENTS.map((seg) => {
              const active =
                aq.aqi >= seg.min && (seg.max === Infinity ? true : aq.aqi <= seg.max)
              return (
                <div
                  key={seg.label}
                  className={cx(
                    'h-full rounded-[1px] transition-all relative',
                    active
                      ? 'ring-2 ring-ink ring-offset-1 ring-offset-paper z-10 opacity-100'
                      : 'opacity-40 hover:opacity-75'
                  )}
                  style={{ backgroundColor: seg.color }}
                  title={seg.label}
                />
              )
            })}
          </div>
          <div className="flex justify-between font-mono text-[0.625rem] text-muted">
            <span>0 Good</span>
            <span>50</span>
            <span>100</span>
            <span>150</span>
            <span>200</span>
            <span>300+</span>
          </div>
        </div>

        {/* Interactive Graph Section */}
        <div className="plate p-4 sm:p-5 space-y-3">
          {/* Header Row: Clean Title on Left, Stable Controls on Right */}
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-rule-2 pb-3">
            <h3 className="text-sm font-semibold flex items-center gap-2 text-ink">
              <Activity size={15} className="text-accent" />
              Trends
            </h3>

            {/* Permanent Toolbar: Metric Selector & Timeframe Selector */}
            <div className="flex flex-wrap items-center gap-2 shrink-0">
              {/* Metric Selector */}
              <div className="well flex items-center p-0.5 text-xs font-mono">
                {(['aqi', 'pm25', 'pm10'] as MetricType[]).map((m) => (
                  <button
                    key={m}
                    onClick={() => setMetric(m)}
                    className={cx(
                      'key key-sm h-7 w-14 justify-center border-transparent uppercase font-mono transition-all',
                      metric === m ? 'key-primary font-bold' : 'key-flat'
                    )}
                    aria-pressed={metric === m}
                  >
                    {m === 'pm25' ? 'PM2.5' : m === 'pm10' ? 'PM10' : 'AQI'}
                  </button>
                ))}
              </div>

              {/* Timeframe Selector */}
              <div className="well flex items-center p-0.5 text-xs font-mono">
                {(
                  [
                    ['24h', '24H'],
                    ['7d', '7 Days'],
                    ['years', 'Year Comparison'],
                  ] as const
                ).map(([t, label]) => (
                  <button
                    key={t}
                    onClick={() => setTimeframe(t)}
                    className={cx(
                      'key key-sm h-7 px-2.5 border-transparent font-mono transition-all',
                      timeframe === t ? 'key-primary font-bold' : 'key-flat'
                    )}
                    aria-pressed={timeframe === t}
                  >
                    {label}
                  </button>
                ))}
              </div>
            </div>
          </div>

          {/* Permanent Telemetry & Stats Banner */}
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 min-h-[2.5rem] bg-paper px-3 py-2 rounded-[2px] border border-rule font-mono text-xs">
            {timeframe === 'years' ? (
              <>
                <div className="flex items-center gap-2">
                  {isBetter ? (
                    <span className="flex items-center gap-1 text-ok font-semibold">
                      <ArrowDownRight size={14} />
                      {Math.abs(diffPct).toFixed(1)}% Lower Pollution
                    </span>
                  ) : (
                    <span className="flex items-center gap-1 text-bad font-semibold">
                      <ArrowUpRight size={14} />
                      {Math.abs(diffPct).toFixed(1)}% Higher Pollution
                    </span>
                  )}
                  <span className="text-muted hidden sm:inline">
                    vs 2025
                  </span>
                </div>
                <div className="flex items-center gap-3 text-muted text-[0.6875rem]">
                  <span>2026: <strong className="text-ink">{avg2026.toFixed(1)}</strong></span>
                  <span>2025: <strong className="text-ink">{avg2025.toFixed(1)}</strong></span>
                  {data2024 && (
                    <span>
                      2024:{' '}
                      <strong className="text-ink">
                        {(
                          data2024[metric].reduce((a, b) => a + b, 0) /
                          data2024[metric].length
                        ).toFixed(1)}
                      </strong>
                    </span>
                  )}
                </div>
              </>
            ) : timeframe === '24h' ? (
              <>
                <div className="flex items-center gap-2">
                  <span className="flex items-center gap-1 text-ink font-semibold">
                    <span className="h-1.5 w-1.5 rounded-full bg-warn" />
                    Peak: {peakHour} ({peak24h.toFixed(1)} {unit})
                  </span>
                </div>
                <div className="flex items-center gap-3 text-muted text-[0.6875rem]">
                  <span>
                    Cleanest:{' '}
                    <strong className="text-ink">
                      {minHour} ({min24h.toFixed(1)} {unit})
                    </strong>
                  </span>
                  <span>
                    Range:{' '}
                    <strong className="text-ink">
                      {(peak24h - min24h).toFixed(1)} {unit}
                    </strong>
                  </span>
                </div>
              </>
            ) : (
              <>
                <div className="flex items-center gap-2">
                  <span className="flex items-center gap-1 text-ink font-semibold">
                    <span className="h-1.5 w-1.5 rounded-full bg-accent" />
                    7-Day Avg: {avg7d.toFixed(1)} {unit}
                  </span>
                </div>
                <div className="flex items-center gap-3 text-muted text-[0.6875rem]">
                  <span>
                    Peak: <strong className="text-ink">{max7d.toFixed(1)}</strong>
                  </span>
                  <span>
                    Low: <strong className="text-ink">{min7d.toFixed(1)}</strong>
                  </span>
                </div>
              </>
            )}
          </div>

          {/* Custom Multi-Line SVG Chart */}
          <div className="pt-1">
            <InteractiveChart
              timeframe={timeframe}
              metric={metric}
              values2026={currentValues}
              values2025={values2025}
              values2024={values2024}
              unit={unit}
            />
          </div>

          {/* Chart Legend with stable layout */}
          <div className="flex flex-wrap items-center justify-between gap-3 pt-2.5 border-t border-rule-2 text-xs font-mono text-muted min-h-[2rem]">
            <div className="flex items-center gap-3 sm:gap-4 flex-wrap">
              <span className="flex items-center gap-1.5">
                <span className="h-2 w-3 rounded-sm bg-accent" />
                <span className="text-ink font-medium">2026</span>
              </span>
              <span
                className={cx(
                  'flex items-center gap-1.5 transition-opacity',
                  timeframe === 'years' ? 'opacity-100' : 'opacity-40'
                )}
              >
                <span className="h-0.5 w-3 border-t-2 border-dashed border-warn" />
                <span>2025</span>
              </span>
              <span
                className={cx(
                  'flex items-center gap-1.5 transition-opacity',
                  timeframe === 'years' ? 'opacity-100' : 'opacity-40'
                )}
              >
                <span className="h-0.5 w-3 border-t-2 border-dotted border-rule-hi" />
                <span>2024</span>
              </span>
            </div>
            <div className="flex items-center gap-1.5 text-[0.6875rem] text-ok">
              <span className="h-1.5 w-1.5 rounded-full bg-ok" />
              <span>
                Target:{' '}
                {metric === 'pm25'
                  ? '15 µg/m³'
                  : metric === 'pm10'
                  ? '45 µg/m³'
                  : '50 AQI'}
              </span>
            </div>
          </div>
        </div>

        {/* Pollutants Matrix */}
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-semibold flex items-center gap-2 text-ink">
              <Wind size={15} className="text-muted" />
              Pollutants
            </h3>
            <span className="font-mono text-[0.6875rem] text-muted">
              WHO 24h targets
            </span>
          </div>

          <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
            <PollutantCard
              name="PM2.5"
              title="Fine particles"
              value={aq.pm25}
              unit="µg/m³"
              guideline={15}
            />
            <PollutantCard
              name="PM10"
              title="Coarse dust"
              value={aq.pm10}
              unit="µg/m³"
              guideline={45}
            />
            <PollutantCard
              name="NO₂"
              title="Nitrogen dioxide"
              value={aq.no2}
              unit="µg/m³"
              guideline={25}
            />
            <PollutantCard
              name="O₃"
              title="Ozone"
              value={aq.o3}
              unit="µg/m³"
              guideline={100}
            />
            <PollutantCard
              name="CO"
              title="Carbon monoxide"
              value={aq.co}
              unit="µg/m³"
              guideline={4000}
            />
            <PollutantCard
              name="SO₂"
              title="Sulphur dioxide"
              value={aq.so2}
              unit="µg/m³"
              guideline={40}
            />
          </div>
        </div>

        {/* Attribution Footer */}
        <div className="flex items-center justify-between pt-2 border-t border-rule-2 font-mono text-[0.6875rem] text-muted">
          <span>Open-Meteo · Copernicus CAMS</span>
          <span>Hourly</span>
        </div>
      </div>
    </Modal>
  )
}

// --- Pollutant Card -----------------------------------------------------------

function PollutantCard({
  name,
  title,
  value,
  unit,
  guideline,
}: {
  name: string
  title: string
  value: number
  unit: string
  guideline: number
}) {
  const ratio = guideline > 0 ? value / guideline : 0
  const isOver = ratio > 1.0

  return (
    <div className="plate p-3.5 flex flex-col justify-between space-y-2">
      <div>
        <div className="flex items-center justify-between">
          <span className="font-mono text-xs font-semibold text-ink">
            {name}
          </span>
          <span className={cx('tag', isOver ? 'tag-bad' : 'tag-ok')}>
            {isOver ? `${ratio.toFixed(1)}x` : 'Safe'}
          </span>
        </div>
        <div className="text-[0.6875rem] text-muted">{title}</div>
      </div>

      <div className="space-y-0.5 pt-1">
        <div className="text-xl font-mono font-semibold text-ink">
          {value.toFixed(1)}{' '}
          <span className="text-xs font-normal text-muted">{unit}</span>
        </div>
        <div className="text-[0.625rem] font-mono text-muted">
          Target: ≤ {guideline} {unit}
        </div>
      </div>
    </div>
  )
}

// --- Interactive Multi-Line SVG Chart -----------------------------------------

function InteractiveChart({
  timeframe,
  metric,
  values2026,
  values2025,
  values2024,
  unit,
}: {
  timeframe: TimeframeType
  metric: MetricType
  values2026: number[]
  values2025: number[]
  values2024: number[]
  unit: string
}) {
  const [hoverIndex, setHoverIndex] = useState<number | null>(null)

  const w = 600
  const h = 180
  const paddingBottom = 24
  const paddingTop = 16
  const paddingLeft = 42
  const paddingRight = 16

  const chartW = w - paddingLeft - paddingRight
  const chartH = h - paddingTop - paddingBottom

  const allValues = [
    ...values2026,
    ...(timeframe === 'years' ? [...values2025, ...values2024] : []),
  ]
  const maxValue = Math.max(...allValues, metric === 'pm25' ? 30 : 60) * 1.15
  const minValue = 0

  const getY = (v: number) =>
    paddingTop + chartH - ((v - minValue) / (maxValue - minValue)) * chartH
  const getX = (i: number, total: number) =>
    paddingLeft + (i / Math.max(1, total - 1)) * chartW

  const buildPath = (vals: number[]) => {
    if (!vals || vals.length < 2) return ''
    return vals
      .map(
        (v, i) =>
          `${i === 0 ? 'M' : 'L'}${getX(i, vals.length).toFixed(1)},${getY(
            v
          ).toFixed(1)}`
      )
      .join(' ')
  }

  const path2026 = buildPath(values2026)
  const path2025 = buildPath(values2025)
  const path2024 = buildPath(values2024)

  const targetGuideline =
    metric === 'pm25' ? 15 : metric === 'pm10' ? 45 : 50
  const targetY = getY(targetGuideline)

  const labels =
    timeframe === '24h'
      ? ['00:00', '04:00', '08:00', '12:00', '16:00', '20:00']
      : ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

  return (
    <div className="relative w-full overflow-hidden select-none">
      <svg
        viewBox={`0 0 ${w} ${h}`}
        className="w-full h-auto overflow-visible font-mono text-[10px]"
        onMouseLeave={() => setHoverIndex(null)}
      >
        {/* Horizontal grid lines & Y labels */}
        {[0, 0.33, 0.66, 1].map((pct, i) => {
          const val = Math.round(minValue + (maxValue - minValue) * pct)
          const y = paddingTop + chartH - pct * chartH
          return (
            <g key={i}>
              <line
                x1={paddingLeft}
                x2={w - paddingRight}
                y1={y}
                y2={y}
                stroke="var(--rule-2)"
                strokeWidth="1"
                strokeDasharray={pct === 0 ? undefined : '2 4'}
              />
              <text
                x={paddingLeft - 6}
                y={y + 3}
                fill="var(--muted)"
                textAnchor="end"
                fontSize="9"
              >
                {val}
              </text>
            </g>
          )
        })}

        {/* Target guideline line */}
        {targetY >= paddingTop && targetY <= paddingTop + chartH && (
          <g>
            <line
              x1={paddingLeft}
              x2={w - paddingRight}
              y1={targetY}
              y2={targetY}
              stroke="var(--ok)"
              strokeWidth="1.2"
              strokeDasharray="4 3"
              opacity="0.9"
            />
            <text
              x={w - paddingRight - 4}
              y={targetY - 4}
              fill="var(--ok)"
              textAnchor="end"
              fontSize="8"
              fontWeight="600"
            >
              Target ({targetGuideline})
            </text>
          </g>
        )}

        {/* 2024 Trace Line */}
        {timeframe === 'years' && path2024 && (
          <path
            d={path2024}
            fill="none"
            stroke="var(--rule-hi)"
            strokeWidth="1.5"
            strokeDasharray="2 3"
          />
        )}

        {/* 2025 Trace Line */}
        {timeframe === 'years' && path2025 && (
          <path
            d={path2025}
            fill="none"
            stroke="var(--warn)"
            strokeWidth="1.5"
            strokeDasharray="4 4"
            opacity="0.85"
          />
        )}

        {/* 2026 Current Year Trace Line */}
        {path2026 && (
          <path
            d={path2026}
            fill="none"
            stroke="var(--accent)"
            strokeWidth="2.5"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        )}

        {/* Interactive Hover Point & Vertical Crosshair */}
        {hoverIndex !== null && hoverIndex < values2026.length && (
          <g>
            <line
              x1={getX(hoverIndex, values2026.length)}
              x2={getX(hoverIndex, values2026.length)}
              y1={paddingTop}
              y2={paddingTop + chartH}
              stroke="var(--ink)"
              strokeWidth="1"
              strokeDasharray="2 2"
              opacity="0.6"
            />
            <circle
              cx={getX(hoverIndex, values2026.length)}
              cy={getY(values2026[hoverIndex])}
              r="4.5"
              fill="var(--accent)"
              stroke="var(--paper)"
              strokeWidth="2"
            />
          </g>
        )}

        {/* Invisible mouse hover touch targets */}
        {values2026.map((_, i) => {
          const x = getX(i, values2026.length)
          const sliceW = chartW / values2026.length
          return (
            <rect
              key={i}
              x={x - sliceW / 2}
              y={paddingTop}
              width={sliceW}
              height={chartH}
              fill="transparent"
              className="cursor-pointer"
              onMouseEnter={() => setHoverIndex(i)}
            />
          )
        })}

        {/* X-axis labels */}
        {labels.map((lbl, i) => {
          const x = paddingLeft + (i / Math.max(1, labels.length - 1)) * chartW
          return (
            <text
              key={i}
              x={x}
              y={h - 6}
              fill="var(--muted)"
              textAnchor="middle"
              fontSize="9"
            >
              {lbl}
            </text>
          )
        })}
      </svg>

      {/* Floating Hover Tooltip */}
      {hoverIndex !== null && hoverIndex < values2026.length && (
        <div
          className="absolute z-20 pointer-events-none bg-paper border border-rule px-2.5 py-1.5 rounded-[2px] shadow-lg font-mono text-[11px] backdrop-blur-sm"
          style={{
            left: `clamp(10px, ${(
              (getX(hoverIndex, values2026.length) / w) *
              100
            ).toFixed(1)}%, calc(100% - 130px))`,
            top: '8px',
          }}
        >
          <div className="text-muted text-[9px] mb-0.5">
            {timeframe === '24h'
              ? labels[Math.min(hoverIndex, labels.length - 1)] || ''
              : labels[Math.min(hoverIndex, labels.length - 1)] || ''}
          </div>
          <div className="text-accent font-semibold">
            2026: {values2026[hoverIndex]?.toFixed(1)} {unit}
          </div>
          {timeframe === 'years' && values2025[hoverIndex] !== undefined && (
            <div className="text-warn text-[10px]">
              2025: {values2025[hoverIndex]?.toFixed(1)} {unit}
            </div>
          )}
          {timeframe === 'years' && values2024[hoverIndex] !== undefined && (
            <div className="text-muted text-[10px]">
              2024: {values2024[hoverIndex]?.toFixed(1)} {unit}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
