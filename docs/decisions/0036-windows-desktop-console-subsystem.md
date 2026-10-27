# ADR 0036: Windows desktop console subsystem

## Status

Accepted.

## Decision

Windows Qt desktop executables are linked with Go's `-H=windowsgui` linker
option. This prevents Windows Explorer/double-click launches from allocating a
new console window while preserving the parent console's standard handles when
the executable is started from `cmd.exe` or PowerShell. The desktop output
capture continues to mirror those handles when they are available and keeps
the diagnostic viewer for application output.

The CLI executables remain console-subsystem binaries.

## Consequences

- Windows desktop build and packaging scripts must pass `-H=windowsgui`.
- A desktop executable started without a parent console has no visible place
  for stdout/stderr; application diagnostics remain available through the
  in-app diagnostic viewer.
- This is a Windows linker setting only and does not change the shared command
  API or the platform-independent runtime.
