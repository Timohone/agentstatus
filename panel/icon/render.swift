// Rendert das App-Icon als macOS-iconset nach panel/icon/AgentStatus.iconset/.
// Aufruf: swift panel/icon/render.swift [ausgabeordner]   (nur CoreGraphics/AppKit)
import AppKit

// Geometrie in 1024er-Einheiten; kleine Grössen bekommen dickere Zeilen.
func draw(_ px: Int) -> Data {
    let s = CGFloat(px), u = s / 1024
    let small = px <= 32
    let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: px, pixelsHigh: px, bitsPerSample: 8,
        samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
    let ctx = NSGraphicsContext.current!.cgContext
    ctx.setShouldAntialias(true)

    let box = CGRect(x: 100 * u, y: 100 * u, width: 824 * u, height: 824 * u)
    let shape = CGPath(roundedRect: box, cornerWidth: 185 * u, cornerHeight: 185 * u, transform: nil)

    // Schatten wie bei Systemicons (bei 16/32 px weglassen, sonst Matsch)
    if px >= 64 {
        ctx.saveGState()
        ctx.setShadow(offset: CGSize(width: 0, height: -10 * u), blur: 24 * u, color: NSColor(white: 0, alpha: 0.35).cgColor)
        ctx.setFillColor(NSColor(srgbRed: 0.08, green: 0.09, blue: 0.11, alpha: 1).cgColor)
        ctx.addPath(shape); ctx.fillPath()
        ctx.restoreGState()
    }
    // Verlauf
    ctx.saveGState()
    ctx.addPath(shape); ctx.clip()
    let grad = CGGradient(colorsSpace: CGColorSpaceCreateDeviceRGB(), colors: [
        NSColor(srgbRed: 0x2B/255, green: 0x2F/255, blue: 0x36/255, alpha: 1).cgColor,
        NSColor(srgbRed: 0x15/255, green: 0x17/255, blue: 0x1B/255, alpha: 1).cgColor] as CFArray, locations: [0, 1])!
    ctx.drawLinearGradient(grad, start: CGPoint(x: 0, y: box.maxY), end: CGPoint(x: 0, y: box.minY), options: [])
    ctx.restoreGState()
    // feiner Rand (innen, oben heller)
    if px >= 64 {
        ctx.saveGState()
        ctx.addPath(shape); ctx.clip()
        ctx.setStrokeColor(NSColor(white: 1, alpha: 0.12).cgColor)
        ctx.setLineWidth(3 * u * 2)
        ctx.addPath(shape); ctx.strokePath()
        ctx.restoreGState()
    }

    // Zeilen: von oben Gelb (needs you), Blau (arbeitet), Grün (fertig)
    let colors: [(CGFloat, CGFloat, CGFloat)] = [(0xFF, 0xC4, 0x00), (0x0A, 0x84, 0xFF), (0x30, 0xD1, 0x58)]
    let h: CGFloat = small ? 170 : 124          // Zeilen-/Punkthöhe
    let pitch: CGFloat = small ? 232 : 212
    let left: CGFloat = small ? 175 : 200, right: CGFloat = small ? 849 : 824
    let gap: CGFloat = small ? 34 : 40
    for (i, c) in colors.enumerated() {
        let cy = (512 + CGFloat(1 - i) * pitch) * u
        let dot = CGRect(x: left * u, y: cy - h / 2 * u, width: h * u, height: h * u)
        ctx.setFillColor(NSColor(srgbRed: c.0/255, green: c.1/255, blue: c.2/255, alpha: 1).cgColor)
        ctx.fillEllipse(in: dot)
        let bar = CGRect(x: dot.maxX + gap * u, y: dot.minY, width: right * u - dot.maxX - gap * u, height: h * u)
        ctx.setFillColor(NSColor(white: 1, alpha: small ? 0.55 : 0.42).cgColor)
        ctx.addPath(CGPath(roundedRect: bar, cornerWidth: h / 2 * u, cornerHeight: h / 2 * u, transform: nil))
        ctx.fillPath()
    }
    NSGraphicsContext.restoreGraphicsState()
    return rep.representation(using: .png, properties: [:])!
}

let dir = CommandLine.arguments.count > 1 ? CommandLine.arguments[1]
    : URL(fileURLWithPath: #filePath).deletingLastPathComponent().appendingPathComponent("AgentStatus.iconset").path
try? FileManager.default.removeItem(atPath: dir)
try! FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
for base in [16, 32, 128, 256, 512] {
    try! draw(base).write(to: URL(fileURLWithPath: "\(dir)/icon_\(base)x\(base).png"))
    try! draw(base * 2).write(to: URL(fileURLWithPath: "\(dir)/icon_\(base)x\(base)@2x.png"))
}
