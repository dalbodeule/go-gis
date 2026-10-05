#define AppVersion GetEnv("GOGIS_VERSION")
#define TargetArch GetEnv("GOGIS_ARCH")
#define TargetArchLabel GetEnv("GOGIS_ARCH_LABEL")
#define PackageDir GetEnv("GOGIS_PACKAGE_DIR")

#if AppVersion == ""
  #error GOGIS_VERSION environment variable is required
#endif
#if TargetArch == ""
  #error GOGIS_ARCH environment variable is required
#endif
#if TargetArchLabel == ""
  #error GOGIS_ARCH_LABEL environment variable is required
#endif
#if PackageDir == ""
  #error GOGIS_PACKAGE_DIR environment variable is required
#endif

[Setup]
AppId={{7F5D3AF8-35A1-4E35-91D7-13D1E9C73F20}
AppName=GoGIS
AppVersion={#AppVersion}
AppPublisher=GoGIS
DefaultDirName={autopf}\GoGIS
DefaultGroupName=GoGIS
PrivilegesRequired=lowest
ArchitecturesAllowed={#TargetArch}
ArchitecturesInstallIn64BitMode={#TargetArch}
OutputDir={#PackageDir}
OutputBaseFilename=GoGIS-{#AppVersion}-windows-{#TargetArchLabel}-setup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
UninstallDisplayIcon={app}\GoGIS.exe

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"; GroupDescription: "Additional shortcuts:"; Flags: unchecked

[Files]
Source: "{#PackageDir}\GoGIS-{#AppVersion}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{autoprograms}\GoGIS"; Filename: "{app}\GoGIS.exe"
Name: "{autodesktop}\GoGIS"; Filename: "{app}\GoGIS.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\GoGIS.exe"; Description: "Launch GoGIS"; Flags: postinstall nowait skipifsilent
