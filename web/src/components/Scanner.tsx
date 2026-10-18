import { useEffect, useRef, useState } from 'react'

declare global {
  interface Window { BarcodeDetector?: new (opts?: { formats?: string[] }) => { detect: (src: ImageBitmapSource) => Promise<{ rawValue: string }[]> } }
}

/** Camera barcode reader (BarcodeDetector API) with a typed fallback. */
export function Scanner({ onCode }: { onCode: (code: string) => void }) {
  const video = useRef<HTMLVideoElement>(null)
  const [supported] = useState(() => typeof window !== 'undefined' && !!window.BarcodeDetector)
  const [err, setErr] = useState('')
  const [manual, setManual] = useState('')
  const [active, setActive] = useState(supported)
  useEffect(() => {
    if (!active || !supported) return
    let stream: MediaStream | null = null
    let stop = false
    const det = new window.BarcodeDetector!({ formats: ['ean_13', 'ean_8', 'upc_a', 'upc_e', 'code_128', 'code_39', 'qr_code'] })
    navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' } }).then(s => {
      stream = s
      if (video.current) { video.current.srcObject = s; video.current.play().catch(() => {}) }
      const tick = async () => {
        if (stop) return
        try {
          if (video.current && video.current.readyState >= 2) {
            const codes = await det.detect(video.current)
            if (codes.length > 0 && codes[0].rawValue) { stop = true; onCode(codes[0].rawValue); return }
          }
        } catch {}
        setTimeout(tick, 250)
      }
      tick()
    }).catch(e => { setErr((e as Error).message || 'Camera not available'); setActive(false) })
    return () => { stop = true; stream?.getTracks().forEach(t => t.stop()) }
  }, [active, supported, onCode])
  return (
    <div className="grid" style={{ gap: 12 }}>
      {active && <video ref={video} className="scan-video" muted playsInline />}
      {!supported && <p className="muted small">This browser cannot read barcodes from the camera (Chrome and Android do). Type the number from the packaging instead.</p>}
      {err && <div className="callout warn">{err}</div>}
      <form className="row" onSubmit={e => { e.preventDefault(); if (manual.trim()) onCode(manual.trim()) }}>
        <input value={manual} onChange={e => setManual(e.target.value)} placeholder="Barcode number" inputMode="numeric" autoFocus={!active} />
        <button className="btn primary">Look up</button>
      </form>
    </div>
  )
}
