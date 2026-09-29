//go:build qt

package main

import (
	_ "embed"
	"os"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
)

// Main.qml is embedded so the prototype can be launched from a checkout
// without relying on a working directory or an installed resource bundle.
//
//go:embed qml/Main.qml
var mainQML []byte

func main() {
	qt.NewQApplication(os.Args)
	engine := qml.NewQQmlApplicationEngine()
	engine.LoadData(mainQML)
	qt.QApplication_Exec()
}
