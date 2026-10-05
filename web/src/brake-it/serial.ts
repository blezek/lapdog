import { clamp } from './scenario'
import type { ControllerDevice, PedalInput } from './types'

export type SerialConnection = { close: () => Promise<void> }

const portIDs = new WeakMap<SerialPort, number>()
let nextPortID = 1

function idForPort(port: SerialPort): string {
  let id = portIDs.get(port)
  if (id === undefined) {
    id = nextPortID
    nextPortID += 1
    portIDs.set(port, id)
  }
  return `serial-${id}`
}

function toPercent(value: unknown): number | null {
  if (typeof value !== 'number' || !Number.isFinite(value)) return null
  return clamp(value <= 1 ? value * 100 : value, 0, 100)
}

function readAxis(message: Record<string, unknown>, names: string[]): number | null {
  for (const name of names) {
    const value = toPercent(message[name])
    if (value !== null) return value
  }
  return null
}

export function parseSerialMessage(line: string): Record<string, unknown> | null {
  const trimmed = line.trim()
  if (!trimmed) return null
  try {
    const parsed: unknown = JSON.parse(trimmed)
    return parsed !== null && typeof parsed === 'object' && !Array.isArray(parsed)
      ? parsed as Record<string, unknown>
      : null
  } catch {
    const message: Record<string, unknown> = {}
    for (const part of trimmed.split(/[,\s]+/)) {
      const [key, raw] = part.split('=')
      const value = Number(raw)
      if (key && Number.isFinite(value)) message[key] = value
    }
    return Object.keys(message).length > 0 ? message : null
  }
}

export function hasWebSerial(): boolean {
  return typeof navigator !== 'undefined' && navigator.serial !== undefined
}

function labelForPort(port: SerialPort, fallbackIndex = 1): string {
  const info = port.getInfo()
  const vendor = info.usbVendorId?.toString(16).padStart(4, '0')
  const product = info.usbProductId?.toString(16).padStart(4, '0')
  return vendor || product ? `Serial ${vendor ?? '----'}:${product ?? '----'}` : `Serial controller ${fallbackIndex}`
}

export async function getAuthorizedSerialDevices(): Promise<ControllerDevice[]> {
  if (!navigator.serial) return []
  const ports = await navigator.serial.getPorts()
  return ports.map((port, index) => ({
    id: idForPort(port),
    label: labelForPort(port, index + 1),
    kind: 'serial',
    status: 'available',
    detail: 'Permission granted',
    port,
  }))
}

export async function requestSerialDevice(filters: SerialPortFilter[]): Promise<ControllerDevice> {
  if (!navigator.serial) throw new Error('Web Serial is not available in this browser.')
  const port = await navigator.serial.requestPort(filters.length > 0 ? { filters } : undefined)
  return {
    id: idForPort(port),
    label: labelForPort(port),
    kind: 'serial',
    status: 'available',
    detail: 'Permission granted',
    port,
  }
}

export async function openSerialController(
  port: SerialPort,
  baudRate: number,
  onInput: (input: PedalInput) => void,
  onStatus: (status: string, hasRequiredAxes?: boolean) => void,
): Promise<SerialConnection> {
  let reader: ReadableStreamDefaultReader<Uint8Array> | null = null
  let keepReading = true
  const decoder = new TextDecoder()
  await port.open({ baudRate })
  onStatus('Connected, waiting for pedal data')
  if (port.writable) {
    const writer = port.writable.getWriter()
    await writer.write(new TextEncoder().encode('{"type":"identify","app":"brake-it","axes":["accelerator","brake"]}\n'))
    writer.releaseLock()
  }
  const readLoop = async () => {
    let buffer = ''
    while (keepReading && port.readable) {
      reader = port.readable.getReader()
      try {
        while (keepReading) {
          const { value, done } = await reader.read()
          if (done) break
          buffer += decoder.decode(value, { stream: true })
          const lines = buffer.split(/\r?\n/)
          buffer = lines.pop() ?? ''
          for (const line of lines) {
            const message = parseSerialMessage(line)
            if (!message) continue
            const accelerator = readAxis(message, ['accelerator', 'accel', 'throttle'])
            const brake = readAxis(message, ['brake', 'brakes'])
            if (accelerator !== null && brake !== null) {
              onStatus('Streaming accelerator and brake', true)
              onInput({ accelerator, brake, source: 'serial' })
            }
          }
        }
      } catch (error) {
        if (keepReading) {
          keepReading = false
          onStatus(error instanceof Error ? error.message : 'Serial read error')
        }
      } finally {
        reader.releaseLock()
        reader = null
      }
    }
  }
  const readTask = readLoop()
  return {
    close: async () => {
      keepReading = false
      await reader?.cancel().catch(() => undefined)
      await readTask.catch(() => undefined)
      await port.close().catch(() => undefined)
    },
  }
}
