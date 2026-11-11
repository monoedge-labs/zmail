package main

import (
	"embed"

	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/lucide"
)

// Lucide icons, through Lobe MyGo.
var (
	icoInbox       = lucide.Inbox()
	icoSent        = lucide.Send()
	icoArchive     = lucide.Archive()
	icoFolder      = lucide.Folder()
	icoJunk        = lucide.MailWarning()
	icoShield      = lucide.ShieldCheck()
	icoShieldOff   = lucide.ShieldOff()
	icoMove        = lucide.FolderInput()
	icoChart       = lucide.ChartColumn()
	icoMail        = lucide.Mail()
	icoPlus        = lucide.Plus()
	icoChevron     = lucide.ChevronRight()
	icoChevronDown = lucide.ChevronDown()
	icoBack        = lucide.ChevronLeft()
	icoClose       = lucide.X()
	icoEye         = lucide.Eye()
	icoMailOpen    = lucide.MailOpen()
	icoFeedback    = lucide.MessageSquareAlert()
	icoWarn        = lucide.TriangleAlert()
)

//go:embed assets
var assets embed.FS

// logo is the app's icon, small, for the sidebar.
var logo = func() ui.ImageSource {
	b, _ := assets.ReadFile("assets/logo.png")
	img, err := ui.DecodeBitmap(b)
	if err != nil {
		panic(err)
	}
	return img
}()

// brand is a mail service's mark (Simple Icons, CC0) and color.
type brand struct {
	icon  *ui.SVG
	color ui.Color
}

var brands = func() map[string]brand {
	m := map[string]brand{}
	for provider, b := range map[string]struct{ file, hex string }{
		"google": {"gmail", "#EA4335"}, "icloud": {"icloud", "#3693F3"},
		"qq": {"tencentqq", "#1EBAFC"}, "aliyun": {"alibabacloud", "#FF6A00"},
	} {
		data, _ := assets.ReadFile("assets/brands/" + b.file + ".svg")
		m[provider] = brand{ui.MustParseSVG(data), ui.Hex(b.hex)}
	}
	return m
}()
