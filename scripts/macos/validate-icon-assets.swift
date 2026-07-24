import AppKit
import Darwin
import Foundation

private struct RasterSummary {
    let width: Int
    let height: Int
    let visiblePixels: Int
    let transparentPixels: Int
    let maximumChannelDelta: CGFloat
    let cornerAlphas: [CGFloat]
}

private struct IconComposerDocument: Decodable {
    let groups: [IconComposerGroup]
}

private struct IconComposerGroup: Decodable {
    let name: String
    let layers: [IconComposerLayer]
    let translucency: IconComposerTranslucency
}

private struct IconComposerLayer: Decodable {
    let imageName: String
    let name: String
    let glass: Bool?

    private enum CodingKeys: String, CodingKey {
        case imageName = "image-name"
        case name
        case glass
    }
}

private struct IconComposerTranslucency: Decodable {
    let enabled: Bool
    let value: Double
}

private func fail(_ message: String) -> Never {
    FileHandle.standardError.write(Data("icon asset validation failed: \(message)\n".utf8))
    Darwin.exit(1)
}

private func summarize(_ path: String) -> RasterSummary {
    guard let data = try? Data(contentsOf: URL(fileURLWithPath: path)),
          let bitmap = NSBitmapImageRep(data: data)
    else {
        fail("cannot decode \(path)")
    }

    var visiblePixels = 0
    var transparentPixels = 0
    var maximumChannelDelta: CGFloat = 0
    for y in 0..<bitmap.pixelsHigh {
        for x in 0..<bitmap.pixelsWide {
            guard let color = bitmap.colorAt(x: x, y: y)?.usingColorSpace(.deviceRGB) else {
                fail("cannot read pixel \(x),\(y) from \(path)")
            }
            if color.alphaComponent <= 0.0001 {
                transparentPixels += 1
                continue
            }
            visiblePixels += 1
            maximumChannelDelta = max(
                maximumChannelDelta,
                abs(color.redComponent - color.greenComponent),
                abs(color.greenComponent - color.blueComponent),
                abs(color.redComponent - color.blueComponent)
            )
        }
    }

    let cornerAlphas = [
        bitmap.colorAt(x: 0, y: 0)?.alphaComponent ?? 1,
        bitmap.colorAt(x: bitmap.pixelsWide - 1, y: 0)?.alphaComponent ?? 1,
        bitmap.colorAt(x: 0, y: bitmap.pixelsHigh - 1)?.alphaComponent ?? 1,
        bitmap.colorAt(x: bitmap.pixelsWide - 1, y: bitmap.pixelsHigh - 1)?.alphaComponent ?? 1,
    ]
    return RasterSummary(
        width: bitmap.pixelsWide,
        height: bitmap.pixelsHigh,
        visiblePixels: visiblePixels,
        transparentPixels: transparentPixels,
        maximumChannelDelta: maximumChannelDelta,
        cornerAlphas: cornerAlphas
    )
}

guard CommandLine.arguments.count == 2 else {
    fail("usage: validate-icon-assets.swift <repo-root>")
}
let root = URL(fileURLWithPath: CommandLine.arguments[1]).standardizedFileURL.path
let iconset = "\(root)/app/macos/Resources/AppIcon/CodexPulse.iconset"
let iconComposerRoot = "\(root)/app/macos/Resources/AppIcon/CodexPulse.icon"
let layerSourceRoot = "\(root)/app/macos/Resources/AppIcon/Layers"
let statusRoot = "\(root)/app/macos/Resources/StatusItem"

let iconComposerJSON = "\(iconComposerRoot)/icon.json"
guard let iconComposerData = try? Data(contentsOf: URL(fileURLWithPath: iconComposerJSON)),
      let iconComposer = try? JSONDecoder().decode(IconComposerDocument.self, from: iconComposerData)
else {
    fail("cannot decode \(iconComposerJSON)")
}
guard iconComposer.groups.count == 1 else {
    fail("Icon Composer source must contain exactly one group")
}
private let iconComposerGroup = iconComposer.groups[0]
guard iconComposerGroup.name == "Pulse Core" else {
    fail("unexpected Icon Composer group name: \(iconComposerGroup.name)")
}
let expectedLayerNames = [
    "06-CoreDot",
    "05-CoreLens",
    "04-Pulse",
    "03-MainOrbit",
    "02-RearTrace",
    "01-Background",
]
guard iconComposerGroup.layers.map(\.name) == expectedLayerNames else {
    fail("unexpected Icon Composer layer order")
}
guard iconComposerGroup.translucency.enabled,
      iconComposerGroup.translucency.value > 0,
      iconComposerGroup.translucency.value <= 1
else {
    fail("Icon Composer group translucency must remain enabled and bounded")
}
for layer in iconComposerGroup.layers {
    let packagedAsset = "\(iconComposerRoot)/Assets/\(layer.imageName)"
    let canonicalSource = "\(layerSourceRoot)/\(layer.name).svg"
    guard FileManager.default.contentsEqual(atPath: packagedAsset, andPath: canonicalSource) else {
        fail("Icon Composer asset differs from canonical layer: \(layer.name)")
    }
    if layer.name == "01-Background" {
        guard layer.glass == false else {
            fail("Icon Composer background must opt out of glass effects")
        }
    } else {
        guard layer.glass != false else {
            fail("Icon Composer foreground layer must keep glass effects: \(layer.name)")
        }
    }
}

let appIcons: [(String, Int)] = [
    ("icon_16x16.png", 16),
    ("icon_16x16@2x.png", 32),
    ("icon_32x32.png", 32),
    ("icon_32x32@2x.png", 64),
    ("icon_128x128.png", 128),
    ("icon_128x128@2x.png", 256),
    ("icon_256x256.png", 256),
    ("icon_256x256@2x.png", 512),
    ("icon_512x512.png", 512),
    ("icon_512x512@2x.png", 1024),
]

for (name, expectedSize) in appIcons {
    let path = "\(iconset)/\(name)"
    let summary = summarize(path)
    guard summary.width == expectedSize, summary.height == expectedSize else {
        fail("unexpected dimensions for \(name): \(summary.width)x\(summary.height)")
    }
    guard summary.visiblePixels > 0, summary.transparentPixels > 0 else {
        fail("\(name) must contain visible and transparent pixels")
    }
    guard summary.cornerAlphas.allSatisfy({ $0 <= 0.0001 }) else {
        fail("\(name) must keep transparent corners")
    }
}

let statusIcons: [(String, Int)] = [
    ("CodexPulseStatusTemplate.png", 19),
    ("CodexPulseStatusTemplate@2x.png", 38),
]
for (name, expectedSize) in statusIcons {
    let summary = summarize("\(statusRoot)/\(name)")
    guard summary.width == expectedSize, summary.height == expectedSize else {
        fail("unexpected dimensions for \(name): \(summary.width)x\(summary.height)")
    }
    guard summary.visiblePixels > 0, summary.transparentPixels > 0 else {
        fail("\(name) must contain visible and transparent pixels")
    }
    guard summary.maximumChannelDelta < 0.001 else {
        fail("\(name) must remain strictly grayscale")
    }
    guard summary.cornerAlphas.allSatisfy({ $0 <= 0.0001 }) else {
        fail("\(name) must keep transparent corners")
    }
}

print("icon asset validation passed: icon-composer=6 appicon=10 status-item=2")
