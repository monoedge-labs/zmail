package main

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
)

// useUpdater lets the app update itself from its GitHub releases. The native
// variant draws the update window with the app's own UI, so it needs no
// web view. Demo mode never checks in the background.
//
// Setting a menu replaces MyGo's default one, so this is that default (App,
// File, Edit, View, Window) with "Check for Updates…" after About.
func useUpdater(demo bool) {
	mygo.Use(native.New(updater.Options{DisableAutomaticChecks: demo}))
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu, Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			updater.MenuItem(),
			mygo.Separator(),
			{Role: mygo.RoleServices},
			mygo.Separator(),
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			{Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Role: mygo.RoleFileMenu},
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleViewMenu},
		{Role: mygo.RoleWindowMenu},
	}))
}
