import Foundation

public enum SessionStatus: String, Codable, Sendable {
    case working, waiting, idle, error
}

public struct SessionEntry: Identifiable, Equatable, Sendable {
    public var id: String { "\(agent)@\(sessionID)" }
    public let agent: String
    public let sessionID: String
    public let status: SessionStatus
    public var title = ""
    public var cwd = ""
    public var pid: Int? = nil
    public let since: Date
    public let updated: Date
    public var detail = ""
    public var topic = ""
    public var stale = false
    /// Projekt-Wurzel und eigener Name (von Board.load gesetzt)
    public var root = ""
    public var customName = ""

    public init(agent: String, sessionID: String, status: SessionStatus, title: String = "", cwd: String = "",
                pid: Int? = nil, since: Date, updated: Date, detail: String = "", stale: Bool = false) {
        self.agent = agent; self.sessionID = sessionID; self.status = status; self.title = title
        self.cwd = cwd; self.pid = pid; self.since = since; self.updated = updated; self.detail = detail; self.stale = stale
    }

    public var displayName: String {
        if !customName.isEmpty { return customName }
        if !title.isEmpty { return title }
        let base = root.isEmpty ? cwd : root
        if !base.isEmpty { return URL(fileURLWithPath: base).lastPathComponent }
        return sessionID
    }
}

private struct Raw: Decodable {
    let v: Int
    let agent: String
    let id: String
    let status: SessionStatus
    let updated: String
    let since: String?
    let cwd: String?
    let title: String?
    let pid: Int?
    let detail: String?
    let topic: String?
}

/// Merkt sich gefundene Projekt-Wurzeln je cwd (das Panel laedt alle 2 s neu).
/// Ohne Fund wird nichts gemerkt: ein spaeteres `git init` soll greifen.
public final class RootCache: @unchecked Sendable {
    private let lock = NSLock()
    private var map: [String: String] = [:]
    public init() {}
    public func get(_ cwd: String, _ find: () -> String?) -> String? {
        lock.lock(); defer { lock.unlock() }
        if let r = map[cwd] { return r }
        guard let r = find() else { return nil }
        map[cwd] = r
        return r
    }
}

public enum Board {
    static let staleAfter: TimeInterval = 24 * 3600
    static let expireAfter: TimeInterval = 7 * 24 * 3600

    public static func defaultDir() -> URL {
        if let d = ProcessInfo.processInfo.environment["AGENTSTATUS_DIR"], !d.isEmpty {
            return URL(fileURLWithPath: d)
        }
        return FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".agentstatus/sessions")
    }

    /// Systemstart ueber sysctl kern.boottime; nil, wenn nicht lesbar.
    public static func bootTime() -> Date? {
        var mib = [CTL_KERN, KERN_BOOTTIME]
        var tv = timeval()
        var size = MemoryLayout<timeval>.stride
        guard sysctl(&mib, 2, &tv, &size, nil, 0) == 0, tv.tv_sec > 0 else { return nil }
        return Date(timeIntervalSince1970: TimeInterval(tv.tv_sec) + TimeInterval(tv.tv_usec) / 1e6)
    }

    static func date(_ s: String) -> Date? {
        let f = ISO8601DateFormatter()
        if let d = f.date(from: s) { return d }
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f.date(from: s)
    }

    static func valid(_ s: String) -> Bool {
        s != "." && s != ".." && (1...128).contains(s.utf8.count)
            && s.utf8.allSatisfy { ($0 >= 48 && $0 <= 57) || ($0 >= 65 && $0 <= 90) || ($0 >= 97 && $0 <= 122) || $0 == 46 || $0 == 95 || $0 == 45 }
    }

    /// Naechster Ordner von `cwd` aufwaerts mit `.git` (Ordner oder Datei), hoechstens bis unterhalb von `home`;
    /// sonst `cwd` selbst.
    public static func projectRoot(cwd: String, home: String = NSHomeDirectory(), exists: (String) -> Bool) -> String {
        findRoot(cwd: cwd, home: home, exists: exists) ?? cwd
    }

    public static func findRoot(cwd: String, home: String = NSHomeDirectory(), exists: (String) -> Bool) -> String? {
        var dir = cwd
        while dir != "/" && dir != home && !dir.isEmpty {
            if exists(dir + "/.git") { return dir }
            dir = (dir as NSString).deletingLastPathComponent
        }
        return nil
    }

    /// Was das Rename-Feld vorbelegt: der eigene Name, sonst der Name der Projekt-Wurzel (nie ein title).
    public static func renamePrefill(customName: String, root: String) -> String {
        customName.isEmpty ? URL(fileURLWithPath: root).lastPathComponent : customName
    }

    public enum RenameChange: Equatable { case none, set(String), reset }

    /// Save ohne Aenderung gegenueber dem Vorbelegten speichert nichts; leer setzt einen eigenen Namen zurueck.
    public static func renameChange(input: String, prefill: String, hasCustom: Bool) -> RenameChange {
        let t = input.trimmingCharacters(in: .whitespacesAndNewlines)
        if t == prefill { return .none }
        if t.isEmpty { return hasCustom ? .reset : .none }
        return .set(t)
    }

    /// Login-Wunsch aus `agentstatus setup` (Defaults-Schluessel launchAtLoginRequested):
    /// nil = nichts zu tun (kein Wunsch oder Zustand schon erreicht), sonst der Zielwert.
    public static func loginRequest(defaultsValue: Bool?, currentlyOn: Bool) -> Bool? {
        guard let want = defaultsValue, want != currentlyOn else { return nil }
        return want
    }

    /// Der LaunchAgent-Rueckfall darf nur fuer eine installierte .app schreiben (nicht fuer `swift run`).
    public static func loginFallbackAllowed(bundlePath: String, home: String = NSHomeDirectory()) -> Bool {
        guard bundlePath.hasSuffix(".app") else { return false }
        return bundlePath.hasPrefix("/Applications/") || bundlePath.hasPrefix(home + "/Applications/")
    }

    /// Sessions, die seit `previous` neu auf waiting stehen. Ohne Vorgaenger (Start) nie.
    public static func transitionsToWaiting(previous: [SessionEntry]?, current: [SessionEntry]) -> [SessionEntry] {
        guard let previous else { return [] }
        let was = Set(previous.filter { $0.status == .waiting && !$0.stale }.map(\.id))
        return current.filter { $0.status == .waiting && !$0.stale && !was.contains($0.id) }
    }

    /// Liest nur; loescht nie. Aufraeumen ist Sache von `agentstatus list`.
    public static func load(dir: URL, now: Date, alive: (Int32) -> Bool, boot: Date?,
                            names: [String: String] = [:], cache: RootCache = RootCache(),
                            exists: (String) -> Bool = { FileManager.default.fileExists(atPath: $0) }) -> [SessionEntry] {
        guard let files = try? FileManager.default.contentsOfDirectory(at: dir, includingPropertiesForKeys: nil) else { return [] }
        var out: [SessionEntry] = []
        for url in files.sorted(by: { $0.lastPathComponent < $1.lastPathComponent }) where url.pathExtension == "json" {
            guard let data = try? Data(contentsOf: url),
                  let r = try? JSONDecoder().decode(Raw.self, from: data),
                  r.v == 1, valid(r.agent), valid(r.id),
                  let updated = date(r.updated) else { continue }
            let age = now.timeIntervalSince(updated)
            let rawPid = r.pid ?? 0
            if rawPid < 0 { continue }
            var pid: Int? = nil
            if rawPid > 0 {
                guard let p32 = Int32(exactly: rawPid) else { continue }
                if !alive(p32) { continue }
                // Geist nach Neustart: der pid kann inzwischen jemand anderem gehoeren
                if let boot, updated < boot { continue }
                pid = rawPid
            }
            var since = updated
            if let rs = r.since {
                guard let d = date(rs) else { continue }
                since = d
            }
            if pid == nil && age > expireAfter { continue }
            let cwd = r.cwd ?? ""
            let root = cwd.isEmpty ? "" : (cache.get(cwd) { findRoot(cwd: cwd, home: NSHomeDirectory(), exists: exists) } ?? cwd)
            var entry = SessionEntry(agent: r.agent, sessionID: r.id, status: r.status, title: r.title ?? "",
                                     cwd: cwd, pid: pid, since: since,
                                     updated: updated, detail: r.detail ?? "",
                                     stale: pid == nil && age > staleAfter)
            entry.topic = r.topic ?? ""
            entry.root = root
            entry.customName = names[root] ?? ""
            out.append(entry)
        }
        func rank(_ e: SessionEntry) -> Int {
            if e.stale { return 4 }
            switch e.status { case .waiting: return 0; case .working: return 1; case .error: return 2; case .idle: return 3 }
        }
        return out.sorted {
            if rank($0) != rank($1) { return rank($0) < rank($1) }
            if $0.since != $1.since { return $0.since < $1.since }
            return $0.id < $1.id
        }
    }

    public static func headline(_ entries: [SessionEntry]) -> String {
        if entries.isEmpty { return "No sessions" }
        let live = entries.filter { !$0.stale }
        let working = live.filter { $0.status == .working }.count
        let waiting = live.filter { $0.status == .waiting }.count
        var parts: [String] = []
        if working > 0 { parts.append(working == 1 ? "1 working" : "\(working) working") }
        if waiting > 0 { parts.append(waiting == 1 ? "1 needs you" : "\(waiting) need you") }
        return parts.isEmpty ? "All quiet" : parts.joined(separator: " · ")
    }

    /// Menueleiste: Zahl der wartenden (nicht veralteten) Sessions und der Tooltip.
    public static func statusBarState(_ entries: [SessionEntry]) -> (waiting: Int, tooltip: String) {
        (entries.filter { $0.status == .waiting && !$0.stale }.count, headline(entries))
    }

    static let menuMax = 15

    /// Eine Zeile je Session (Reihenfolge wie im Panel), hoechstens 15, danach "… and N more".
    public static func menuLines(_ entries: [SessionEntry], now: Date) -> [String] {
        if entries.isEmpty { return ["No sessions"] }
        let amb = ambiguousNames(entries)
        var lines = entries.prefix(menuMax).map { e -> String in
            let mark = e.stale ? "○" : ["working": "↻", "waiting": "!", "idle": "●", "error": "✕"][e.status.rawValue]!
            let agent = e.agent.replacingOccurrences(of: "-code", with: "")
            var name = e.displayName
            if var second = secondLine(e, ambiguous: amb.contains(name)) {
                if second.count > 60 { second = String(second.prefix(59)) + "…" }
                name += " — " + second
            }
            return "\(mark)  \(name) · \(agent) · \(duration(e.since, now: now))"
        }
        if entries.count > menuMax { lines.append("… and \(entries.count - menuMax) more") }
        return lines
    }

    /// Angezeigte Namen, die unter den Sessions mehrfach vorkommen.
    public static func ambiguousNames(_ entries: [SessionEntry]) -> Set<String> {
        var seen = Set<String>(), dup = Set<String>()
        for e in entries where !seen.insert(e.displayName).inserted { dup.insert(e.displayName) }
        return dup
    }

    /// Zweite Zeile: Detail (waiting/error), sonst Thema, sonst "#" + 4 Zeichen der ID bei mehrdeutigem Namen.
    public static func secondLine(_ e: SessionEntry, ambiguous: Bool) -> String? {
        if (e.status == .waiting || e.status == .error) && !e.detail.isEmpty { return e.detail }
        if !e.topic.isEmpty { return e.topic }
        return ambiguous ? "#" + e.sessionID.prefix(4) : nil
    }

    public static func duration(_ since: Date, now: Date) -> String {
        let s = max(0, now.timeIntervalSince(since))
        switch s {
        case ..<60: return "now"
        case ..<3600: return "\(Int(s / 60))m"
        case ..<86400: return "\(Int(s / 3600))h"
        default: return "\(Int(s / 86400))d"
        }
    }
}
