import AppKit
import Combine
import AgentStatusCore
import Darwin
import SwiftUI

@MainActor
final class Store: ObservableObject {
    @Published var entries: [SessionEntry] = []
    @Published var now = Date()
    private let dir = Board.defaultDir()
    private let cache = RootCache()
    private var previous: [SessionEntry]? = nil
    private var source: DispatchSourceFileSystemObject?
    private var timer: Timer?

    init() {
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true,
                                                 attributes: [.posixPermissions: 0o700])
        reload()
        watch()
        // Prozess-Check und Dauer-Anzeige; Datei-Ereignisse decken den Rest sofort ab.
        // Der Timer ist auch der Rueckfall der Ordner-Wache: ohne Quelle wird sie hier neu versucht.
        timer = Timer.scheduledTimer(withTimeInterval: 2, repeats: true) { [weak self] _ in
            Task { @MainActor in
                guard let self else { return }
                if self.source == nil { self.rewatch() }
                self.reload()
            }
        }
    }

    func reload() {
        now = Date()
        entries = Board.load(dir: dir, now: now, alive: { pid in kill(pid, 0) == 0 || errno == EPERM }, boot: Board.bootTime(),
                             names: Prefs.names, cache: cache)
        if Prefs.notify { Notifier.notify(Board.transitionsToWaiting(previous: previous, current: entries)) }
        previous = entries
    }

    private func rewatch() {
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true,
                                                 attributes: [.posixPermissions: 0o700])
        watch()
    }

    private func watch() {
        let fd = open(dir.path, O_EVTONLY)
        guard fd >= 0 else { return }
        let s = DispatchSource.makeFileSystemObjectSource(fileDescriptor: fd, eventMask: [.write, .rename, .delete], queue: .main)
        s.setEventHandler { [weak self, weak s] in
            let lost = s.map { !DispatchSource.FileSystemEvent(rawValue: $0.data).isDisjoint(with: [.delete, .rename]) } ?? false
            Task { @MainActor in
                guard let self else { return }
                if lost {
                    self.source?.cancel()
                    self.source = nil
                    self.rewatch()
                }
                self.reload()
            }
        }
        s.setCancelHandler { close(fd) }
        s.resume()
        source = s
    }
}

struct Symbol: View {
    let entry: SessionEntry
    @State private var pulse = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        Group {
            if entry.stale {
                Image(systemName: "circle.dotted").foregroundStyle(.secondary)
            } else {
                switch entry.status {
                case .working: Image(systemName: "arrow.triangle.2.circlepath").foregroundStyle(.blue)
                case .waiting: Image(systemName: "exclamationmark.circle.fill").foregroundStyle(.yellow)
                        .opacity(pulse ? 0.35 : 1)
                        .onAppear { if !reduceMotion { withAnimation(.easeInOut(duration: 0.9).repeatForever()) { pulse = true } } }
                        .onDisappear { pulse = false }
                case .idle: Image(systemName: "circle.fill").foregroundStyle(.green)
                case .error: Image(systemName: "xmark.circle.fill").foregroundStyle(.red)
                }
            }
        }
        .frame(width: 16)
        .accessibilityLabel(entry.stale ? "stale" : entry.status.rawValue)
    }
}

struct SettingsMenu: View {
    @ObservedObject var store: Store
    var body: some View {
        Toggle("Launch at Login", isOn: Binding(get: { LoginItem.isOn }, set: { _ in SettingsActions.toggleLogin(); store.reload() }))
        Toggle("Notify When a Session Needs You", isOn: Binding(get: { Prefs.notify }, set: { _ in
            SettingsActions.toggleNotify()
            store.reload()
        }))
        Divider()
        Button("About AgentStatus") { SettingsActions.about() }
        Button("Quit") { NSApp.terminate(nil) }
    }
}

struct Row: View {
    let entry: SessionEntry
    let now: Date
    let ambiguous: Bool
    @ObservedObject var store: Store

    var body: some View {
        HStack(spacing: 8) {
            Symbol(entry: entry).id(entry.status)
            VStack(alignment: .leading, spacing: 1) {
                Text(entry.displayName).lineLimit(1).truncationMode(.tail)
                if let second = Board.secondLine(entry, ambiguous: ambiguous) {
                    Text(second).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                }
            }
            Spacer(minLength: 4)
            Text(entry.agent.replacingOccurrences(of: "-code", with: "")).font(.caption2).foregroundStyle(.secondary)
            Text(Board.duration(entry.since, now: now)).font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                .frame(minWidth: 28, alignment: .trailing)
        }
        .opacity(entry.stale ? 0.5 : 1)
        .help(entry.cwd)
        .contextMenu {
            if !entry.root.isEmpty {
                Button("Rename…") { Rename.ask(root: entry.root, customName: entry.customName); store.reload() }
                Divider()
            }
            SettingsMenu(store: store)
        }
    }
}

struct PanelView: View {
    @ObservedObject var store: Store

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("SESSIONS").font(.caption.bold()).foregroundStyle(.orange)
            Text(Board.headline(store.entries)).font(.callout.bold())
            if store.entries.isEmpty {
                Text("agentstatus install claude").font(.caption.monospaced()).foregroundStyle(.secondary)
            } else {
                Divider()
                let amb = Board.ambiguousNames(store.entries)
                ForEach(store.entries) { Row(entry: $0, now: store.now, ambiguous: amb.contains($0.displayName), store: store) }
            }
        }
        .padding(12)
        .frame(width: 260, alignment: .leading)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 12))
        .contextMenu { SettingsMenu(store: store) }
    }
}

final class FloatingPanel: NSPanel {
    override var canBecomeKey: Bool { false }
    override var canBecomeMain: Bool { false }
}

final class Host: NSHostingView<PanelView> {
    var onLayout: (() -> Void)?
    override func layout() {
        super.layout()
        onLayout?()
    }
}

@MainActor
final class StatusBar: NSObject, NSMenuDelegate {
    private let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
    private let store: Store
    private let togglePanel: () -> Void
    private var sub: AnyCancellable?

    init(store: Store, togglePanel: @escaping () -> Void) {
        self.store = store
        self.togglePanel = togglePanel
        super.init()
        let menu = NSMenu()
        menu.delegate = self
        item.menu = menu
        update(store.entries)
        // gleiche Quelle wie das Panel: jeder Store-Reload
        sub = store.$entries.sink { [weak self] in self?.update($0) }
    }

    private func update(_ entries: [SessionEntry]) {
        let s = Board.statusBarState(entries)
        guard let b = item.button else { return }
        b.toolTip = s.tooltip
        if s.waiting > 0 {
            let img = NSImage(systemSymbolName: "exclamationmark.circle.fill", accessibilityDescription: "Needs input")?
                .withSymbolConfiguration(.init(paletteColors: [.systemYellow]))
            img?.isTemplate = false
            b.image = img
            b.title = " \(s.waiting)"
        } else {
            let img = NSImage(systemSymbolName: "circle.grid.2x1", accessibilityDescription: "AgentStatus")
            img?.isTemplate = true
            b.image = img
            b.title = ""
        }
    }

    func menuNeedsUpdate(_ menu: NSMenu) {
        menu.removeAllItems()
        for line in Board.menuLines(store.entries, now: Date()) {
            let m = NSMenuItem(title: line, action: nil, keyEquivalent: "")
            m.isEnabled = false
            menu.addItem(m)
        }
        menu.addItem(.separator())
        menu.addItem(action(Prefs.panelHidden ? "Show Panel" : "Hide Panel", #selector(panelClicked)))
        menu.addItem(.separator())
        let login = action("Launch at Login", #selector(loginClicked))
        login.state = LoginItem.isOn ? .on : .off
        menu.addItem(login)
        let notify = action("Notify When a Session Needs You", #selector(notifyClicked))
        notify.state = Prefs.notify ? .on : .off
        menu.addItem(notify)
        menu.addItem(.separator())
        menu.addItem(action("About AgentStatus", #selector(aboutClicked)))
        menu.addItem(action("Quit", #selector(quitClicked)))
    }

    private func action(_ title: String, _ sel: Selector) -> NSMenuItem {
        let m = NSMenuItem(title: title, action: sel, keyEquivalent: "")
        m.target = self
        return m
    }

    @objc private func panelClicked() { togglePanel() }
    @objc private func loginClicked() { SettingsActions.toggleLogin(); store.reload() }
    @objc private func notifyClicked() { SettingsActions.toggleNotify(); store.reload() }
    @objc private func aboutClicked() { SettingsActions.about() }
    @objc private func quitClicked() { NSApp.terminate(nil) }
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    var statusBar: StatusBar!
    var panel: FloatingPanel!
    let store = Store()
    var host: Host!

    /// Passt die Hoehe an den Inhalt an; die obere linke Ecke bleibt stehen.
    func fit() {
        host.layoutSubtreeIfNeeded()
        let size = host.fittingSize
        guard size.width > 0, size.height > 0 else { return }
        let old = panel.frame
        guard abs(old.height - size.height) > 0.5 || abs(old.width - size.width) > 0.5 else { return }
        panel.setFrame(NSRect(x: old.minX, y: old.maxY - size.height, width: size.width, height: size.height), display: true)
    }

    /// `open -a AgentStatus` auf eine laufende App (setup --remove): Login-Wunsch jetzt anwenden.
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        LoginItem.applyRequest()
        return true
    }

    func applicationDidFinishLaunching(_ note: Notification) {
        LoginItem.applyRequest()
        panel = FloatingPanel(contentRect: NSRect(x: 0, y: 0, width: 260, height: 120),
                              styleMask: [.nonactivatingPanel, .borderless], backing: .buffered, defer: false)
        panel.level = .floating
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .stationary]
        panel.isMovableByWindowBackground = true
        panel.backgroundColor = .clear
        panel.hasShadow = true
        panel.hidesOnDeactivate = false
        host = Host(rootView: PanelView(store: store))
        host.onLayout = { [weak self] in DispatchQueue.main.async { MainActor.assumeIsolated { self?.fit() } } }
        host.sizingOptions = [.intrinsicContentSize]
        panel.contentView = host
        if panel.setFrameUsingName("AgentStatusPanel") {
            // nur die gespeicherte obere linke Ecke gilt, nicht die alte Hoehe
            panel.setFrameTopLeftPoint(NSPoint(x: panel.frame.minX, y: panel.frame.maxY))
        } else if let screen = NSScreen.main {
            let f = screen.visibleFrame
            panel.setFrameTopLeftPoint(NSPoint(x: f.maxX - 280, y: f.maxY - 20))
        }
        fit()
        panel.setFrameAutosaveName("AgentStatusPanel")
        if !Prefs.panelHidden { panel.orderFrontRegardless() }
        statusBar = StatusBar(store: store) { [weak self] in
            guard let self else { return }
            Prefs.panelHidden.toggle()
            if Prefs.panelHidden { self.panel.orderOut(nil) } else { self.panel.orderFrontRegardless() }
        }
        if Prefs.notify { Notifier.requestPermission() }
    }
}

let app = NSApplication.shared
app.setActivationPolicy(.accessory) // kein Dock-Symbol, auch ohne Bundle
let delegate = AppDelegate()
app.delegate = delegate
app.run()
