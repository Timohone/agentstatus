import Foundation
import Testing
@testable import AgentStatusCore

let t0 = ISO8601DateFormatter().date(from: "2026-10-06T18:00:00Z")!

func makeDir() -> URL {
    let d = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try! FileManager.default.createDirectory(at: d, withIntermediateDirectories: true)
    return d
}

func put(_ dir: URL, _ name: String, _ body: String) {
    try! body.write(to: dir.appendingPathComponent(name), atomically: true, encoding: .utf8)
}

func session(_ id: String, _ status: String, updated: String = "2026-10-06T18:00:00Z", since: String? = nil, pid: Int? = nil, title: String? = nil, cwd: String? = nil) -> String {
    var s = #"{"v":1,"agent":"claude-code","id":"\#(id)","status":"\#(status)","updated":"\#(updated)""#
    if let since { s += #","since":"\#(since)""# }
    if let pid { s += #","pid":\#(pid)"# }
    if let title { s += #","title":"\#(title)""# }
    if let cwd { s += #","cwd":"\#(cwd)""# }
    return s + "}"
}

@Test func skipsGarbage() {
    let d = makeDir()
    put(d, "a.json", session("gut", "idle"))
    put(d, "b.json", #"{"v":1,"agent":"#)
    put(d, "c.json", session("x", "busy"))
    put(d, "d.json", #"{"v":2,"agent":"a","id":"b","status":"idle","updated":"2026-10-06T18:00:00Z"}"#)
    put(d, "e.txt", "hallo")
    let got = Board.load(dir: d, now: t0, alive: { _ in true }, boot: nil)
    #expect(got.map(\.id) == ["claude-code@gut"])
    #expect(FileManager.default.fileExists(atPath: d.appendingPathComponent("b.json").path))
}

@Test func missingDirIsEmpty() {
    let got = Board.load(dir: URL(fileURLWithPath: "/gibt/es/nicht"), now: t0, alive: { _ in true }, boot: nil)
    #expect(got.isEmpty)
}

@Test func sortsAndHidesDead() {
    let d = makeDir()
    put(d, "1.json", session("idle", "idle", since: "2026-10-06T17:00:00Z"))
    put(d, "2.json", session("work", "working", since: "2026-10-06T17:50:00Z"))
    put(d, "3.json", session("wait", "waiting"))
    put(d, "4.json", session("err", "error"))
    put(d, "5.json", session("tot", "working", pid: 111))
    put(d, "6.json", session("alt", "waiting", updated: "2026-10-05T10:00:00Z"))
    put(d, "7.json", session("uralt", "idle", updated: "2026-09-20T10:00:00Z"))
    let got = Board.load(dir: d, now: t0, alive: { $0 != 111 }, boot: nil)
    #expect(got.map(\.id) == ["claude-code@wait", "claude-code@work", "claude-code@err", "claude-code@idle", "claude-code@alt"])
    #expect(got.last?.stale == true)
}

@Test func displayNameFallsBackToFolder() {
    let d = makeDir()
    put(d, "a.json", session("a", "idle", cwd: "/Users/x/repos/myproject"))
    put(d, "b.json", session("b", "idle", title: "Feinschliff", cwd: "/Users/x/repos/myproject"))
    put(d, "c.json", session("c", "idle"))
    let names = Board.load(dir: d, now: t0, alive: { _ in true }, boot: nil).map(\.displayName).sorted()
    #expect(names == ["Feinschliff", "c", "myproject"])
}

@Test func headline() {
    func e(_ s: SessionStatus) -> SessionEntry {
        SessionEntry(agent: "a", sessionID: UUID().uuidString, status: s, since: t0, updated: t0)
    }
    #expect(Board.headline([]) == "No sessions")
    #expect(Board.headline([e(.idle)]) == "All quiet")
    #expect(Board.headline([e(.working), e(.working), e(.waiting), e(.idle)]) == "2 working · 1 needs you")
    #expect(Board.headline([e(.working)]) == "1 working")
    #expect(Board.headline([e(.waiting), e(.waiting)]) == "2 need you")
}

@Test func duration() {
    #expect(Board.duration(t0, now: t0.addingTimeInterval(30)) == "now")
    #expect(Board.duration(t0, now: t0.addingTimeInterval(5 * 60)) == "5m")
    #expect(Board.duration(t0, now: t0.addingTimeInterval(3 * 3600)) == "3h")
    #expect(Board.duration(t0, now: t0.addingTimeInterval(2 * 86400)) == "2d")
    #expect(Board.duration(t0, now: t0.addingTimeInterval(-60)) == "now")
}

@Test func acceptsFractionalSeconds() {
    let d = makeDir()
    put(d, "a.json", session("frac", "idle", updated: "2026-10-06T18:00:00.123456789Z"))
    #expect(Board.load(dir: d, now: t0, alive: { _ in true }, boot: nil).map(\.id) == ["claude-code@frac"])
}

@Test func hugePidIsSkippedWithoutCrash() {
    let d = makeDir()
    put(d, "a.json", session("riesig", "idle", pid: 99999999999))
    put(d, "b.json", session("ok", "idle"))
    #expect(Board.load(dir: d, now: t0, alive: { _ in true }, boot: nil).map(\.id) == ["claude-code@ok"])
}

@Test func negativeAndZeroPid() {
    let d = makeDir()
    put(d, "a.json", session("neg", "idle", pid: -5))
    put(d, "b.json", session("null", "idle", updated: "2026-10-05T10:00:00Z", pid: 0))
    let got = Board.load(dir: d, now: t0, alive: { _ in false }, boot: nil)
    #expect(got.map(\.id) == ["claude-code@null"])
    #expect(got[0].stale == true)
    #expect(got[0].pid == nil)
}

@Test func orderIsStable() {
    let d = makeDir()
    for id in ["z", "m", "a", "k"] { put(d, "\(id).json", session(id, "working", since: "2026-10-06T17:00:00Z")) }
    for _ in 0..<20 {
        #expect(Board.load(dir: d, now: t0, alive: { _ in true }, boot: nil).map(\.id) == ["claude-code@a", "claude-code@k", "claude-code@m", "claude-code@z"])
    }
}

@Test func tieBreakOlderSinceFirst() {
    let d = makeDir()
    put(d, "a.json", session("neu", "working", since: "2026-10-06T17:50:00Z"))
    put(d, "b.json", session("alt", "working", since: "2026-10-06T17:00:00Z"))
    #expect(Board.load(dir: d, now: t0, alive: { _ in true }, boot: nil).map(\.id) == ["claude-code@alt", "claude-code@neu"])
}

@Test func badSinceIsInvalid() {
    let d = makeDir()
    put(d, "a.json", session("kaputt", "idle", since: "gestern"))
    #expect(Board.load(dir: d, now: t0, alive: { _ in true }, boot: nil).isEmpty)
}

@Test func invalidNamesAndMissingUpdated() {
    let d = makeDir()
    put(d, "1.json", #"{"v":1,"agent":"a/b","id":"x","status":"idle","updated":"2026-10-06T18:00:00Z"}"#)
    put(d, "2.json", #"{"v":1,"agent":"a","id":"..","status":"idle","updated":"2026-10-06T18:00:00Z"}"#)
    put(d, "3.json", #"{"v":1,"agent":"","id":"x","status":"idle","updated":"2026-10-06T18:00:00Z"}"#)
    put(d, "4.json", #"{"v":1,"agent":"a","id":"\#(String(repeating: "x", count: 129))","status":"idle","updated":"2026-10-06T18:00:00Z"}"#)
    put(d, "5.json", #"{"v":1,"agent":"a","id":"x","status":"idle"}"#)
    #expect(Board.load(dir: d, now: t0, alive: { _ in true }, boot: nil).isEmpty)
}

@Test func oldWithLivePidIsShownNotStale() {
    let d = makeDir()
    put(d, "a.json", session("lebt", "working", updated: "2026-09-28T18:00:00Z", pid: 42))
    let got = Board.load(dir: d, now: t0, alive: { _ in true }, boot: nil)
    #expect(got.count == 1 && got[0].stale == false)
}

@Test func sinceFallsBackToUpdated() {
    let d = makeDir()
    put(d, "a.json", session("a", "idle", updated: "2026-10-06T17:30:00Z"))
    let got = Board.load(dir: d, now: t0, alive: { _ in true }, boot: nil)
    #expect(got[0].since == got[0].updated)
}

@Test func ghostAfterBootIsHidden() {
    let d = makeDir()
    put(d, "a.json", session("geist", "working", updated: "2026-10-06T16:00:00Z", pid: 42))
    put(d, "b.json", session("neu", "working", updated: "2026-10-06T17:30:00Z", pid: 43))
    put(d, "c.json", session("ohnepid", "idle", updated: "2026-10-06T16:00:00Z"))
    let boot = t0.addingTimeInterval(-3600)
    #expect(Board.load(dir: d, now: t0, alive: { _ in true }, boot: boot).map(\.id) == ["claude-code@neu", "claude-code@ohnepid"])
    #expect(Board.load(dir: d, now: t0, alive: { _ in true }, boot: nil).count == 3)
}

@Test func bootTimeIsPlausible() {
    let b = Board.bootTime()
    #expect(b != nil && b! < Date())
}

@Test func idsDoNotCollide() {
    func e(_ a: String, _ s: String) -> SessionEntry { SessionEntry(agent: a, sessionID: s, status: .idle, since: t0, updated: t0) }
    #expect(e("a-b", "c").id != e("a", "b-c").id)
}

// MARK: Phase 2

@Test func projectRootWalksUpToGit() {
    let git: Set<String> = ["/Users/u/code/app/.git"]
    let ex: (String) -> Bool = { git.contains($0) }
    #expect(Board.projectRoot(cwd: "/Users/u/code/app/src/deep", home: "/Users/u", exists: ex) == "/Users/u/code/app")
    #expect(Board.projectRoot(cwd: "/Users/u/code/app", home: "/Users/u", exists: ex) == "/Users/u/code/app")
}

@Test func projectRootAcceptsWorktreeFileAndFallsBack() {
    // exists() sagt nichts ueber Datei oder Ordner: eine .git-Datei zaehlt wie ein Ordner
    let ex: (String) -> Bool = { $0 == "/Users/u/wt/feature/.git" }
    #expect(Board.projectRoot(cwd: "/Users/u/wt/feature/pkg", home: "/Users/u", exists: ex) == "/Users/u/wt/feature")
    #expect(Board.projectRoot(cwd: "/Users/u/loose/dir", home: "/Users/u", exists: { _ in false }) == "/Users/u/loose/dir")
}

@Test func projectRootStopsAtHome() {
    let ex: (String) -> Bool = { $0 == "/Users/u/.git" || $0 == "/.git" }
    #expect(Board.projectRoot(cwd: "/Users/u/notes/x", home: "/Users/u", exists: ex) == "/Users/u/notes/x")
    #expect(Board.projectRoot(cwd: "/tmp/a/b", home: "/Users/u", exists: ex) == "/tmp/a/b")
}

@Test func displayNameOrder() {
    let d = makeDir()
    put(d, "a.json", session("s1", "idle", title: "T", cwd: "/tmp/zz/proj/sub"))
    put(d, "b.json", session("s2", "idle", cwd: "/tmp/zz/proj/sub"))
    put(d, "c.json", session("s3", "idle"))
    let ex: (String) -> Bool = { $0 == "/tmp/zz/proj/.git" }
    func names(_ n: [String: String]) -> [String: String] {
        Dictionary(uniqueKeysWithValues: Board.load(dir: d, now: t0, alive: { _ in true }, boot: nil, names: n, exists: ex)
            .map { ($0.sessionID, $0.displayName) })
    }
    #expect(names([:]) == ["s1": "T", "s2": "proj", "s3": "s3"])
    #expect(names(["/tmp/zz/proj": "Mine"]) == ["s1": "Mine", "s2": "Mine", "s3": "s3"])
}

@Test func transitionsToWaitingCases() {
    func e(_ id: String, _ s: SessionStatus) -> SessionEntry { SessionEntry(agent: "a", sessionID: id, status: s, since: t0, updated: t0) }
    #expect(Board.transitionsToWaiting(previous: nil, current: [e("x", .waiting)]).isEmpty)
    #expect(Board.transitionsToWaiting(previous: [e("x", .working)], current: [e("x", .waiting)]).map(\.sessionID) == ["x"])
    #expect(Board.transitionsToWaiting(previous: [], current: [e("n", .waiting)]).map(\.sessionID) == ["n"])
    #expect(Board.transitionsToWaiting(previous: [e("x", .waiting)], current: [e("x", .waiting)]).isEmpty)
    let back = Board.transitionsToWaiting(previous: [e("x", .idle)], current: [e("x", .waiting)])
    #expect(back.count == 1)
    var stale = e("y", .waiting); stale.stale = true
    #expect(Board.transitionsToWaiting(previous: [], current: [stale]).isEmpty)
}

@Test func renamePrefillUsesCustomThenRootNeverTitle() {
    #expect(Board.renamePrefill(customName: "Mine", root: "/u/code/app") == "Mine")
    #expect(Board.renamePrefill(customName: "", root: "/u/code/app") == "app")
}

@Test func renameChangeCases() {
    #expect(Board.renameChange(input: "app", prefill: "app", hasCustom: false) == .none)
    #expect(Board.renameChange(input: " Mine ", prefill: "Mine", hasCustom: true) == .none)
    #expect(Board.renameChange(input: "New", prefill: "app", hasCustom: false) == .set("New"))
    #expect(Board.renameChange(input: "  ", prefill: "Mine", hasCustom: true) == .reset)
    #expect(Board.renameChange(input: "", prefill: "app", hasCustom: false) == .none)
}

@Test func rootCacheKeepsOnlyHits() {
    let cache = RootCache()
    var calls = 0
    #expect(cache.get("/a") { calls += 1; return "/a" } == "/a")
    #expect(cache.get("/a") { calls += 1; return "/x" } == "/a")
    #expect(calls == 1)
    #expect(cache.get("/b") { calls += 1; return nil } == nil)
    #expect(cache.get("/b") { calls += 1; return nil } == nil)
    #expect(calls == 3)
}

@Test func loadSearchesAgainWithoutGit() throws {
    let dir = FileManager.default.temporaryDirectory.appendingPathComponent("as-\(UUID().uuidString)")
    try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: dir) }
    let json = #"{"v":1,"agent":"x","id":"s","status":"idle","updated":"2026-10-06T10:00:00Z","cwd":"/zz/proj/sub"}"#
    try json.write(to: dir.appendingPathComponent("x@s.json"), atomically: true, encoding: .utf8)
    let cache = RootCache()
    var git = false
    let now = ISO8601DateFormatter().date(from: "2026-10-06T10:01:00Z")!
    func run() -> String {
        Board.load(dir: dir, now: now, alive: { _ in true }, boot: nil, cache: cache,
                   exists: { git && $0 == "/zz/proj/.git" }).first!.root
    }
    #expect(run() == "/zz/proj/sub")
    git = true
    #expect(run() == "/zz/proj")
}

@Test func loginFallbackOnlyForInstalledApp() {
    #expect(Board.loginFallbackAllowed(bundlePath: "/Applications/AgentStatus.app", home: "/Users/u"))
    #expect(Board.loginFallbackAllowed(bundlePath: "/Users/u/Applications/AgentStatus.app", home: "/Users/u"))
    #expect(!Board.loginFallbackAllowed(bundlePath: "/Users/u/code/panel/.build/debug", home: "/Users/u"))
    #expect(!Board.loginFallbackAllowed(bundlePath: "/Users/u/Downloads/AgentStatus.app", home: "/Users/u"))
    #expect(!Board.loginFallbackAllowed(bundlePath: "/Applications/tool", home: "/Users/u"))
}

private func entry(_ id: String, _ status: SessionStatus, stale: Bool = false, title: String = "") -> SessionEntry {
    SessionEntry(agent: "claude-code", sessionID: id, status: status, title: title, since: t0, updated: t0, stale: stale)
}

@Test func statusBarCountsOnlyLiveWaiting() {
    #expect(Board.statusBarState([]).waiting == 0)
    #expect(Board.statusBarState([entry("a", .working)]).waiting == 0)
    #expect(Board.statusBarState([entry("a", .waiting)]).waiting == 1)
    let three = [entry("a", .waiting), entry("b", .waiting), entry("c", .waiting), entry("d", .idle)]
    #expect(Board.statusBarState(three).waiting == 3)
    #expect(Board.statusBarState([entry("a", .waiting, stale: true)]).waiting == 0)
    #expect(Board.statusBarState(three).tooltip == Board.headline(three))
}

@Test func menuLinesFormatAndTruncate() {
    #expect(Board.menuLines([], now: t0) == ["No sessions"])
    let one = Board.menuLines([entry("a", .waiting, title: "Proj")], now: t0.addingTimeInterval(300))
    #expect(one == ["!  Proj · claude · 5m"])
    let many = (0..<17).map { entry("s\($0)", .idle) }
    let lines = Board.menuLines(many, now: t0)
    #expect(lines.count == 16)
    #expect(lines.last == "… and 2 more")
    #expect(Board.menuLines(Array(many.prefix(15)), now: t0).count == 15)
}

@Test func secondLineCases() {
    var w = entry("abcdef", .waiting, title: "P"); w.detail = "needs OK"; w.topic = "Thema"
    #expect(Board.secondLine(w, ambiguous: true) == "needs OK")
    var wk = SessionEntry(agent: "claude-code", sessionID: "abcdef", status: .working, title: "P", since: t0, updated: t0, detail: "x")
    wk.topic = "Thema"
    #expect(Board.secondLine(wk, ambiguous: true) == "Thema")
    var nt = entry("abcdef", .idle, title: "P")
    #expect(Board.secondLine(nt, ambiguous: true) == "#abcd")
    #expect(Board.secondLine(nt, ambiguous: false) == nil)
    nt.topic = "Thema"
    #expect(Board.secondLine(nt, ambiguous: false) == "Thema")
}

@Test func secondLineError() {
    var e = SessionEntry(agent: "claude-code", sessionID: "abcdef", status: .error, since: t0, updated: t0, detail: "rate_limit")
    e.topic = "Thema"
    #expect(Board.secondLine(e, ambiguous: false) == "rate_limit")
    e.detail = ""
    #expect(Board.secondLine(e, ambiguous: false) == "Thema")
}

@Test func ambiguityOnlyForSameDisplayedName() {
    let a = entry("s1", .idle, title: "assistant"), b = entry("s2", .idle, title: "assistant"), c = entry("s3", .idle, title: "other")
    #expect(Board.ambiguousNames([a, b, c]) == ["assistant"])
    let lines = Board.menuLines([a, b, c], now: t0)
    #expect(lines[0].contains("assistant — #s1"))
    #expect(!lines[2].contains("—"))
}

@Test func topicLoadedAndMenuTruncated() {
    let d = makeDir()
    put(d, "claude-code@x.json", #"{"v":1,"agent":"claude-code","id":"x","status":"idle","updated":"2026-10-06T18:00:00Z","title":"P","topic":"\#(String(repeating: "t", count: 100))"}"#)
    let es = Board.load(dir: d, now: t0, alive: { _ in true }, boot: nil)
    #expect(es[0].topic.count == 100)
    #expect(Board.menuLines(es, now: t0)[0].contains("P — " + String(repeating: "t", count: 59) + "…"))
}

@Test func loginRequestDecision() {
    #expect(Board.loginRequest(defaultsValue: nil, currentlyOn: false) == nil)
    #expect(Board.loginRequest(defaultsValue: true, currentlyOn: false) == true)
    #expect(Board.loginRequest(defaultsValue: false, currentlyOn: true) == false)
    // schon im gewuenschten Zustand: nichts tun (sonst Dialog im LaunchAgent-Rueckfall)
    #expect(Board.loginRequest(defaultsValue: true, currentlyOn: true) == nil)
    #expect(Board.loginRequest(defaultsValue: false, currentlyOn: false) == nil)
}
