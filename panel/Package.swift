// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "AgentStatusPanel",
    platforms: [.macOS(.v14)],
    targets: [
        .target(name: "AgentStatusCore"),
        .executableTarget(name: "AgentStatusPanel", dependencies: ["AgentStatusCore"]),
        .testTarget(name: "AgentStatusCoreTests", dependencies: ["AgentStatusCore"]),
    ]
)
