# Relocate unique workspace sources across platforms

## Context

Moving `공주시.gogis` from its original directory left three saved relative SHP paths pointing at missing files. All three datasets still existed in subdirectories under the new workspace folder. A user could relink manually, but opening the unchanged workspace still showed empty layers. Windows backslashes and drive paths, POSIX paths, and macOS Unicode filename forms also differ.

## Decision

Keep portable slash-separated relative paths on save. Preserve foreign absolute paths, including UNC shares, for display and relinking on another host. On load, first honor the saved path when it exists. If it is missing, scan only the workspace folder and two nested levels for a unique matching filename. Match case-insensitively after NFC Unicode normalization, including a foreign absolute path's basename. Do not follow symlinks or choose among duplicate names. Cap the scan at 20,000 entries; an incomplete scan leaves sources unavailable for explicit relinking. Preserve URI sources such as `postgis://` without filesystem rewriting.

## Verification

Workspace tests cover moved relative files, ambiguous names, a Windows absolute path on another host with an NFD Korean filename, UNC preservation, and URI preservation. The original unmodified `~/Downloads/공주시.gogis` now loads all three SHP layers from their current Downloads subdirectories in a native offscreen runtime; source counts are 291,314, 1,000,000, and 153,803. Windows and Linux workspace test binaries compile from this source; native execution on those hosts still needs platform testing.
