//go:build windows

package main

func main() {
	if err := runGUIApp(); err != nil {
		showSimpleMessageBox("VAXEE AutoSwitch", err.Error())
	}
}
