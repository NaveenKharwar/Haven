import type { ComponentType } from 'react'
import {
  ColorPanels, colorPanelsPresets, Dithering, ditheringPresets, DotGrid, dotGridPresets, DotOrbit, dotOrbitPresets,
  GodRays, godRaysPresets, GrainGradient, grainGradientPresets, MeshGradient, meshGradientPresets, Metaballs, metaballsPresets,
  NeuroNoise, neuroNoisePresets, PerlinNoise, perlinNoisePresets, SimplexNoise, simplexNoisePresets, SmokeRing, smokeRingPresets,
  Spiral, spiralPresets, StaticMeshGradient, staticMeshGradientPresets, StaticRadialGradient, staticRadialGradientPresets,
  Swirl, swirlPresets, Voronoi, voronoiPresets, Warp, warpPresets, Waves, wavesPresets,
} from '@paper-design/shaders-react'
import type { Background } from './api'

// Animated WebGL backgrounds from Paper Design's shaders (Apache-2.0), the
// same library beUI's Shader Background wraps. This module is loaded only
// when a background is switched on, so plain pages stay light.

type Preset = { name: string; params: Record<string, unknown> }
type Family = { id: string; label: string; Comp: ComponentType<any>; presets: Preset[] }

// Haven's own preset: the Faceplate graphite with a slow signal-orange glow.
// Shaders need literal colours, so this is the one place they appear.
const havenMesh: Preset = {
  name: 'Haven',
  params: { ...meshGradientPresets[0].params, colors: ['#15130f', '#221c16', '#3a2316', '#b0521c'], distortion: 0.9, swirl: 0.25, speed: 0.6 },
}

export const families: Family[] = [
  { id: 'meshGradient', label: 'Mesh', Comp: MeshGradient, presets: [havenMesh, ...meshGradientPresets] },
  { id: 'grainGradient', label: 'Grain', Comp: GrainGradient, presets: grainGradientPresets },
  { id: 'staticMeshGradient', label: 'Still mesh', Comp: StaticMeshGradient, presets: staticMeshGradientPresets },
  { id: 'staticRadialGradient', label: 'Radial', Comp: StaticRadialGradient, presets: staticRadialGradientPresets },
  { id: 'warp', label: 'Warp', Comp: Warp, presets: warpPresets },
  { id: 'waves', label: 'Waves', Comp: Waves, presets: wavesPresets },
  { id: 'smokeRing', label: 'Smoke', Comp: SmokeRing, presets: smokeRingPresets },
  { id: 'neuroNoise', label: 'Neuro', Comp: NeuroNoise, presets: neuroNoisePresets },
  { id: 'voronoi', label: 'Voronoi', Comp: Voronoi, presets: voronoiPresets },
  { id: 'swirl', label: 'Swirl', Comp: Swirl, presets: swirlPresets },
  { id: 'metaballs', label: 'Metaballs', Comp: Metaballs, presets: metaballsPresets },
  { id: 'godRays', label: 'Rays', Comp: GodRays, presets: godRaysPresets },
  { id: 'spiral', label: 'Spiral', Comp: Spiral, presets: spiralPresets },
  { id: 'dithering', label: 'Dither', Comp: Dithering, presets: ditheringPresets },
  { id: 'dotOrbit', label: 'Orbit', Comp: DotOrbit, presets: dotOrbitPresets },
  { id: 'dotGrid', label: 'Grid', Comp: DotGrid, presets: dotGridPresets },
  { id: 'perlinNoise', label: 'Perlin', Comp: PerlinNoise, presets: perlinNoisePresets },
  { id: 'simplexNoise', label: 'Simplex', Comp: SimplexNoise, presets: simplexNoisePresets },
  { id: 'colorPanels', label: 'Panels', Comp: ColorPanels, presets: colorPanelsPresets },
]

export function findBackground(b: Pick<Background, 'shader' | 'preset'>) {
  const fam = families.find((f) => f.id === b.shader)
  if (!fam) return null
  const preset = fam.presets.find((p) => p.name === b.preset) || fam.presets[0]
  return { fam, preset }
}

function reducedMotion() {
  return typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

/** Draws one shader preset filling its box. `speed` scales the preset's own speed. */
export function ShaderView({ shader, preset, speed = 1, maxPixelCount, className }: { shader: string; preset: string; speed?: number; maxPixelCount?: number; className?: string }) {
  const found = findBackground({ shader, preset })
  if (!found) return null
  const { fam, preset: p } = found
  const base = typeof p.params.speed === 'number' ? (p.params.speed as number) : 1
  return (
    <fam.Comp
      {...p.params}
      fit="cover"
      speed={reducedMotion() ? 0 : base * speed}
      maxPixelCount={maxPixelCount}
      className={className}
      style={{ width: '100%', height: '100%' }}
    />
  )
}

/** The page background: the shader, then a paper-coloured veil for readability. */
export default function Backdrop({ bg, tv }: { bg: Background; tv?: boolean }) {
  if (!findBackground(bg)) return null
  return (
    <div className="pointer-events-none fixed inset-0 -z-10" aria-hidden>
      <ShaderView shader={bg.shader} preset={bg.preset} speed={bg.speed} maxPixelCount={tv ? 1280 * 720 : 1920 * 1080} />
      <div className="absolute inset-0 bg-paper" style={{ opacity: bg.dim / 100 }} />
    </div>
  )
}
