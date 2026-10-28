# Own Qt settings keys for each operation

## Context

GoGIS reads three integer preferences through miqt v0.15.0's `QAnyStringView` constructor. Its C++ constructor creates a view over a temporary `QString`, so the view can point to freed storage before `QSettings` reads it. The preference tests intermittently returned missing or stale values. This could also affect the saved SHP index and visible viewport limits in the application.

## Decision

Use a small Qt Core bridge for these preference reads. It converts the Go key to an owned `QString` and performs the complete `QSettings` operation before that string is destroyed. Keep the existing range checks and defaults. The same bridge provides test writes and removal so the tests exercise the actual key path without the dangling view.

## Verification

Repeat preference tests under the documented `qt` build and run the full `qt` and `qt native` desktop suites.
