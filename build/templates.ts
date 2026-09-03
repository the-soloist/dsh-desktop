import type { ApplicationMetadata } from "./config";

export function macOSInfoPlist(metadata: ApplicationMetadata, version: string): string {
  return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key>
  <string>zh_CN</string>
  <key>CFBundleDisplayName</key>
  <string>${metadata.displayName}</string>
  <key>CFBundleExecutable</key>
  <string>${metadata.displayName}</string>
  <key>CFBundleIdentifier</key>
  <string>${metadata.bundleIdentifier}</string>
  <key>CFBundleIconFile</key>
  <string>${metadata.displayName}.icns</string>
  <key>CFBundleInfoDictionaryVersion</key>
  <string>6.0</string>
  <key>CFBundleName</key>
  <string>${metadata.displayName}</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>${version}</string>
  <key>CFBundleVersion</key>
  <string>${version}</string>
  <key>LSMinimumSystemVersion</key>
  <string>13.0</string>
  <key>NSAppTransportSecurity</key>
  <dict>
    <key>NSAllowsLocalNetworking</key>
    <true/>
  </dict>
  <key>NSHighResolutionCapable</key>
  <true/>
</dict>
</plist>
`;
}

export function linuxDesktopEntry(metadata: ApplicationMetadata): string {
  return `[Desktop Entry]
Type=Application
Name=${metadata.displayName}
Comment=${metadata.description}
Exec="${metadata.displayName}"
Icon=${metadata.linuxDesktopId}
Categories=Development;
Terminal=false
`;
}
