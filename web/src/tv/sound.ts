// Soft interface sounds for TV mode, made on the fly with the Web Audio API
// (no sound files). Quiet by design: a light tick when focus moves, a
// two-note chime on OK, a falling note on Back, and a low bump at an edge.

type Kind = 'move' | 'select' | 'back' | 'edge'

let ctx: AudioContext | null = null
let enabled = readEnabled()

function readEnabled(): boolean {
  try {
    return localStorage.getItem('haven-tv-sound') !== 'off'
  } catch {
    return true
  }
}

export function soundOn(): boolean {
  return enabled
}

export function setSound(on: boolean) {
  enabled = on
  try {
    localStorage.setItem('haven-tv-sound', on ? 'on' : 'off')
  } catch {
    /* storage blocked */
  }
  if (on) play('select')
}

function audio(): AudioContext | null {
  if (!ctx) {
    const AC = window.AudioContext || (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
    if (!AC) return null
    ctx = new AC()
  }
  // Browsers start audio suspended until a key or click; a remote press counts.
  if (ctx.state === 'suspended') void ctx.resume()
  return ctx
}

function tone(ac: AudioContext, freq: number, endFreq: number, start: number, dur: number, peak: number, type: OscillatorType) {
  const osc = ac.createOscillator()
  const gain = ac.createGain()
  osc.type = type
  osc.frequency.setValueAtTime(freq, start)
  osc.frequency.exponentialRampToValueAtTime(endFreq, start + dur)
  // Quick fade in and a soft exponential fade out: no clicks.
  gain.gain.setValueAtTime(0.0001, start)
  gain.gain.exponentialRampToValueAtTime(peak, start + 0.008)
  gain.gain.exponentialRampToValueAtTime(0.0001, start + dur)
  osc.connect(gain).connect(ac.destination)
  osc.start(start)
  osc.stop(start + dur + 0.02)
}

export function play(kind: Kind) {
  if (!enabled) return
  const ac = audio()
  if (!ac) return
  const t = ac.currentTime + 0.005
  switch (kind) {
    case 'move':
      tone(ac, 1320, 1100, t, 0.045, 0.035, 'sine')
      break
    case 'select':
      tone(ac, 660, 660, t, 0.09, 0.05, 'triangle')
      tone(ac, 990, 990, t + 0.07, 0.14, 0.045, 'triangle')
      break
    case 'back':
      tone(ac, 740, 440, t, 0.14, 0.045, 'triangle')
      break
    case 'edge':
      tone(ac, 190, 150, t, 0.08, 0.05, 'sine')
      break
  }
}
