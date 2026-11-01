//go:build linux && webkitgui && profile

package main

func init() { profileLinuxGUI = func() func() { return profileHeadless() } }
