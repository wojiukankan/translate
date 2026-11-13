// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "Selection",
    platforms: [.macOS(.v13)],
    products: [.library(name: "Selection", type: .dynamic, targets: ["Selection"])],
    dependencies: [],
    targets: [
        .target(name: "Selection"),
    ],
    swiftLanguageModes: [.v5]
)
