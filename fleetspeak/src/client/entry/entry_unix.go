//go:build linux || darwin

package entry

// RunMain starts the application.
func RunMain(innerMain func(), _ /* windowsServiceName */ string) {
	innerMain()
}
