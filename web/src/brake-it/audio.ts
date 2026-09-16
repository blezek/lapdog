export class CuePlayer {
  private context: AudioContext | null = null
  private enabled = true

  setEnabled(enabled: boolean) {
    this.enabled = enabled
  }

  async wake() {
    if (!this.enabled) return
    this.context ??= new AudioContext()
    if (this.context.state === 'suspended') await this.context.resume()
  }

  async play(kind: 'brake' | 'threshold' | 'trail' | 'transition' | 'accelerate' | 'done') {
    if (!this.enabled) return
    await this.wake()
    if (!this.context) return
    const patterns: Record<typeof kind, number[]> = {
      brake: [880],
      threshold: [520, 520],
      trail: [420],
      transition: [360, 440],
      accelerate: [620, 840],
      done: [720, 920, 1120],
    }
    patterns[kind].forEach((frequency, index) => {
      const context = this.context
      if (!context) return
      const start = context.currentTime + index * 0.14
      const oscillator = context.createOscillator()
      const gain = context.createGain()
      oscillator.frequency.value = frequency
      gain.gain.setValueAtTime(0.001, start)
      gain.gain.exponentialRampToValueAtTime(0.16, start + 0.015)
      gain.gain.exponentialRampToValueAtTime(0.001, start + 0.12)
      oscillator.connect(gain)
      gain.connect(context.destination)
      oscillator.start(start)
      oscillator.stop(start + 0.13)
    })
  }

  async close() {
    await this.context?.close().catch(() => undefined)
    this.context = null
  }
}
