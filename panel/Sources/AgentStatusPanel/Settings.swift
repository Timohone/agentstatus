import AppKit
import AgentStatusCore
import ServiceManagement
import UserNotifications

/// Eigene Namen und Schalter, gespeichert in den UserDefaults der App.
enum Prefs {
    static let namesKey = "projectNames"   // [Pfad der Projekt-Wurzel: Name]
    static let notifyKey = "notifyOnWaiting"

    static let panelHiddenKey = "panelHidden"

    static var panelHidden: Bool {
        get { UserDefaults.standard.bool(forKey: panelHiddenKey) }
        set { UserDefaults.standard.set(newValue, forKey: panelHiddenKey) }
    }
    static var names: [String: String] {
        get { UserDefaults.standard.dictionary(forKey: namesKey) as? [String: String] ?? [:] }
        set { UserDefaults.standard.set(newValue, forKey: namesKey) }
    }
    static var notify: Bool {
        get { UserDefaults.standard.object(forKey: notifyKey) as? Bool ?? true }
        set { UserDefaults.standard.set(newValue, forKey: notifyKey) }
    }
}

@MainActor
enum Rename {
    /// Kleines Eingabefeld; die App wird nur dafuer kurz aktiv. Leer oder Reset = zuruecksetzen.
    static func ask(root: String, customName: String) {
        let hasCustom = !customName.isEmpty
        let prefill = Board.renamePrefill(customName: customName, root: root)
        let alert = NSAlert()
        alert.messageText = "Rename project"
        alert.informativeText = root
        let field = NSTextField(frame: NSRect(x: 0, y: 0, width: 240, height: 24))
        field.stringValue = prefill
        alert.accessoryView = field
        alert.window.initialFirstResponder = field
        alert.addButton(withTitle: "Save")
        alert.addButton(withTitle: "Cancel")
        if hasCustom { alert.addButton(withTitle: "Reset") }
        NSApp.activate()
        let r = alert.runModal()
        NSApp.deactivate()
        var names = Prefs.names
        switch r {
        case .alertFirstButtonReturn:
            switch Board.renameChange(input: field.stringValue, prefill: prefill, hasCustom: hasCustom) {
            case .none: return
            case .set(let n): names[root] = n
            case .reset: names[root] = nil
            }
        case .alertThirdButtonReturn: names[root] = nil
        default: return
        }
        Prefs.names = names
    }
}

/// Dieselben Aktionen fuer Panel-Kontextmenue und Menueleiste.
@MainActor
enum SettingsActions {
    static func about() {
        NSApp.activate()
        NSApp.orderFrontStandardAboutPanel(nil)
    }
    static func toggleLogin() { LoginItem.set(!LoginItem.isOn) }
    static func toggleNotify() {
        Prefs.notify.toggle()
        if Prefs.notify { Notifier.requestPermission() }
    }
}

@MainActor
enum LoginItem {
    static let requestKey = "launchAtLoginRequested"

    /// Wunsch von `agentstatus setup` (`defaults write com.timohone.agentstatus …`) einmal umsetzen und Schluessel loeschen.
    static func applyRequest(defaults: UserDefaults = .standard) {
        let requested = defaults.object(forKey: requestKey) as? Bool
        if let want = Board.loginRequest(defaultsValue: requested, currentlyOn: isOn) { set(want) }
        defaults.removeObject(forKey: requestKey)
    }

    static let plist = FileManager.default.homeDirectoryForCurrentUser
        .appendingPathComponent("Library/LaunchAgents/com.timohone.agentstatus.plist")

    static var isOn: Bool {
        SMAppService.mainApp.status == .enabled || FileManager.default.fileExists(atPath: plist.path)
    }

    static func set(_ on: Bool) {
        if !on {
            try? SMAppService.mainApp.unregister()
            try? FileManager.default.removeItem(at: plist)
            return
        }
        do {
            try SMAppService.mainApp.register()
            if SMAppService.mainApp.status == .enabled { return }
        } catch {}
        // Rueckfall: LaunchAgent (z. B. ad-hoc signiert), aber nur fuer eine installierte .app
        guard Board.loginFallbackAllowed(bundlePath: Bundle.main.bundlePath) else {
            let a = NSAlert()
            a.messageText = "Launch at Login unavailable"
            a.informativeText = "Move AgentStatus.app to /Applications first, then turn on Launch at Login."
            NSApp.activate()
            a.runModal()
            NSApp.deactivate()
            return
        }
        try? SMAppService.mainApp.unregister()
        let dict: [String: Any] = ["Label": "com.timohone.agentstatus", "RunAtLoad": true,
                                   "ProgramArguments": [Bundle.main.executablePath ?? CommandLine.arguments[0]]]
        try? FileManager.default.createDirectory(at: plist.deletingLastPathComponent(), withIntermediateDirectories: true)
        _ = (dict as NSDictionary).write(to: plist, atomically: true)
        let a = NSAlert()
        a.messageText = "Launch at Login uses a LaunchAgent"
        a.informativeText = "The system login item could not be registered, so AgentStatus starts via ~/Library/LaunchAgents/com.timohone.agentstatus.plist."
        NSApp.activate()
        a.runModal()
        NSApp.deactivate()
    }
}

@MainActor
enum Notifier {
    // ohne App-Bundle (swift run) gibt es kein UNUserNotificationCenter: dann nur Ton
    static var center: UNUserNotificationCenter? { Bundle.main.bundleIdentifier == nil ? nil : .current() }

    static func requestPermission() {
        center?.requestAuthorization(options: [.alert]) { _, _ in }
    }

    static func notify(_ entries: [SessionEntry]) {
        for e in entries {
            NSSound(named: "Glass")?.play()
            guard let center else { continue }
            let content = UNMutableNotificationContent()
            content.title = "\(e.displayName) needs you"
            content.body = e.detail
            let req = UNNotificationRequest(identifier: "waiting-\(e.id)-\(Int(e.since.timeIntervalSince1970))", content: content, trigger: nil)
            center.add(req)  // ohne Berechtigung verwirft das System still; der Ton kam schon
        }
    }
}
