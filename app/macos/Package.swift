// swift-tools-version: 6.1

import PackageDescription

let package = Package(
    name: "CodexPulseMacOS",
    platforms: [.macOS(.v15)],
    products: [
        .library(name: "CodexPulseCoreClient", targets: ["CodexPulseCoreClient"]),
        .executable(name: "codex-pulse-transport-spike", targets: ["CodexPulseTransportSpike"]),
        .executable(name: "codex-pulse-core-client-tests", targets: ["CodexPulseCoreClientTests"]),
    ],
    dependencies: [
        .package(url: "https://github.com/grpc/grpc-swift-2.git", exact: "2.4.2"),
        .package(url: "https://github.com/grpc/grpc-swift-nio-transport.git", exact: "2.9.0"),
        .package(url: "https://github.com/grpc/grpc-swift-protobuf.git", exact: "2.4.1"),
        .package(url: "https://github.com/apple/swift-protobuf.git", exact: "1.38.1"),
    ],
    targets: [
        .target(
            name: "CodexPulseProtocolGenerated",
            dependencies: [
                .product(name: "GRPCCore", package: "grpc-swift-2"),
                .product(name: "GRPCProtobuf", package: "grpc-swift-protobuf"),
                .product(name: "SwiftProtobuf", package: "swift-protobuf"),
            ],
            exclude: ["README.md"]
        ),
        .target(
            name: "CodexPulseCoreClient",
            dependencies: [
                "CodexPulseProtocolGenerated",
                .product(name: "GRPCCore", package: "grpc-swift-2"),
                .product(name: "GRPCNIOTransportHTTP2", package: "grpc-swift-nio-transport"),
                .product(name: "GRPCProtobuf", package: "grpc-swift-protobuf"),
            ]
        ),
        .executableTarget(
            name: "CodexPulseTransportSpike",
            dependencies: ["CodexPulseCoreClient"]
        ),
        .executableTarget(
            name: "CodexPulseCoreClientTests",
            dependencies: [
                "CodexPulseCoreClient",
                "CodexPulseProtocolGenerated",
                .product(name: "GRPCCore", package: "grpc-swift-2"),
                .product(name: "GRPCProtobuf", package: "grpc-swift-protobuf"),
                .product(name: "SwiftProtobuf", package: "swift-protobuf"),
            ],
            path: "Tests/CodexPulseCoreClientTests"
        ),
    ],
    swiftLanguageModes: [.v6]
)
