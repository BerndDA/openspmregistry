// swift-tools-version:6.0
import PackageDescription

let package = Package(
    name: "SMSSDKConsumer",
    platforms: [.macOS(.v12)],
    dependencies: [
        .package(id: "example.SMSSDK", from: "1.0.0"),
    ],
    targets: [
        .executableTarget(
            name: "SMSSDKConsumer",
            dependencies: [
                .product(name: "SMSSDK", package: "example.SMSSDK"),
            ]
        ),
    ]
)
